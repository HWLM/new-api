package service

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var ErrUpstreamNotFound = errors.New("upstream not found")

func NormalizeUpstreamBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("valid http or https base_url is required")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("base_url must use http or https")
	}
	if parsed.User != nil {
		return "", errors.New("base_url must not contain credentials")
	}
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func ValidateUpstreamCandidateInput(input dto.UpstreamCandidateRequest) (string, error) {
	if !IsValidUpstreamChannelType(input.Type) {
		return "", errors.New("invalid channel type")
	}
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 {
		return "", errors.New("name is required and must be at most 128 characters")
	}
	if len(input.APIKey) > 8192 {
		return "", errors.New("api_key is too long")
	}
	return NormalizeUpstreamBaseURL(input.BaseURL)
}

// IsValidUpstreamChannelType keeps upstream applications aligned with the
// channel types available in the main channel configuration.
func IsValidUpstreamChannelType(channelType int) bool {
	if channelType == constant.ChannelTypeUnknown {
		return false
	}
	_, ok := constant.ChannelTypeNames[channelType]
	return ok
}

func normalizeUpstreamModels(models string) string {
	models = strings.ReplaceAll(models, "\uFF0C", ",")
	parts := strings.FieldsFunc(models, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	seen := make(map[string]struct{}, len(parts))
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		modelName := strings.TrimSpace(part)
		if modelName == "" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		normalized = append(normalized, modelName)
	}
	return strings.Join(normalized, ",")
}

func validateBusinessCooperationModels(models string) (string, error) {
	normalized := normalizeUpstreamModels(models)
	if normalized == "" {
		// Keep older clients compatible; the application UI requires an explicit
		// model selection, while legacy API callers receive a sensible default.
		return "gpt-4o-mini", nil
	}
	if len(normalized) > 8192 {
		return "", errors.New("models are too long")
	}
	return normalized, nil
}

func CreateUpstreamCandidate(input dto.UpstreamCandidateRequest, source string, ownerUserID *int, applicationID *int64) (*model.UpstreamCandidate, error) {
	if source != constant.UpstreamSourceAdmin && source != constant.UpstreamSourceSelf {
		return nil, errors.New("invalid upstream source")
	}
	if strings.TrimSpace(input.APIKey) == "" {
		return nil, errors.New("api_key is required")
	}
	return createUpstreamCandidate(model.DB, input, source, ownerUserID, applicationID)
}

func createUpstreamCandidate(db *gorm.DB, input dto.UpstreamCandidateRequest, source string, ownerUserID *int, applicationID *int64) (*model.UpstreamCandidate, error) {
	normalized, err := ValidateUpstreamCandidateInput(input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.APIKey) == "" {
		return nil, errors.New("api_key is required")
	}
	item := &model.UpstreamCandidate{
		Source: source, OwnerUserID: ownerUserID, ApplicationID: applicationID,
		Type: input.Type, Name: strings.TrimSpace(input.Name), BaseURL: strings.TrimSpace(input.BaseURL), Models: normalizeUpstreamModels(input.Models),
		NormalizedBaseURL: normalized, APIKey: input.APIKey, Contact: strings.TrimSpace(input.Contact), Remark: strings.TrimSpace(input.Remark),
	}
	if err := db.Create(item).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, errors.New("upstream type and base_url already exist")
		}
		return nil, err
	}
	return item, nil
}

func UpdateUpstreamCandidate(id int64, input dto.UpstreamCandidateRequest) (*model.UpstreamCandidate, error) {
	var item *model.UpstreamCandidate
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := model.LockUpstreamCandidate(tx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUpstreamNotFound
			}
			return err
		}
		if locked.Source != constant.UpstreamSourceAdmin || locked.BenchmarkStatus != constant.UpstreamBenchmarkPending {
			return errors.New("only pending admin upstreams can be edited")
		}
		normalized, err := ValidateUpstreamCandidateInput(input)
		if err != nil {
			return err
		}
		locked.Type, locked.Name, locked.BaseURL, locked.NormalizedBaseURL = input.Type, strings.TrimSpace(input.Name), strings.TrimSpace(input.BaseURL), normalized
		locked.Models = normalizeUpstreamModels(input.Models)
		locked.Contact, locked.Remark = strings.TrimSpace(input.Contact), strings.TrimSpace(input.Remark)
		if strings.TrimSpace(input.APIKey) != "" {
			locked.APIKey = input.APIKey
		}
		if err := tx.Save(locked).Error; err != nil {
			return err
		}
		item = locked
		return nil
	})
	return item, err
}

func CreateBusinessCooperation(userID int, input dto.BusinessCooperationRequest) (*model.BusinessCooperation, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user")
	}
	var item *model.BusinessCooperation
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		item = &model.BusinessCooperation{UserID: userID}
		if err := tx.Create(item).Error; err != nil {
			return err
		}
		owner := userID
		candidate, err := createUpstreamCandidate(tx, input.Upstream, constant.UpstreamSourceSelf, &owner, &item.ID)
		if err != nil {
			return err
		}
		models, err := validateBusinessCooperationModels(input.Upstream.Models)
		if err != nil {
			return err
		}
		candidate.Models = models
		if err := tx.Model(candidate).Update("models", models).Error; err != nil {
			return err
		}
		item.UpstreamID = candidate.ID
		return tx.Save(item).Error
	})
	return item, err
}

func UpdateBusinessCooperation(userID int, id int64, input dto.BusinessCooperationRequest, resubmit bool) (*model.BusinessCooperation, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user")
	}
	var application *model.BusinessCooperation
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := model.LockBusinessCooperation(tx, id, userID)
		if err != nil {
			return err
		}
		application = locked
		if resubmit {
			if application.Status != constant.BusinessCooperationRejected {
				return errors.New("only rejected applications can be resubmitted")
			}
		} else if application.Status != constant.BusinessCooperationPendingReview {
			return errors.New("only pending applications can be edited")
		}
		candidate, err := model.LockUpstreamCandidate(tx, application.UpstreamID)
		if err != nil {
			return err
		}
		normalized, err := ValidateUpstreamCandidateInput(input.Upstream)
		if err != nil {
			return err
		}
		candidate.Type = input.Upstream.Type
		candidate.Name = strings.TrimSpace(input.Upstream.Name)
		candidate.BaseURL = strings.TrimSpace(input.Upstream.BaseURL)
		candidate.NormalizedBaseURL = normalized
		candidate.Contact = strings.TrimSpace(input.Upstream.Contact)
		candidate.Remark = strings.TrimSpace(input.Upstream.Remark)
		models, err := validateBusinessCooperationModels(input.Upstream.Models)
		if err != nil {
			return err
		}
		candidate.Models = models
		if strings.TrimSpace(input.Upstream.APIKey) != "" {
			candidate.APIKey = input.Upstream.APIKey
		}
		if resubmit {
			candidate.BenchmarkStatus = constant.UpstreamBenchmarkPending
			candidate.LatestRunID = nil
		}
		if err := tx.Save(candidate).Error; err != nil {
			return err
		}
		if resubmit {
			application.Status = constant.BusinessCooperationPendingReview
			application.RejectReason = ""
			application.Revision++
			application.SubmittedTime = common.GetTimestamp()
			application.ReviewedTime = 0
			return tx.Save(application).Error
		}
		return nil
	})
	return application, err
}

func DeleteBusinessCooperation(userID int, id int64) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		application, err := model.LockBusinessCooperation(tx, id, userID)
		if err != nil {
			return err
		}
		if application.Status != constant.BusinessCooperationPendingReview {
			return errors.New("only pending applications can be deleted")
		}
		if err := tx.Where("upstream_id = ?", application.UpstreamID).Delete(&model.UpstreamBenchmarkRun{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.UpstreamCandidate{}, application.UpstreamID).Error; err != nil {
			return err
		}
		return tx.Delete(application).Error
	})
}

func CandidateResponse(item *model.UpstreamCandidate) dto.UpstreamCandidateResponse {
	response := dto.UpstreamCandidateResponse{ID: item.ID, Source: item.Source, OwnerUserID: item.OwnerUserID, ApplicationID: item.ApplicationID, Type: item.Type, Name: item.Name, BaseURL: item.BaseURL, Models: item.Models, Contact: item.Contact, Remark: item.Remark, BenchmarkStatus: item.BenchmarkStatus, LatestRunID: item.LatestRunID, ChannelID: item.ChannelID, SyncedTime: item.SyncedTime, CreatedTime: item.CreatedTime, UpdatedTime: item.UpdatedTime, HasAPIKey: item.HasAPIKey()}
	if item.ApplicationID != nil {
		if application, err := model.GetBusinessCooperation(*item.ApplicationID); err == nil && application != nil {
			response.ApplicationStatus = application.Status
		}
	}
	if item.LatestRunID != nil {
		if run, err := model.GetUpstreamBenchmarkRun(*item.LatestRunID); err == nil && run != nil {
			response.BenchmarkStatus = benchmarkStatusFromRun(run.Status, item.BenchmarkStatus)
			response.LatestBenchmark = benchmarkRunResponse(run)
		}
	}
	return response
}

func benchmarkStatusFromRun(runStatus, fallback string) string {
	switch runStatus {
	case constant.UpstreamBenchmarkRunPending:
		return constant.UpstreamBenchmarkPending
	case constant.UpstreamBenchmarkRunRunning:
		return constant.UpstreamBenchmarkRunning
	case constant.UpstreamBenchmarkRunSucceeded:
		return constant.UpstreamBenchmarkCompleted
	case constant.UpstreamBenchmarkRunFailed, constant.UpstreamBenchmarkRunCancelled:
		return constant.UpstreamBenchmarkPending
	default:
		return fallback
	}
}

func benchmarkRunResponse(run *model.UpstreamBenchmarkRun) *dto.UpstreamBenchmarkResponse {
	response := &dto.UpstreamBenchmarkResponse{
		ID: run.ID, Status: run.Status, Concurrency: run.Concurrency, Model: run.Model, OverallScore: run.OverallScore,
		StatusCode: run.StatusCode, LatencyMS: run.LatencyMS, UnmetCount: run.UnmetCount,
		AutoSyncStatus: run.AutoSyncStatus, AutoSyncedTime: run.AutoSyncedTime,
		StartedTime: run.StartedTime, FinishedTime: run.FinishedTime, DurationMS: run.DurationMS,
		ProfileName: run.ProfileName, ProfileVersion: run.ProfileVersion,
	}
	if run.Metrics != "" {
		_ = common.UnmarshalJsonStr(run.Metrics, &response.Metrics)
		// Older runs stored the complete result in metrics; keep them readable.
		if response.StatusCode == 0 {
			var legacy UpstreamBenchmarkResult
			if common.UnmarshalJsonStr(run.Metrics, &legacy) == nil && legacy.StatusCode != 0 {
				response.StatusCode = legacy.StatusCode
				response.LatencyMS = legacy.LatencyMS
			}
		}
	}
	if run.Scores != "" {
		_ = common.UnmarshalJsonStr(run.Scores, &response.Scores)
	}
	baselineSnapshot := run.BaselineSnapshot
	if strings.TrimSpace(baselineSnapshot) == "" {
		baselineSnapshot = run.ProfileSnapshot
	}
	profile := defaultUpstreamBenchmarkProfileConfig()
	if strings.TrimSpace(baselineSnapshot) != "" {
		var storedProfile UpstreamBenchmarkProfileConfig
		if common.UnmarshalJsonStr(baselineSnapshot, &storedProfile) == nil && len(storedProfile.Metrics) > 0 {
			profile = normalizeUpstreamBenchmarkProfileConfig(storedProfile)
		}
	}
	response.Baseline = make(map[string]dto.UpstreamBenchmarkMetric, len(profile.Metrics))
	for name, metric := range profile.Metrics {
		response.Baseline[name] = dto.UpstreamBenchmarkMetric{
			Admission: metric.Admission, Full: metric.Full, Weight: metric.Weight,
			Enabled: metric.Enabled, Direction: metric.Direction,
		}
	}
	return response
}

func ListUpstreamBenchmarkResponses(upstreamID int64, limit int, includeError bool) ([]*dto.UpstreamBenchmarkRunResponse, error) {
	runs, err := model.ListUpstreamBenchmarkRuns(upstreamID, limit)
	if err != nil {
		return nil, err
	}
	responses := make([]*dto.UpstreamBenchmarkRunResponse, 0, len(runs))
	for _, run := range runs {
		base := benchmarkRunResponse(run)
		response := &dto.UpstreamBenchmarkRunResponse{UpstreamBenchmarkResponse: *base}
		if includeError && run.Error != "" {
			response.Error = common.MaskSensitiveInfo(run.Error)
		}
		if includeError && run.AutoSyncError != "" {
			response.AutoSyncError = common.MaskSensitiveInfo(run.AutoSyncError)
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func BusinessCooperationResponse(item *model.BusinessCooperation) (dto.BusinessCooperationResponse, error) {
	candidate, err := model.GetUpstreamCandidate(item.UpstreamID)
	if err != nil {
		return dto.BusinessCooperationResponse{}, err
	}
	if candidate == nil {
		return dto.BusinessCooperationResponse{}, ErrUpstreamNotFound
	}
	return dto.BusinessCooperationResponse{ID: item.ID, UserID: item.UserID, UpstreamID: item.UpstreamID, Status: item.Status, RejectReason: item.RejectReason, Revision: item.Revision, CreatedTime: item.CreatedTime, UpdatedTime: item.UpdatedTime, SubmittedTime: item.SubmittedTime, ReviewedTime: item.ReviewedTime, Upstream: CandidateResponse(candidate)}, nil
}

func SanitizeUpstreamError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("upstream operation failed: %s", common.MaskSensitiveInfo(err.Error()))
}
