package dto

type UpstreamCandidateRequest struct {
	Type    int    `json:"type"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Models  string `json:"models"`
	Contact string `json:"contact"`
	Remark  string `json:"remark"`
}

type UpstreamCandidateResponse struct {
	ID                int64                      `json:"id"`
	Source            string                     `json:"source"`
	OwnerUserID       *int                       `json:"owner_user_id,omitempty"`
	ApplicationID     *int64                     `json:"application_id,omitempty"`
	ApplicationStatus string                     `json:"application_status,omitempty"`
	Type              int                        `json:"type"`
	Name              string                     `json:"name"`
	BaseURL           string                     `json:"base_url"`
	Models            string                     `json:"models"`
	Contact           string                     `json:"contact"`
	Remark            string                     `json:"remark"`
	BenchmarkStatus   string                     `json:"benchmark_status"`
	LatestRunID       *int64                     `json:"latest_run_id,omitempty"`
	ChannelID         *int                       `json:"channel_id,omitempty"`
	SyncedTime        int64                      `json:"synced_time,omitempty"`
	CreatedTime       int64                      `json:"created_time"`
	UpdatedTime       int64                      `json:"updated_time"`
	HasAPIKey         bool                       `json:"has_api_key"`
	LatestBenchmark   *UpstreamBenchmarkResponse `json:"latest_benchmark,omitempty"`
}

type UpstreamBenchmarkResponse struct {
	ID             int64                              `json:"id"`
	Status         string                             `json:"status"`
	Concurrency    int                                `json:"concurrency,omitempty"`
	Model          string                             `json:"model,omitempty"`
	StatusCode     int                                `json:"status_code,omitempty"`
	LatencyMS      int64                              `json:"latency_ms,omitempty"`
	OverallScore   float64                            `json:"overall_score,omitempty"`
	Metrics        map[string]float64                 `json:"metrics,omitempty"`
	Scores         map[string]float64                 `json:"scores,omitempty"`
	UnmetCount     int                                `json:"unmet_count,omitempty"`
	AutoSyncStatus string                             `json:"auto_sync_status,omitempty"`
	AutoSyncError  string                             `json:"auto_sync_error,omitempty"`
	AutoSyncedTime int64                              `json:"auto_synced_time,omitempty"`
	StartedTime    int64                              `json:"started_time,omitempty"`
	FinishedTime   int64                              `json:"finished_time,omitempty"`
	DurationMS     int64                              `json:"duration_ms,omitempty"`
	ProfileName    string                             `json:"profile_name,omitempty"`
	ProfileVersion int                                `json:"profile_version,omitempty"`
	Baseline       map[string]UpstreamBenchmarkMetric `json:"baseline,omitempty"`
}

type UpstreamBenchmarkRunResponse struct {
	UpstreamBenchmarkResponse
	Error string `json:"error,omitempty"`
}

type BusinessCooperationRequest struct {
	Upstream UpstreamCandidateRequest `json:"upstream"`
}

type BusinessCooperationResponse struct {
	ID            int64                     `json:"id"`
	UserID        int                       `json:"user_id"`
	UpstreamID    int64                     `json:"upstream_id"`
	Status        string                    `json:"status"`
	RejectReason  string                    `json:"reject_reason,omitempty"`
	Revision      int                       `json:"revision"`
	CreatedTime   int64                     `json:"created_time"`
	UpdatedTime   int64                     `json:"updated_time"`
	SubmittedTime int64                     `json:"submitted_time"`
	ReviewedTime  int64                     `json:"reviewed_time,omitempty"`
	Upstream      UpstreamCandidateResponse `json:"upstream"`
}

type UpstreamBenchmarkRequest struct {
	Model       string `json:"model"`
	Concurrency int    `json:"concurrency"`
}

type UpstreamModelsResponse struct {
	Models []string `json:"models"`
}

type UpstreamSyncRequest struct {
	IDs []int64 `json:"ids"`
	Tag string  `json:"tag"`
}

type UpstreamBenchmarkMetric struct {
	Admission float64 `json:"admission"`
	Full      float64 `json:"full"`
	Weight    float64 `json:"weight"`
	Enabled   bool    `json:"enabled"`
	Direction string  `json:"direction"`
}

type UpstreamBenchmarkProfileResponse struct {
	Name        string                             `json:"name"`
	Version     int                                `json:"version"`
	Metrics     map[string]UpstreamBenchmarkMetric `json:"metrics"`
	UpdatedTime int64                              `json:"updated_time"`
}

type UpstreamBenchmarkProfileRequest struct {
	Metrics map[string]UpstreamBenchmarkMetric `json:"metrics"`
}

type UpstreamAutoSyncConfig struct {
	Enabled    bool    `json:"enabled"`
	ChannelTag string  `json:"channel_tag"`
	MinScore   float64 `json:"min_score"`
}
