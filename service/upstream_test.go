package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUpstreamTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(models...))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	return db
}

func TestNormalizeUpstreamBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "normalizes host and trailing slash", input: "HTTPS://Example.COM:443/v1/", want: "https://example.com/v1"},
		{name: "removes query and fragment", input: "https://example.com/api?token=secret#part", want: "https://example.com/api"},
		{name: "rejects unsupported scheme", input: "file:///tmp/upstream", wantErr: true},
		{name: "rejects missing host", input: "https:///v1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeUpstreamBaseURL(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateUpstreamCandidateAcceptsAllChannelTypes(t *testing.T) {
	for channelType := range constant.ChannelTypeNames {
		if channelType == constant.ChannelTypeUnknown {
			continue
		}
		_, err := ValidateUpstreamCandidateInput(dto.UpstreamCandidateRequest{
			Type: channelType, Name: "partner", BaseURL: "https://example.com",
		})
		require.NoErrorf(t, err, "channel type %d should be accepted", channelType)
	}
}

func TestUpstreamBenchmarkAcceptsAllChannelTypes(t *testing.T) {
	for channelType := range constant.ChannelTypeNames {
		if channelType == constant.ChannelTypeUnknown {
			continue
		}
		assert.Truef(t, SupportsUpstreamBenchmark(channelType), "channel type %d should support benchmarking", channelType)
	}
}

func TestDiscoverUpstreamModelsUsesConfiguredModelsForUnsupportedDiscovery(t *testing.T) {
	models, err := DiscoverUpstreamModels(context.Background(), &model.UpstreamCandidate{
		Type:   constant.ChannelTypeMidjourney,
		Models: "custom-model, custom-model, second-model",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"custom-model", "second-model"}, models)
}

func TestCreateBusinessCooperationIsAtomic(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{})

	valid := dto.BusinessCooperationRequest{Upstream: dto.UpstreamCandidateRequest{Type: constant.ChannelTypeOpenAI, Name: "partner", BaseURL: "https://example.com", APIKey: "secret"}}
	application, err := CreateBusinessCooperation(7, valid)
	require.NoError(t, err)
	assert.NotZero(t, application.UpstreamID)

	_, err = CreateBusinessCooperation(8, valid)
	assert.ErrorContains(t, err, "already exist")
	var applicationCount int64
	require.NoError(t, db.Model(&model.BusinessCooperation{}).Count(&applicationCount).Error)
	assert.EqualValues(t, 1, applicationCount)
}

func TestBusinessCooperationPersistsNormalizedModels(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{})
	application, err := CreateBusinessCooperation(7, dto.BusinessCooperationRequest{Upstream: dto.UpstreamCandidateRequest{
		Type: constant.ChannelTypeOpenAI, Name: "partner", BaseURL: "https://example.com", APIKey: "secret", Models: "gpt-4o, gpt-4o, custom-model",
	}})
	require.NoError(t, err)
	var candidate model.UpstreamCandidate
	require.NoError(t, db.First(&candidate, application.UpstreamID).Error)
	assert.Equal(t, "gpt-4o,custom-model", candidate.Models)
}

func TestBusinessCooperationEditResubmitAndDeleteStates(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{})
	input := dto.BusinessCooperationRequest{Upstream: dto.UpstreamCandidateRequest{Type: constant.ChannelTypeOpenAI, Name: "partner", BaseURL: "https://example.com", APIKey: "secret"}}
	application, err := CreateBusinessCooperation(7, input)
	require.NoError(t, err)

	updated, err := UpdateBusinessCooperation(7, application.ID, dto.BusinessCooperationRequest{Upstream: dto.UpstreamCandidateRequest{
		Type: constant.ChannelTypeOpenAI, Name: "updated", BaseURL: "https://example.com/v1", Contact: "ops",
	}}, false)
	require.NoError(t, err)
	assert.Equal(t, constant.BusinessCooperationPendingReview, updated.Status)

	require.NoError(t, db.Model(&model.BusinessCooperation{}).Where("id = ?", application.ID).Updates(map[string]any{
		"status":        constant.BusinessCooperationRejected,
		"reject_reason": "score below threshold",
	}).Error)
	_, err = UpdateBusinessCooperation(7, application.ID, input, false)
	assert.ErrorContains(t, err, "only pending applications can be edited")
	assert.ErrorContains(t, DeleteBusinessCooperation(7, application.ID), "only pending applications can be deleted")
	resubmitted, err := UpdateBusinessCooperation(7, application.ID, input, true)
	require.NoError(t, err)
	assert.Equal(t, constant.BusinessCooperationPendingReview, resubmitted.Status)
	assert.Equal(t, 2, resubmitted.Revision)

	require.NoError(t, DeleteBusinessCooperation(7, application.ID))
	var count int64
	require.NoError(t, db.Model(&model.BusinessCooperation{}).Where("id = ?", application.ID).Count(&count).Error)
	assert.Zero(t, count)
}

func TestRecoverExpiredUpstreamBenchmarkTask(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{}, &model.UpstreamBenchmarkProfile{}, &model.SystemTask{}, &model.SystemTaskLock{})
	application := &model.BusinessCooperation{UserID: 7, Status: constant.BusinessCooperationBenchmarkRunning}
	require.NoError(t, db.Create(application).Error)
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceSelf, OwnerUserID: &application.UserID, ApplicationID: &application.ID, Type: constant.ChannelTypeOpenAI, Name: "partner", BaseURL: "https://example.com", NormalizedBaseURL: "https://example.com", APIKey: "secret", BenchmarkStatus: constant.UpstreamBenchmarkRunning}
	require.NoError(t, db.Create(candidate).Error)
	run := &model.UpstreamBenchmarkRun{UpstreamID: candidate.ID, Status: constant.UpstreamBenchmarkRunRunning}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Model(candidate).Update("latest_run_id", run.ID).Error)
	payload := UpstreamBenchmarkPayload{UpstreamID: candidate.ID, RunID: run.ID}
	task, err := model.CreateUpstreamBenchmarkSystemTask(db, payload, UpstreamBenchmarkState{Stage: "request"})
	require.NoError(t, err)
	require.NoError(t, db.Model(task).Updates(map[string]any{"status": model.SystemTaskStatusRunning, "locked_by": "worker-a"}).Error)
	require.NoError(t, db.Create(&model.SystemTaskLock{Type: constant.SystemTaskTypeUpstreamBenchmark, TaskID: task.TaskID, LockedBy: "worker-a", LockedUntil: common.GetTimestamp() - 1}).Error)

	require.NoError(t, RecoverExpiredUpstreamBenchmarkTasks(common.GetTimestamp()))
	var gotTask model.SystemTask
	require.NoError(t, db.First(&gotTask, "task_id = ?", task.TaskID).Error)
	assert.Equal(t, model.SystemTaskStatusFailed, gotTask.Status)
	var gotCandidate model.UpstreamCandidate
	require.NoError(t, db.First(&gotCandidate, candidate.ID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkPending, gotCandidate.BenchmarkStatus)
	var gotApplication model.BusinessCooperation
	require.NoError(t, db.First(&gotApplication, application.ID).Error)
	assert.Equal(t, constant.BusinessCooperationPendingReview, gotApplication.Status)
	var gotRun model.UpstreamBenchmarkRun
	require.NoError(t, db.First(&gotRun, run.ID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkRunFailed, gotRun.Status)
}

func TestCancelUpstreamBenchmarkStopsActiveTaskAndRestoresReviewState(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{}, &model.SystemTask{}, &model.SystemTaskLock{})
	application := &model.BusinessCooperation{UserID: 7, Status: constant.BusinessCooperationBenchmarkRunning}
	require.NoError(t, db.Create(application).Error)
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceSelf, OwnerUserID: &application.UserID, ApplicationID: &application.ID, Type: constant.ChannelTypeOpenAI, Name: "partner", BaseURL: "https://example.com", NormalizedBaseURL: "https://example.com", APIKey: "secret", BenchmarkStatus: constant.UpstreamBenchmarkRunning}
	require.NoError(t, db.Create(candidate).Error)
	run := &model.UpstreamBenchmarkRun{UpstreamID: candidate.ID, Status: constant.UpstreamBenchmarkRunRunning, StartedTime: common.GetTimestamp() - 2}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Model(candidate).Update("latest_run_id", run.ID).Error)
	payload := UpstreamBenchmarkPayload{UpstreamID: candidate.ID, RunID: run.ID}
	task, err := model.CreateUpstreamBenchmarkSystemTask(db, payload, UpstreamBenchmarkState{Stage: "request"})
	require.NoError(t, err)
	require.NoError(t, db.Model(run).Update("system_task_id", task.TaskID).Error)
	require.NoError(t, db.Model(task).Updates(map[string]any{"status": model.SystemTaskStatusRunning, "locked_by": "worker-a"}).Error)
	require.NoError(t, db.Create(&model.SystemTaskLock{Type: constant.SystemTaskTypeUpstreamBenchmark, TaskID: task.TaskID, LockedBy: "worker-a", LockedUntil: common.GetTimestamp() + 300}).Error)

	require.NoError(t, CancelUpstreamBenchmark(candidate.ID))

	var gotTask model.SystemTask
	require.NoError(t, db.First(&gotTask, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusCancelled, gotTask.Status)
	assert.Nil(t, gotTask.ActiveKey)
	var state UpstreamBenchmarkState
	require.NoError(t, gotTask.DecodeState(&state))
	assert.Equal(t, "cancelled", state.Stage)
	var gotRun model.UpstreamBenchmarkRun
	require.NoError(t, db.First(&gotRun, run.ID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkRunCancelled, gotRun.Status)
	assert.GreaterOrEqual(t, gotRun.DurationMS, int64(2000))
	var gotCandidate model.UpstreamCandidate
	require.NoError(t, db.First(&gotCandidate, candidate.ID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkPending, gotCandidate.BenchmarkStatus)
	var gotApplication model.BusinessCooperation
	require.NoError(t, db.First(&gotApplication, application.ID).Error)
	assert.Equal(t, constant.BusinessCooperationPendingReview, gotApplication.Status)
	var lockCount int64
	require.NoError(t, db.Model(&model.SystemTaskLock{}).Where("task_id = ?", task.TaskID).Count(&lockCount).Error)
	assert.Zero(t, lockCount)
	assert.ErrorIs(t, completeUpstreamBenchmarkTask(task, "worker-a", payload, &UpstreamBenchmarkResult{StatusCode: 200, Score: 100}, nil), model.ErrSystemTaskLockLost)
}

func TestBenchmarkCompletionCannotWriteAfterLeaseLoss(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{}, &model.SystemTask{}, &model.SystemTaskLock{})
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeOpenAI, Name: "admin", BaseURL: "https://example.com", NormalizedBaseURL: "https://example.com", APIKey: "secret", BenchmarkStatus: constant.UpstreamBenchmarkRunning}
	require.NoError(t, db.Create(candidate).Error)
	run := &model.UpstreamBenchmarkRun{UpstreamID: candidate.ID, Status: constant.UpstreamBenchmarkRunRunning}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Model(candidate).Update("latest_run_id", run.ID).Error)
	payload := UpstreamBenchmarkPayload{UpstreamID: candidate.ID, RunID: run.ID}
	task, err := model.CreateUpstreamBenchmarkSystemTask(db, payload, UpstreamBenchmarkState{Stage: "request"})
	require.NoError(t, err)
	require.NoError(t, db.Model(task).Updates(map[string]any{"status": model.SystemTaskStatusRunning, "locked_by": "worker-a"}).Error)
	require.NoError(t, db.Create(&model.SystemTaskLock{Type: constant.SystemTaskTypeUpstreamBenchmark, TaskID: task.TaskID, LockedBy: "worker-a", LockedUntil: common.GetTimestamp() - 1}).Error)

	err = completeUpstreamBenchmarkTask(task, "worker-a", payload, &UpstreamBenchmarkResult{StatusCode: 200, Score: 100}, nil)
	assert.True(t, errors.Is(err, model.ErrSystemTaskLockLost))
	var gotRun model.UpstreamBenchmarkRun
	require.NoError(t, db.First(&gotRun, run.ID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkRunRunning, gotRun.Status)
}

func TestEnqueueUpstreamBenchmarkIsGlobalAndPropagatesState(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{}, &model.UpstreamBenchmarkProfile{}, &model.SystemTask{}, &model.SystemTaskLock{})
	first, err := CreateBusinessCooperation(7, dto.BusinessCooperationRequest{Upstream: dto.UpstreamCandidateRequest{
		Type: constant.ChannelTypeOpenAI, Name: "first", BaseURL: "https://first.example.com", APIKey: "secret-1",
	}})
	require.NoError(t, err)
	second, err := CreateBusinessCooperation(8, dto.BusinessCooperationRequest{Upstream: dto.UpstreamCandidateRequest{
		Type: constant.ChannelTypeOpenAI, Name: "second", BaseURL: "https://second.example.com", APIKey: "secret-2",
	}})
	require.NoError(t, err)

	task, err := EnqueueUpstreamBenchmark(first.UpstreamID, "gpt-4o-mini")
	require.NoError(t, err)
	assert.NotEmpty(t, task.TaskID)
	var firstCandidate model.UpstreamCandidate
	require.NoError(t, db.First(&firstCandidate, first.UpstreamID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkRunning, firstCandidate.BenchmarkStatus)
	var firstApplication model.BusinessCooperation
	require.NoError(t, db.First(&firstApplication, first.ID).Error)
	assert.Equal(t, constant.BusinessCooperationBenchmarkRunning, firstApplication.Status)
	var run model.UpstreamBenchmarkRun
	require.NoError(t, db.First(&run, "id = ?", firstCandidate.LatestRunID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkProfileGeneral, run.ProfileName)
	assert.NotEmpty(t, run.ProfileSnapshot)
	assert.NotContains(t, task.Payload, "secret-1")

	_, err = EnqueueUpstreamBenchmark(second.UpstreamID, "gpt-4o-mini")
	assert.ErrorIs(t, err, ErrUpstreamBenchmarkBusy)
}

func TestCompleteUpstreamBenchmarkUpdatesApplicationState(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.BusinessCooperation{}, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{}, &model.SystemTask{}, &model.SystemTaskLock{})
	application := &model.BusinessCooperation{UserID: 7, Status: constant.BusinessCooperationBenchmarkRunning}
	require.NoError(t, db.Create(application).Error)
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceSelf, OwnerUserID: &application.UserID, ApplicationID: &application.ID, Type: constant.ChannelTypeOpenAI, Name: "partner", BaseURL: "https://example.com", NormalizedBaseURL: "https://example.com", APIKey: "secret", BenchmarkStatus: constant.UpstreamBenchmarkRunning}
	require.NoError(t, db.Create(candidate).Error)
	run := &model.UpstreamBenchmarkRun{UpstreamID: candidate.ID, Status: constant.UpstreamBenchmarkRunRunning}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Model(candidate).Update("latest_run_id", run.ID).Error)
	payload := UpstreamBenchmarkPayload{UpstreamID: candidate.ID, RunID: run.ID}
	task, err := model.CreateUpstreamBenchmarkSystemTask(db, payload, UpstreamBenchmarkState{Stage: "request"})
	require.NoError(t, err)
	require.NoError(t, db.Model(task).Updates(map[string]any{"status": model.SystemTaskStatusRunning, "locked_by": "worker-a"}).Error)
	require.NoError(t, db.Create(&model.SystemTaskLock{Type: constant.SystemTaskTypeUpstreamBenchmark, TaskID: task.TaskID, LockedBy: "worker-a", LockedUntil: common.GetTimestamp() + 300}).Error)

	require.NoError(t, completeUpstreamBenchmarkTask(task, "worker-a", payload, &UpstreamBenchmarkResult{StatusCode: 200, LatencyMS: 12, Score: 100}, nil))
	var gotApplication model.BusinessCooperation
	require.NoError(t, db.First(&gotApplication, application.ID).Error)
	assert.Equal(t, constant.BusinessCooperationPendingDecision, gotApplication.Status)
	var gotCandidate model.UpstreamCandidate
	require.NoError(t, db.First(&gotCandidate, candidate.ID).Error)
	assert.Equal(t, constant.UpstreamBenchmarkCompleted, gotCandidate.BenchmarkStatus)
	var gotTask model.SystemTask
	require.NoError(t, db.First(&gotTask, task.ID).Error)
	assert.Equal(t, model.SystemTaskStatusSucceeded, gotTask.Status)
}

func TestBenchmarkStatusFromRunReflectsExecutionState(t *testing.T) {
	tests := []struct {
		name      string
		runStatus string
		want      string
	}{
		{name: "queued", runStatus: constant.UpstreamBenchmarkRunPending, want: constant.UpstreamBenchmarkPending},
		{name: "executing", runStatus: constant.UpstreamBenchmarkRunRunning, want: constant.UpstreamBenchmarkRunning},
		{name: "completed", runStatus: constant.UpstreamBenchmarkRunSucceeded, want: constant.UpstreamBenchmarkCompleted},
		{name: "failed", runStatus: constant.UpstreamBenchmarkRunFailed, want: constant.UpstreamBenchmarkPending},
		{name: "cancelled", runStatus: constant.UpstreamBenchmarkRunCancelled, want: constant.UpstreamBenchmarkPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, benchmarkStatusFromRun(tt.runStatus, constant.UpstreamBenchmarkRunning))
		})
	}
	assert.Equal(t, constant.UpstreamBenchmarkCompleted, benchmarkStatusFromRun("unknown", constant.UpstreamBenchmarkCompleted))
}

func TestCompleteUpstreamBenchmarkPersistsMetricsSeparately(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{}, &model.SystemTask{}, &model.SystemTaskLock{})
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeOpenAI, Name: "admin", BaseURL: "https://example.com", NormalizedBaseURL: "https://example.com", APIKey: "secret", BenchmarkStatus: constant.UpstreamBenchmarkRunning}
	require.NoError(t, db.Create(candidate).Error)
	run := &model.UpstreamBenchmarkRun{
		UpstreamID: candidate.ID, Status: constant.UpstreamBenchmarkRunRunning,
		StartedTime: common.GetTimestamp() - 2, ProfileName: "general", ProfileVersion: 1,
		BaselineSnapshot: `{"name":"general","version":1,"metrics":{"RPM":{"admission":75,"full":600,"weight":1,"enabled":true,"direction":"higher"}}}`,
	}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Model(candidate).Update("latest_run_id", run.ID).Error)
	payload := UpstreamBenchmarkPayload{UpstreamID: candidate.ID, RunID: run.ID}
	task, err := model.CreateUpstreamBenchmarkSystemTask(db, payload, UpstreamBenchmarkState{Stage: "request"})
	require.NoError(t, err)
	require.NoError(t, db.Model(task).Updates(map[string]any{"status": model.SystemTaskStatusRunning, "locked_by": "worker-a"}).Error)
	require.NoError(t, db.Create(&model.SystemTaskLock{Type: constant.SystemTaskTypeUpstreamBenchmark, TaskID: task.TaskID, LockedBy: "worker-a", LockedUntil: common.GetTimestamp() + 300}).Error)

	result := &UpstreamBenchmarkResult{
		StatusCode: 200, LatencyMS: 37,
		Metrics:    map[string]float64{constant.UpstreamMetricTTFTP50_8K: 37},
		Scores:     map[string]float64{constant.UpstreamMetricTTFTP50_8K: 94.2},
		UnmetCount: 0, Score: 94.2,
	}
	require.NoError(t, completeUpstreamBenchmarkTask(task, "worker-a", payload, result, nil))

	var got model.UpstreamBenchmarkRun
	require.NoError(t, db.First(&got, run.ID).Error)
	assert.Equal(t, 200, got.StatusCode)
	assert.EqualValues(t, 37, got.LatencyMS)
	assert.GreaterOrEqual(t, got.DurationMS, int64(1000))
	assert.NotEqual(t, got.Metrics, got.Scores)
	response := benchmarkRunResponse(&got)
	assert.Equal(t, result.Metrics, response.Metrics)
	assert.Equal(t, result.Scores, response.Scores)
	assert.Equal(t, "general", response.ProfileName)
	assert.Equal(t, 75.0, response.Baseline[constant.UpstreamMetricRPM].Admission)
}

func TestUpstreamBenchmarkScoringRules(t *testing.T) {
	higher := UpstreamBenchmarkMetricConfig{Admission: 100, Full: 200, Weight: 1, Enabled: true, Direction: "higher"}
	score, met := ScoreUpstreamBenchmarkMetric(50, higher)
	assert.Equal(t, 30.0, score)
	assert.False(t, met)
	score, met = ScoreUpstreamBenchmarkMetric(150, higher)
	assert.Equal(t, 80.0, score)
	assert.True(t, met)

	lower := UpstreamBenchmarkMetricConfig{Admission: 2000, Full: 500, Weight: 1, Enabled: true, Direction: "lower"}
	score, met = ScoreUpstreamBenchmarkMetric(3000, lower)
	assert.InDelta(t, 40.0, score, 0.0001)
	assert.False(t, met)
	score, met = ScoreUpstreamBenchmarkMetric(500, lower)
	assert.Equal(t, 100.0, score)
	assert.True(t, met)
}

func TestCalculateUpstreamBenchmarkScoreUsesWeightsAndRejectsMissingMetrics(t *testing.T) {
	config := UpstreamBenchmarkProfileConfig{Metrics: map[string]UpstreamBenchmarkMetricConfig{
		"RPM":  {Admission: 100, Full: 200, Weight: 2, Enabled: true, Direction: "higher"},
		"TPM":  {Admission: 100, Full: 200, Weight: 1, Enabled: true, Direction: "higher"},
		"TTFT": {Admission: 200, Full: 100, Weight: 1, Enabled: true, Direction: "lower"},
	}}

	score, scores, unmet := CalculateUpstreamBenchmarkScore(
		map[string]float64{"RPM": 100, "TPM": 200},
		config,
	)

	assert.Equal(t, 60.0, scores["RPM"])
	assert.Equal(t, 100.0, scores["TPM"])
	assert.Equal(t, 0.0, scores["TTFT"])
	assert.Equal(t, 55.0, score)
	assert.Equal(t, 1, unmet)
}

func TestBenchmarkRunResponseFallsBackToDefaultBaseline(t *testing.T) {
	response := benchmarkRunResponse(&model.UpstreamBenchmarkRun{ID: 1})

	assert.Equal(t, 75.0, response.Baseline[constant.UpstreamMetricRPM].Admission)
	assert.Equal(t, 600.0, response.Baseline[constant.UpstreamMetricRPM].Full)
	assert.Equal(t, 1.0, response.Baseline[constant.UpstreamMetricRPM].Weight)
}

func TestDiscoverUpstreamModelsNormalizesGeminiNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/models", r.URL.Path)
		assert.Equal(t, "secret", r.Header.Get("x-goog-api-key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-2.0-flash"},{"name":"gemini-2.0-flash"}]}`))
	}))
	defer server.Close()
	InitHttpClient()
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeGemini, NormalizedBaseURL: server.URL, APIKey: "secret"}
	models, err := DiscoverUpstreamModels(context.Background(), candidate)
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-2.0-flash"}, models)
}

func TestDiscoverUpstreamModelsUsesChannelProtocol(t *testing.T) {
	tests := []struct {
		name          string
		channelType   int
		wantPath      string
		wantHeader    string
		wantHeaderVal string
		response      string
		wantModels    []string
	}{
		{
			name: "ollama tags", channelType: constant.ChannelTypeOllama,
			wantPath: "/api/tags", wantHeader: "Authorization", wantHeaderVal: "Bearer secret",
			response: `{"models":[{"name":"llama3.2"}]}`, wantModels: []string{"llama3.2"},
		},
		{
			name: "anthropic models", channelType: constant.ChannelTypeAnthropic,
			wantPath: "/v1/models", wantHeader: "x-api-key", wantHeaderVal: "secret",
			response: `{"data":[{"id":"claude-sonnet-4"}]}`, wantModels: []string{"claude-sonnet-4"},
		},
		{
			name: "ali compatible models", channelType: constant.ChannelTypeAli,
			wantPath: "/compatible-mode/v1/models", wantHeader: "Authorization", wantHeaderVal: "Bearer secret",
			response: `{"data":[{"id":"qwen-plus"}]}`, wantModels: []string{"qwen-plus"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.wantPath, r.URL.Path)
				assert.Equal(t, tt.wantHeaderVal, r.Header.Get(tt.wantHeader))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			InitHttpClient()

			models, err := DiscoverUpstreamModels(context.Background(), &model.UpstreamCandidate{
				Source: constant.UpstreamSourceAdmin, Type: tt.channelType,
				NormalizedBaseURL: server.URL, APIKey: "secret",
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantModels, models)
		})
	}
}

func TestBuildGeminiProbeRequestUsesModelInURL(t *testing.T) {
	candidate := &model.UpstreamCandidate{Type: constant.ChannelTypeGemini, NormalizedBaseURL: "https://generativelanguage.googleapis.com"}
	payload, endpoint, err := buildUpstreamProbeRequest(candidate, "gemini-2.0-flash", "hello")
	require.NoError(t, err)
	assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent", endpoint)
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, common.Unmarshal(body, &fields))
	assert.NotContains(t, fields, "model")
	assert.Contains(t, fields, "contents")
}

func TestExecuteUpstreamProbeUsesConcurrencyAndAggregatesP90(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-test"}]}`))
	}))
	defer server.Close()
	InitHttpClient()
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeOpenAI, NormalizedBaseURL: server.URL, APIKey: "secret"}
	result, err := executeUpstreamProbe(context.Background(), candidate, "gpt-test", 3)
	require.NoError(t, err)
	assert.Equal(t, int32(4), requests.Load())
	assert.GreaterOrEqual(t, result.LatencyMS, int64(0))
}

func TestExecuteUpstreamWorkloadCollectsContextAndThroughputMetrics(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-test"}]}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions" {
			requests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":128,"completion_tokens":8,"total_tokens":136}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	InitHttpClient()
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeOpenAI, NormalizedBaseURL: server.URL, APIKey: "secret"}
	result, err := executeUpstreamWorkload(context.Background(), candidate, "gpt-test", 2)
	require.NoError(t, err)
	assert.Equal(t, int32(8), requests.Load())
	assert.GreaterOrEqual(t, result.Metrics[constant.UpstreamMetricTTFTP50_8K], float64(0))
	assert.GreaterOrEqual(t, result.Metrics[constant.UpstreamMetricTTFTP90_8K], result.Metrics[constant.UpstreamMetricTTFTP50_8K])
	assert.Greater(t, result.Metrics[constant.UpstreamMetricRPM], float64(0))
	assert.Greater(t, result.Metrics[constant.UpstreamMetricTPM], float64(0))
	assert.Greater(t, result.Metrics[constant.UpstreamMetricOTPPSP50], float64(0))
}

func TestExecuteUpstreamWorkloadSupportsAnthropicProtocol(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-test"}]}`))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" {
			if r.Header.Get("x-api-key") != "secret" || r.Header.Get("anthropic-version") == "" {
				http.Error(w, "missing anthropic authentication", http.StatusUnauthorized)
				return
			}
			requests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"usage":{"input_tokens":128,"output_tokens":8}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	InitHttpClient()
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeAnthropic, NormalizedBaseURL: server.URL, APIKey: "secret"}
	result, err := executeUpstreamWorkload(context.Background(), candidate, "claude-test", 2)
	require.NoError(t, err)
	assert.Equal(t, int32(8), requests.Load())
	assert.Greater(t, result.Metrics[constant.UpstreamMetricTPM], float64(0))
	assert.Greater(t, result.Metrics[constant.UpstreamMetricOTPPSP50], float64(0))
}

func TestExecuteUpstreamWorkloadUsesConfiguredModelWhenDiscoveryFails(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	InitHttpClient()
	candidate := &model.UpstreamCandidate{
		Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeMidjourney,
		NormalizedBaseURL: server.URL, APIKey: "secret", Models: "configured-model",
	}
	result, err := executeUpstreamWorkload(context.Background(), candidate, "configured-model", 1)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, result.StatusCode)
}

func TestEnqueueUpstreamBenchmarkValidatesConcurrency(t *testing.T) {
	_, err := EnqueueUpstreamBenchmarkWithOptions(1, "model", 101)
	assert.ErrorContains(t, err, "between 1 and 100")
}

func TestSyncUpstreamCandidatesRollsBackAsOneTransaction(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.UpstreamCandidate{}, &model.Channel{}, &model.Ability{})
	first := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeOpenAI, Name: "first", BaseURL: "https://first.example.com", NormalizedBaseURL: "https://first.example.com", APIKey: "secret-1", BenchmarkStatus: constant.UpstreamBenchmarkCompleted}
	second := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeOpenAI, Name: "second", BaseURL: "https://second.example.com", NormalizedBaseURL: "https://second.example.com", APIKey: "secret-2", BenchmarkStatus: constant.UpstreamBenchmarkPending}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	_, err := SyncUpstreamCandidates([]int64{first.ID, second.ID}, "partner")
	assert.ErrorContains(t, err, "benchmark is not completed")
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
	assert.Zero(t, channelCount)
	var gotFirst model.UpstreamCandidate
	require.NoError(t, db.First(&gotFirst, first.ID).Error)
	assert.Nil(t, gotFirst.ChannelID)
}

func TestSyncUpstreamCandidateUsesLatestBenchmarkModel(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.UpstreamCandidate{}, &model.UpstreamBenchmarkRun{}, &model.Channel{}, &model.Ability{})
	candidate := &model.UpstreamCandidate{Source: constant.UpstreamSourceAdmin, Type: constant.ChannelTypeOpenAI, Name: "custom", BaseURL: "https://custom.example.com", NormalizedBaseURL: "https://custom.example.com", APIKey: "secret", BenchmarkStatus: constant.UpstreamBenchmarkCompleted}
	require.NoError(t, db.Create(candidate).Error)
	run := &model.UpstreamBenchmarkRun{UpstreamID: candidate.ID, Model: "custom-model", Status: constant.UpstreamBenchmarkRunSucceeded}
	require.NoError(t, db.Create(run).Error)
	require.NoError(t, db.Model(candidate).Update("latest_run_id", run.ID).Error)
	channel, err := SyncUpstreamCandidateWithTag(candidate.ID, "partner")
	require.NoError(t, err)
	assert.Equal(t, "custom-model", channel.Models)
	assert.Equal(t, "custom-model", *channel.TestModel)
	assert.Equal(t, "partner", *channel.Tag)
}

func TestUpstreamBenchmarkProfileValidationAndVersionSnapshot(t *testing.T) {
	db := setupUpstreamTestDB(t, &model.UpstreamBenchmarkProfile{}, &model.Option{})
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptions })

	profile, err := GetUpstreamBenchmarkProfileResponse()
	require.NoError(t, err)
	assert.Equal(t, constant.UpstreamBenchmarkProfileGeneral, profile.Name)
	assert.Equal(t, 1, profile.Version)
	assert.Len(t, profile.Metrics, 9)
	for _, metric := range profile.Metrics {
		assert.True(t, metric.Enabled)
		assert.Positive(t, metric.Admission)
		assert.Positive(t, metric.Full)
		assert.Equal(t, float64(1), metric.Weight)
	}
	assert.Equal(t, float64(75), profile.Metrics[constant.UpstreamMetricRPM].Admission)
	assert.Equal(t, float64(600), profile.Metrics[constant.UpstreamMetricRPM].Full)
	assert.Equal(t, float64(1_000), profile.Metrics[constant.UpstreamMetricTPM].Admission)
	assert.Equal(t, float64(6_000_000), profile.Metrics[constant.UpstreamMetricTPM].Full)
	assert.Equal(t, float64(2_500), profile.Metrics[constant.UpstreamMetricTTFTP50_8K].Admission)
	assert.Equal(t, float64(1_000), profile.Metrics[constant.UpstreamMetricTTFTP50_8K].Full)

	updatedMetrics := profile.Metrics
	metric := updatedMetrics[constant.UpstreamMetricTTFTP50_8K]
	metric.Admission = 1500
	metric.Full = 400
	updatedMetrics[constant.UpstreamMetricTTFTP50_8K] = metric
	updated, err := SaveUpstreamBenchmarkProfile(dto.UpstreamBenchmarkProfileRequest{Metrics: updatedMetrics})
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Version)

	bad := updatedMetrics
	badMetric := bad[constant.UpstreamMetricTTFTP50_8K]
	badMetric.Full = 2000
	bad[constant.UpstreamMetricTTFTP50_8K] = badMetric
	_, err = SaveUpstreamBenchmarkProfile(dto.UpstreamBenchmarkProfileRequest{Metrics: bad})
	assert.ErrorContains(t, err, "full score must be <= admission")
	_ = db
}

func TestDecodeUpstreamBenchmarkProfileNormalizesLegacyMetrics(t *testing.T) {
	raw := `{"name":"general","version":1,"metrics":{"TTFT_P50_8K":{"admission":2000,"full":500,"weight":1,"enabled":true,"direction":"lower"}}}`
	config, err := decodeUpstreamBenchmarkProfile(raw)
	require.NoError(t, err)
	require.Len(t, config.Metrics, 9)
	for _, metric := range config.Metrics {
		assert.True(t, metric.Enabled)
		assert.Positive(t, metric.Admission)
		assert.Positive(t, metric.Full)
		assert.Positive(t, metric.Weight)
	}
	assert.Equal(t, float64(75), config.Metrics[constant.UpstreamMetricRPM].Admission)
	assert.Equal(t, float64(6_000_000), config.Metrics[constant.UpstreamMetricTPM].Full)
	assert.Equal(t, float64(30_000), config.Metrics[constant.UpstreamMetricTTFTP50_128K].Admission)
}

func TestDecodeUpstreamBenchmarkProfileNormalizesReversedThresholds(t *testing.T) {
	raw := `{"name":"general","version":2,"metrics":{"TPM":{"admission":6000000,"full":1000,"weight":1,"enabled":true,"direction":"higher"}}}`
	config, err := decodeUpstreamBenchmarkProfile(raw)
	require.NoError(t, err)

	assert.Equal(t, float64(1_000), config.Metrics[constant.UpstreamMetricTPM].Admission)
	assert.Equal(t, float64(6_000_000), config.Metrics[constant.UpstreamMetricTPM].Full)
}

func TestUpstreamAutoSyncConfigValidation(t *testing.T) {
	if _, err := UpdateUpstreamAutoSyncConfig(dto.UpstreamAutoSyncConfig{Enabled: true, MinScore: 80}); err == nil {
		t.Fatal("expected channel tag validation error")
	}
	if _, err := UpdateUpstreamAutoSyncConfig(dto.UpstreamAutoSyncConfig{MinScore: 101}); err == nil {
		t.Fatal("expected score range validation error")
	}
}

func TestCandidateResponseNeverContainsAPIKey(t *testing.T) {
	response := CandidateResponse(&model.UpstreamCandidate{ID: 1, APIKey: "secret"})
	assert.True(t, response.HasAPIKey)
	data, err := common.Marshal(response)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "secret")
}
