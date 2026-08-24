package constant

const (
	UpstreamSourceAdmin = "admin"
	UpstreamSourceSelf  = "self"

	BusinessCooperationPendingReview    = "pending_review"
	BusinessCooperationBenchmarkRunning = "benchmark_running"
	BusinessCooperationPendingDecision  = "pending_decision"
	BusinessCooperationApproved         = "approved"
	BusinessCooperationRejected         = "rejected"

	UpstreamBenchmarkPending   = "pending"
	UpstreamBenchmarkRunning   = "running"
	UpstreamBenchmarkCompleted = "completed"

	UpstreamBenchmarkRunPending   = "pending"
	UpstreamBenchmarkRunRunning   = "running"
	UpstreamBenchmarkRunSucceeded = "succeeded"
	UpstreamBenchmarkRunFailed    = "failed"
	UpstreamBenchmarkRunCancelled = "cancelled"

	SystemTaskTypeUpstreamBenchmark = "upstream_benchmark"

	UpstreamBenchmarkProfileGeneral  = "general"
	UpstreamAutoSyncEnabledOption    = "UpstreamAutoSyncEnabled"
	UpstreamAutoSyncChannelTagOption = "UpstreamAutoSyncChannelTag"
	UpstreamAutoSyncMinScoreOption   = "UpstreamAutoSyncMinScore"
)

const (
	UpstreamMetricRPM          = "RPM"
	UpstreamMetricTPM          = "TPM"
	UpstreamMetricTTFTP50_8K   = "TTFT_P50_8K"
	UpstreamMetricTTFTP90_8K   = "TTFT_P90_8K"
	UpstreamMetricTTFTP50_32K  = "TTFT_P50_32K"
	UpstreamMetricTTFTP90_32K  = "TTFT_P90_32K"
	UpstreamMetricTTFTP50_128K = "TTFT_P50_128K"
	UpstreamMetricTTFTP90_128K = "TTFT_P90_128K"
	UpstreamMetricOTPPSP50     = "OTPS_P50_128K"
)
