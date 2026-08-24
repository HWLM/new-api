package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type UpstreamBenchmarkPayload struct {
	UpstreamID      int64  `json:"upstream_id"`
	RunID           int64  `json:"run_id"`
	Model           string `json:"model,omitempty"`
	Concurrency     int    `json:"concurrency"`
	ProfileName     string `json:"profile_name"`
	ProfileVersion  int    `json:"profile_version"`
	ProfileSnapshot string `json:"profile_snapshot"`
}

type UpstreamBenchmarkState struct {
	Progress int    `json:"progress"`
	Stage    string `json:"stage,omitempty"`
}

type UpstreamBenchmarkResult struct {
	StatusCode int                `json:"status_code"`
	LatencyMS  int64              `json:"latency_ms"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Scores     map[string]float64 `json:"scores,omitempty"`
	UnmetCount int                `json:"unmet_count,omitempty"`
	Score      float64            `json:"score"`
}

var (
	ErrUpstreamBenchmarkBusy       = errors.New("another upstream benchmark is already active")
	ErrUpstreamBenchmarkNotRunning = errors.New("upstream benchmark is not running")
)

const upstreamBenchmarkExecutionTimeout = 15 * time.Minute

func SupportsUpstreamBenchmark(channelType int) bool {
	return IsValidUpstreamChannelType(channelType)
}

func EnqueueUpstreamBenchmark(upstreamID int64, modelName string) (*model.SystemTask, error) {
	return EnqueueUpstreamBenchmarkWithOptions(upstreamID, modelName, 1)
}

func EnqueueUpstreamBenchmarkWithOptions(upstreamID int64, modelName string, concurrency int) (*model.SystemTask, error) {
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 100 {
		return nil, errors.New("benchmark concurrency must be between 1 and 100")
	}
	var task *model.SystemTask
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		candidate, err := model.LockUpstreamCandidate(tx, upstreamID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUpstreamNotFound
			}
			return err
		}
		if !candidate.HasAPIKey() {
			return errors.New("upstream api_key is required")
		}
		if !SupportsUpstreamBenchmark(candidate.Type) {
			return errors.New("benchmark is not supported for this channel type")
		}
		if candidate.ApplicationID != nil && candidate.ChannelID == nil {
			application, err := model.LockBusinessCooperationByID(tx, *candidate.ApplicationID)
			if err != nil {
				return err
			}
			if application.Status == constant.BusinessCooperationRejected {
				return errors.New("rejected applications must be resubmitted before benchmarking")
			}
		}

		active, err := model.GetActiveUpstreamBenchmarkSystemTask(tx)
		if err != nil {
			return err
		}
		if active != nil {
			var activePayload UpstreamBenchmarkPayload
			if err := active.DecodePayload(&activePayload); err == nil && activePayload.UpstreamID == upstreamID {
				task = active
				return nil
			}
			return ErrUpstreamBenchmarkBusy
		}

		run := &model.UpstreamBenchmarkRun{UpstreamID: upstreamID, Status: constant.UpstreamBenchmarkRunPending}
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		profile, profileConfig, err := GetOrCreateUpstreamBenchmarkProfile(tx)
		if err != nil {
			return err
		}
		profileSnapshot, err := common.Marshal(profileConfig)
		if err != nil {
			return err
		}
		payload := UpstreamBenchmarkPayload{
			UpstreamID: upstreamID, RunID: run.ID, Model: strings.TrimSpace(modelName),
			Concurrency: concurrency,
			ProfileName: profile.Name, ProfileVersion: profile.Version, ProfileSnapshot: string(profileSnapshot),
		}
		if err := tx.Model(run).Updates(map[string]any{
			"concurrency":  concurrency,
			"model":        strings.TrimSpace(modelName),
			"profile_name": profile.Name, "profile_version": profile.Version,
			"profile_snapshot": string(profileSnapshot), "baseline_snapshot": string(profileSnapshot),
		}).Error; err != nil {
			return err
		}
		task, err = model.CreateUpstreamBenchmarkSystemTask(tx, payload, UpstreamBenchmarkState{Stage: "pending"})
		if err != nil {
			return err
		}
		if err := tx.Model(run).Update("system_task_id", task.TaskID).Error; err != nil {
			return err
		}
		if err := tx.Model(candidate).Updates(map[string]any{
			"benchmark_status": constant.UpstreamBenchmarkRunning,
			"latest_run_id":    run.ID,
		}).Error; err != nil {
			return err
		}
		if candidate.ApplicationID != nil && candidate.ChannelID == nil {
			return tx.Model(&model.BusinessCooperation{}).
				Where("id = ?", *candidate.ApplicationID).
				Update("status", constant.BusinessCooperationBenchmarkRunning).Error
		}
		return nil
	})
	return task, err
}

func CancelUpstreamBenchmark(upstreamID int64) error {
	candidateSnapshot, err := model.GetUpstreamCandidate(upstreamID)
	if err != nil {
		return err
	}
	if candidateSnapshot == nil {
		return ErrUpstreamNotFound
	}
	if candidateSnapshot.BenchmarkStatus != constant.UpstreamBenchmarkRunning || candidateSnapshot.LatestRunID == nil {
		return ErrUpstreamBenchmarkNotRunning
	}
	runSnapshot, err := model.GetUpstreamBenchmarkRun(*candidateSnapshot.LatestRunID)
	if err != nil {
		return err
	}
	if runSnapshot == nil || runSnapshot.SystemTaskID == "" {
		return ErrUpstreamBenchmarkNotRunning
	}

	return model.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := model.LockUpstreamBenchmarkLeaseByTaskID(tx, runSnapshot.SystemTaskID); err != nil {
			return err
		}
		run, err := model.LockUpstreamBenchmarkRun(tx, runSnapshot.ID)
		if err != nil {
			return err
		}
		candidate, err := model.LockUpstreamCandidate(tx, upstreamID)
		if err != nil {
			return err
		}
		if candidate.BenchmarkStatus != constant.UpstreamBenchmarkRunning || candidate.LatestRunID == nil || *candidate.LatestRunID != run.ID {
			return ErrUpstreamBenchmarkNotRunning
		}
		if run.Status != constant.UpstreamBenchmarkRunPending && run.Status != constant.UpstreamBenchmarkRunRunning {
			return ErrUpstreamBenchmarkNotRunning
		}
		task, err := model.LockUpstreamBenchmarkSystemTask(tx, run.SystemTaskID)
		if err != nil {
			return err
		}
		if task.Status != model.SystemTaskStatusPending && task.Status != model.SystemTaskStatusRunning {
			return ErrUpstreamBenchmarkNotRunning
		}

		now := common.GetTimestamp()
		state, err := common.Marshal(UpstreamBenchmarkState{Progress: 100, Stage: "cancelled"})
		if err != nil {
			return err
		}
		durationMS := int64(0)
		if run.StartedTime > 0 {
			durationMS = (now - run.StartedTime) * 1000
		}
		if err := tx.Model(run).Updates(map[string]any{
			"status":        constant.UpstreamBenchmarkRunCancelled,
			"finished_time": now,
			"duration_ms":   durationMS,
			"error":         "",
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(candidate).Update("benchmark_status", constant.UpstreamBenchmarkPending).Error; err != nil {
			return err
		}
		if candidate.ApplicationID != nil && candidate.ChannelID == nil {
			application, err := model.LockBusinessCooperationByID(tx, *candidate.ApplicationID)
			if err != nil {
				return err
			}
			if application.Status == constant.BusinessCooperationBenchmarkRunning {
				if err := tx.Model(application).Update("status", constant.BusinessCooperationPendingReview).Error; err != nil {
					return err
				}
			}
		}
		updated := tx.Model(task).
			Where("status IN ?", []model.SystemTaskStatus{model.SystemTaskStatusPending, model.SystemTaskStatusRunning}).
			Updates(map[string]any{
				"status":     model.SystemTaskStatusCancelled,
				"active_key": nil,
				"state":      string(state),
				"error":      "",
				"updated_at": now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return ErrUpstreamBenchmarkNotRunning
		}
		return tx.Where("type = ? AND task_id = ?", constant.SystemTaskTypeUpstreamBenchmark, task.TaskID).
			Delete(&model.SystemTaskLock{}).Error
	})
}

func RunUpstreamBenchmarkTask(ctx context.Context, task *model.SystemTask, runnerID string) {
	benchmarkCtx, cancel := context.WithTimeout(ctx, upstreamBenchmarkExecutionTimeout)
	defer cancel()
	payload := UpstreamBenchmarkPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishUpstreamBenchmarkTask(task, runnerID, payload, err)
		return
	}
	candidate, err := model.GetUpstreamCandidate(payload.UpstreamID)
	if err != nil || candidate == nil {
		if err == nil {
			err = ErrUpstreamNotFound
		}
		finishUpstreamBenchmarkTask(task, runnerID, payload, err)
		return
	}
	run, err := model.GetUpstreamBenchmarkRun(payload.RunID)
	if err != nil || run == nil {
		if err == nil {
			err = errors.New("benchmark run not found")
		}
		finishUpstreamBenchmarkTask(task, runnerID, payload, err)
		return
	}
	now := common.GetTimestamp()
	if err := model.DB.Model(run).Updates(map[string]any{"status": constant.UpstreamBenchmarkRunRunning, "started_time": now}).Error; err != nil {
		finishUpstreamBenchmarkTask(task, runnerID, payload, err)
		return
	}
	if err := model.UpdateSystemTaskState(task.TaskID, runnerID, UpstreamBenchmarkState{Progress: 10, Stage: "request"}); err != nil {
		finishUpstreamBenchmarkTask(task, runnerID, payload, err)
		return
	}
	result, runErr := executeUpstreamWorkload(benchmarkCtx, candidate, payload.Model, payload.Concurrency)
	if runErr != nil {
		finishUpstreamBenchmarkTask(task, runnerID, payload, runErr)
		return
	}
	if payload.ProfileSnapshot != "" {
		profileConfig, err := decodeUpstreamBenchmarkProfile(payload.ProfileSnapshot)
		if err != nil {
			finishUpstreamBenchmarkTask(task, runnerID, payload, err)
			return
		}
		if len(result.Metrics) == 0 {
			result.Metrics = map[string]float64{constant.UpstreamMetricTTFTP50_8K: float64(result.LatencyMS)}
		}
		result.Score, result.Scores, result.UnmetCount = CalculateUpstreamBenchmarkScore(result.Metrics, profileConfig)
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			result.Score = 0
		}
	}
	if err := completeUpstreamBenchmarkTask(task, runnerID, payload, &result, nil); err != nil {
		common.SysError(fmt.Sprintf("failed to complete upstream benchmark task: %v", err))
	} else {
		config := GetUpstreamAutoSyncConfig()
		if !config.Enabled || result.Score < config.MinScore {
			_ = model.DB.Model(&model.UpstreamBenchmarkRun{}).Where("id = ?", payload.RunID).Updates(map[string]any{"auto_sync_status": "skipped"}).Error
		} else if err := TryAutoSyncUpstream(payload.UpstreamID, result.Score); err != nil {
			message := common.MaskSensitiveInfo(err.Error())
			_ = model.DB.Model(&model.UpstreamBenchmarkRun{}).Where("id = ?", payload.RunID).Updates(map[string]any{"auto_sync_status": "failed", "auto_sync_error": message}).Error
			common.SysError(fmt.Sprintf("automatic upstream synchronization failed: %s", message))
		} else {
			_ = model.DB.Model(&model.UpstreamBenchmarkRun{}).Where("id = ?", payload.RunID).Updates(map[string]any{"auto_sync_status": "succeeded", "auto_synced_time": common.GetTimestamp(), "auto_sync_error": ""}).Error
		}
	}
}

func executeUpstreamProbe(ctx context.Context, candidate *model.UpstreamCandidate, modelName string, concurrency int) (UpstreamBenchmarkResult, error) {
	if candidate == nil {
		return UpstreamBenchmarkResult{}, ErrUpstreamNotFound
	}
	if !SupportsUpstreamBenchmark(candidate.Type) {
		return UpstreamBenchmarkResult{}, errors.New("benchmark is not supported for this channel type")
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 100 {
		return UpstreamBenchmarkResult{}, errors.New("benchmark concurrency must be between 1 and 100")
	}
	selectedModel := strings.TrimSpace(modelName)
	if selectedModel == "" {
		configured := configuredUpstreamModels(candidate.Models)
		if len(configured) > 0 {
			selectedModel = configured[0]
		}
	}
	if selectedModel != "" {
		models := configuredUpstreamModels(candidate.Models)
		if candidate.Type == constant.ChannelTypeOpenAI || candidate.Type == constant.ChannelTypeGemini {
			var err error
			models, err = DiscoverUpstreamModels(ctx, candidate)
			if err != nil {
				return UpstreamBenchmarkResult{}, err
			}
		}
		found := false
		for _, available := range models {
			if available == selectedModel {
				found = true
				break
			}
		}
		if len(models) > 0 && !found {
			return UpstreamBenchmarkResult{}, errors.New("selected model is not available from upstream")
		}
	}
	client := GetHttpClient()
	if candidate.Source == constant.UpstreamSourceSelf {
		client = GetSSRFProtectedHTTPClient()
	}
	latencies := make([]int64, 0, concurrency)
	statusCodes := make([]int, 0, concurrency)
	errorsFound := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	var mutex sync.Mutex
	for i := 0; i < concurrency; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			request, err := newUpstreamBenchmarkRequest(ctx, candidate, selectedModel)
			if err != nil {
				errorsFound <- err
				return
			}
			started := time.Now()
			response, err := client.Do(request)
			if err != nil {
				errorsFound <- err
				return
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 2<<20))
			_ = response.Body.Close()
			mutex.Lock()
			latencies = append(latencies, time.Since(started).Milliseconds())
			statusCodes = append(statusCodes, response.StatusCode)
			mutex.Unlock()
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	if len(latencies) == 0 {
		for err := range errorsFound {
			if err != nil {
				return UpstreamBenchmarkResult{}, err
			}
		}
		return UpstreamBenchmarkResult{}, errors.New("upstream probe returned no responses")
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	statusCode := statusCodes[0]
	for _, code := range statusCodes {
		if code < 200 || code >= 300 {
			statusCode = code
			break
		}
	}
	latency := latencies[(len(latencies)-1)/2]
	p90Index := (len(latencies)*9 + 9) / 10
	if p90Index > len(latencies) {
		p90Index = len(latencies)
	}
	p90 := latencies[p90Index-1]
	score := 0.0
	if statusCode >= 200 && statusCode < 300 && len(latencies) == concurrency {
		score = 100
	}
	return UpstreamBenchmarkResult{
		StatusCode: statusCode, LatencyMS: latency, Score: score,
		Metrics: map[string]float64{
			constant.UpstreamMetricTTFTP50_8K: float64(latency),
			constant.UpstreamMetricTTFTP90_8K: float64(p90),
		},
	}, nil
}

func newUpstreamBenchmarkRequest(ctx context.Context, candidate *model.UpstreamCandidate, modelName string) (*http.Request, error) {
	if candidate.Type == constant.ChannelTypeOpenAI || candidate.Type == constant.ChannelTypeGemini {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamModelListEndpoint(candidate), nil)
		if err != nil {
			return nil, err
		}
		setUpstreamAuthentication(request, candidate)
		return request, nil
	}

	baseURL := strings.TrimRight(candidate.NormalizedBaseURL, "/")
	endpoint := baseURL + "/v1/chat/completions"
	if strings.HasSuffix(baseURL, "/v1") {
		endpoint = baseURL + "/chat/completions"
	}
	payload := map[string]any{
		"model": modelName,
		"messages": []map[string]string{{
			"role":    "user",
			"content": "ping",
		}},
		"max_tokens": 1,
	}
	if candidate.Type == constant.ChannelTypeAnthropic {
		endpoint = baseURL + "/v1/messages"
		if strings.HasSuffix(baseURL, "/v1") {
			endpoint = baseURL + "/messages"
		}
		payload = map[string]any{
			"model":      modelName,
			"max_tokens": 1,
			"messages": []map[string]any{{
				"role":    "user",
				"content": []map[string]string{{"type": "text", "text": "ping"}},
			}},
		}
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	setUpstreamAuthentication(request, candidate)
	return request, nil
}

func finishUpstreamBenchmarkTask(task *model.SystemTask, runnerID string, payload UpstreamBenchmarkPayload, runErr error) {
	if runErr == nil {
		runErr = errors.New("benchmark failed")
	}
	if err := completeUpstreamBenchmarkTask(task, runnerID, payload, nil, runErr); err != nil {
		common.SysError(fmt.Sprintf("failed to finish upstream benchmark task: %v", err))
	}
}

func completeUpstreamBenchmarkTask(task *model.SystemTask, runnerID string, payload UpstreamBenchmarkPayload, result *UpstreamBenchmarkResult, runErr error) error {
	now := common.GetTimestamp()
	succeeded := runErr == nil && result != nil
	message := ""
	if !succeeded {
		if runErr == nil {
			runErr = errors.New("benchmark failed")
		}
		message = common.MaskSensitiveInfo(runErr.Error())
	}
	resultBytes, err := common.Marshal(result)
	if err != nil {
		return err
	}
	stateBytes, err := common.Marshal(UpstreamBenchmarkState{Progress: 100, Stage: map[bool]string{true: "completed", false: "failed"}[succeeded]})
	if err != nil {
		return err
	}

	return model.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := model.LockUpstreamBenchmarkLease(tx, task.TaskID, runnerID, now); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return model.ErrSystemTaskLockLost
			}
			return err
		}

		if payload.RunID > 0 {
			run, err := model.LockUpstreamBenchmarkRun(tx, payload.RunID)
			if err != nil {
				return err
			}
			durationMS := int64(0)
			if run.StartedTime > 0 {
				durationMS = (now - run.StartedTime) * 1000
			}
			updates := map[string]any{
				"finished_time": now,
				"duration_ms":   durationMS,
			}
			if succeeded {
				metricsBytes, err := common.Marshal(result.Metrics)
				if err != nil {
					return err
				}
				scoresBytes, err := common.Marshal(result.Scores)
				if err != nil {
					return err
				}
				updates["status"] = constant.UpstreamBenchmarkRunSucceeded
				updates["metrics"] = string(metricsBytes)
				updates["scores"] = string(scoresBytes)
				updates["status_code"] = result.StatusCode
				updates["latency_ms"] = result.LatencyMS
				updates["unmet_count"] = result.UnmetCount
				updates["overall_score"] = result.Score
				updates["auto_sync_status"] = "pending"
				updates["error"] = ""
			} else {
				updates["status"] = constant.UpstreamBenchmarkRunFailed
				updates["error"] = message
			}
			if err := tx.Model(run).Updates(updates).Error; err != nil {
				return err
			}
		}

		if payload.UpstreamID > 0 {
			candidate, err := model.LockUpstreamCandidate(tx, payload.UpstreamID)
			if err != nil {
				return err
			}
			if candidate.LatestRunID == nil || *candidate.LatestRunID != payload.RunID {
				return model.ErrSystemTaskLockLost
			}
			benchmarkStatus := constant.UpstreamBenchmarkPending
			if succeeded {
				benchmarkStatus = constant.UpstreamBenchmarkCompleted
			}
			if err := tx.Model(candidate).Update("benchmark_status", benchmarkStatus).Error; err != nil {
				return err
			}
			if candidate.ApplicationID != nil && candidate.ChannelID == nil {
				application, err := model.LockBusinessCooperationByID(tx, *candidate.ApplicationID)
				if err != nil {
					return err
				}
				if application.Status == constant.BusinessCooperationBenchmarkRunning {
					applicationStatus := constant.BusinessCooperationPendingReview
					if succeeded {
						applicationStatus = constant.BusinessCooperationPendingDecision
					}
					if err := tx.Model(application).Update("status", applicationStatus).Error; err != nil {
						return err
					}
				}
			}
		}

		taskStatus := model.SystemTaskStatusFailed
		if succeeded {
			taskStatus = model.SystemTaskStatusSucceeded
		}
		updated := tx.Model(&model.SystemTask{}).
			Where("task_id = ? AND status = ? AND locked_by = ?", task.TaskID, model.SystemTaskStatusRunning, runnerID).
			Updates(map[string]any{
				"status":     taskStatus,
				"active_key": nil,
				"state":      string(stateBytes),
				"result":     string(resultBytes),
				"error":      message,
				"updated_at": now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return model.ErrSystemTaskLockLost
		}
		return tx.Where("type = ? AND task_id = ? AND locked_by = ?", constant.SystemTaskTypeUpstreamBenchmark, task.TaskID, runnerID).
			Delete(&model.SystemTaskLock{}).Error
	})
}

func RecoverExpiredUpstreamBenchmarkTasks(now int64) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		leases, err := model.LockExpiredUpstreamBenchmarkLeases(tx, now)
		if err != nil {
			return err
		}
		for _, lease := range leases {
			var task model.SystemTask
			err := tx.Where("task_id = ? AND type = ? AND status = ?", lease.TaskID, constant.SystemTaskTypeUpstreamBenchmark, model.SystemTaskStatusRunning).First(&task).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Delete(lease).Error; err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}

			var payload UpstreamBenchmarkPayload
			_ = task.DecodePayload(&payload)
			message := "task lease expired"
			if payload.RunID > 0 {
				if err := tx.Model(&model.UpstreamBenchmarkRun{}).Where("id = ? AND status = ?", payload.RunID, constant.UpstreamBenchmarkRunRunning).Updates(map[string]any{
					"status":        constant.UpstreamBenchmarkRunFailed,
					"error":         message,
					"finished_time": now,
				}).Error; err != nil {
					return err
				}
			}
			if payload.UpstreamID > 0 {
				var candidate model.UpstreamCandidate
				if err := tx.Where("id = ?", payload.UpstreamID).First(&candidate).Error; err == nil {
					if candidate.LatestRunID != nil && *candidate.LatestRunID == payload.RunID {
						if err := tx.Model(&candidate).Update("benchmark_status", constant.UpstreamBenchmarkPending).Error; err != nil {
							return err
						}
						if candidate.ApplicationID != nil && candidate.ChannelID == nil {
							if err := tx.Model(&model.BusinessCooperation{}).
								Where("id = ? AND status = ?", *candidate.ApplicationID, constant.BusinessCooperationBenchmarkRunning).
								Update("status", constant.BusinessCooperationPendingReview).Error; err != nil {
								return err
							}
						}
					}
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}
			if err := tx.Model(&task).Updates(map[string]any{
				"status":     model.SystemTaskStatusFailed,
				"active_key": nil,
				"error":      message,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
			if err := tx.Delete(lease).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func RunUpstreamBenchmarkWorker(ctx context.Context, runnerID string, pollInterval, lockSeconds int) error {
	if runnerID == "" {
		return errors.New("worker runner id is required")
	}
	if pollInterval <= 0 {
		pollInterval = 2
	}
	if lockSeconds < 30 {
		lockSeconds = 300
	}
	ticker := time.NewTicker(time.Duration(pollInterval) * time.Second)
	defer ticker.Stop()
	for {
		if err := claimAndRunUpstreamBenchmark(ctx, runnerID, int64(lockSeconds)); err != nil {
			common.SysError(fmt.Sprintf("upstream benchmark worker pass failed: %v", err))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func claimAndRunUpstreamBenchmark(ctx context.Context, runnerID string, lockSeconds int64) error {
	if err := RecoverExpiredUpstreamBenchmarkTasks(common.GetTimestamp()); err != nil {
		return err
	}
	tasks, err := model.FindPendingSystemTasks(constant.SystemTaskTypeUpstreamBenchmark, 1)
	if err != nil || len(tasks) == 0 {
		return err
	}
	task, claimed, err := model.ClaimSystemTask(tasks[0].ID, constant.SystemTaskTypeUpstreamBenchmark, runnerID, common.GetTimestamp()+lockSeconds)
	if err != nil || !claimed {
		return err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	interval := time.Duration(lockSeconds/3) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if err := model.RenewSystemTaskLock(task.TaskID, runnerID, common.GetTimestamp()+lockSeconds); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	RunUpstreamBenchmarkTask(workerCtx, task, runnerID)
	close(done)
	return nil
}
