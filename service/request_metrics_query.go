package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"golang.org/x/sync/errgroup"
)

// ===== 时间范围 / 桶 =====

func ParseTimeRange(s string, nowUnix int64) (from, to int64, err error) {
	to = nowUnix
	switch s {
	case "30m", "":
		from = to - 30*60
	case "1h":
		from = to - 3600
	case "6h":
		from = to - 6*3600
	case "24h":
		from = to - 24*3600
	case "48h":
		from = to - 48*3600
	default:
		return 0, 0, errors.New("invalid range: " + s)
	}
	return
}

// BucketSecondsForRange 趋势折线时间桶大小。
func BucketSecondsForRange(s string) int {
	switch s {
	case "30m", "1h":
		return 60
	case "6h":
		return 300
	case "24h":
		return 900
	case "48h":
		return 1800
	}
	return 60
}

// ===== Overview =====

type OverviewTotal struct {
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	AvgTTFTMs     int     `json:"avg_ttft_ms"`
	TTFTP50Ms     int     `json:"ttft_p50_ms"`
	TTFTP95Ms     int     `json:"ttft_p95_ms"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate"`
}

type OverviewPlatform struct {
	ChannelType   int     `json:"channel_type" gorm:"column:channel_type"`
	PlatformName  string  `json:"platform_name" gorm:"-"`
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate"`
}

type OverviewResult struct {
	Total     OverviewTotal      `json:"total"`
	Platforms []OverviewPlatform `json:"platforms"`
}

func QueryOverview(ctx context.Context, from, to int64, userIdFilter int) (*OverviewResult, error) {
	th := setting.GetMetricsThresholds()

	userFilter, args := buildUserFilter(userIdFilter)
	totalSQL := `
SELECT
    COUNT(*) AS req_total,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p95_ms,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS slow_resp_rate,
    COALESCE(AVG(first_token_ms) FILTER (WHERE is_stream AND first_token_ms > 0), 0)::INT AS avg_ttft_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY first_token_ms)
        FILTER (WHERE is_stream AND first_token_ms > 0), 0)::INT AS ttft_p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_ms)
        FILTER (WHERE is_stream AND first_token_ms > 0), 0)::INT AS ttft_p95_ms,
    COALESCE(SUM(CASE WHEN is_stream AND first_token_ms > 0 AND first_token_ms > ?
                 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(SUM(CASE WHEN is_stream AND first_token_ms > 0 THEN 1 ELSE 0 END), 0), 0) AS slow_ttft_rate
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ? ` + userFilter
	totalArgs := append([]any{th.SlowResponseMs, th.SlowTTFTMs, from, to}, args...)

	platformSQL := `
SELECT
    channel_type,
    COUNT(*) AS req_total,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p95_ms,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS slow_resp_rate,
    COALESCE(SUM(CASE WHEN is_stream AND first_token_ms > 0 AND first_token_ms > ?
                 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(SUM(CASE WHEN is_stream AND first_token_ms > 0 THEN 1 ELSE 0 END), 0), 0) AS slow_ttft_rate
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ? ` + userFilter + `
GROUP BY channel_type
ORDER BY req_total DESC`
	platformArgs := append([]any{th.SlowResponseMs, th.SlowTTFTMs, from, to}, args...)

	var (
		total     OverviewTotal
		platforms []OverviewPlatform
		eg        errgroup.Group
	)
	eg.Go(func() error {
		return model.LOG_DB.WithContext(ctx).Raw(totalSQL, totalArgs...).Scan(&total).Error
	})
	eg.Go(func() error {
		return model.LOG_DB.WithContext(ctx).Raw(platformSQL, platformArgs...).Scan(&platforms).Error
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	for i := range platforms {
		platforms[i].PlatformName = ChannelTypeName(platforms[i].ChannelType)
	}
	return &OverviewResult{Total: total, Platforms: platforms}, nil
}

// ===== Users =====

type UserMetricsRow struct {
	UserId        int      `json:"user_id"`
	Username      string   `json:"username"`
	Platforms     []string `json:"platforms" gorm:"-"`
	PlatformsCSV  string   `json:"-" gorm:"column:platforms_csv"`
	ReqTotal      int64    `json:"req_total"`
	AvgDurationMs int      `json:"avg_duration_ms"`
	P50Ms         int      `json:"p50_ms"`
	P95Ms         int      `json:"p95_ms"`
	ErrorRate     float64  `json:"error_rate"`
	SlowRespRate  float64  `json:"slow_resp_rate"`
	SlowTTFTRate  float64  `json:"slow_ttft_rate"`
}

type UserMetricsFilter struct {
	Username    string // 模糊匹配 username(ILIKE %?%)
	ChannelType int    // 0 = 不限;否则只看该 channel_type 的请求
}

// QueryUserMetricsCount 返回符合过滤条件的用户总数(用于分页)。
func QueryUserMetricsCount(ctx context.Context, from, to int64, filter UserMetricsFilter) (int64, error) {
	conds := []string{"created_at >= ?", "created_at < ?"}
	args := []any{from, to}
	if username := strings.TrimSpace(filter.Username); username != "" {
		conds = append(conds, "username ILIKE ?")
		args = append(args, "%"+username+"%")
	}
	if filter.ChannelType > 0 {
		conds = append(conds, "channel_type = ?")
		args = append(args, filter.ChannelType)
	}
	sql := "SELECT COUNT(DISTINCT user_id) FROM request_metrics_logs WHERE " + strings.Join(conds, " AND ")
	var total int64
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func QueryUserMetrics(ctx context.Context, from, to int64, page, size int, filter UserMetricsFilter) ([]UserMetricsRow, error) {
	th := setting.GetMetricsThresholds()
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 500 {
		size = 100
	}
	offset := (page - 1) * size

	// 动态 WHERE
	conds := []string{"created_at >= ?", "created_at < ?"}
	args := []any{th.SlowResponseMs, th.SlowTTFTMs, from, to}
	if username := strings.TrimSpace(filter.Username); username != "" {
		conds = append(conds, "username ILIKE ?")
		args = append(args, "%"+username+"%")
	}
	if filter.ChannelType > 0 {
		conds = append(conds, "channel_type = ?")
		args = append(args, filter.ChannelType)
	}
	args = append(args, size, offset)

	sql := `
SELECT
    user_id,
    COALESCE(MAX(username), '') AS username,
    STRING_AGG(DISTINCT channel_type::TEXT, ',') AS platforms_csv,
    COUNT(*) AS req_total,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p95_ms,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS slow_resp_rate,
    COALESCE(SUM(CASE WHEN is_stream AND first_token_ms > 0 AND first_token_ms > ?
                 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(SUM(CASE WHEN is_stream AND first_token_ms > 0 THEN 1 ELSE 0 END), 0), 0) AS slow_ttft_rate
FROM request_metrics_logs
WHERE ` + strings.Join(conds, " AND ") + `
GROUP BY user_id
ORDER BY req_total DESC
LIMIT ? OFFSET ?`

	var rows []UserMetricsRow
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Platforms = toPlatformNames(rows[i].PlatformsCSV)
	}
	return rows, nil
}

// ===== Platforms =====

type PlatformMetricsRow struct {
	ChannelType   int     `json:"channel_type"`
	PlatformName  string  `json:"platform_name" gorm:"-"`
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
}

func QueryPlatformMetrics(ctx context.Context, from, to int64) ([]PlatformMetricsRow, error) {
	th := setting.GetMetricsThresholds()
	sql := `
SELECT
    channel_type,
    COUNT(*) AS req_total,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p95_ms,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS slow_resp_rate
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ?
GROUP BY channel_type
ORDER BY req_total DESC`

	var rows []PlatformMetricsRow
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, th.SlowResponseMs, from, to).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].PlatformName = ChannelTypeName(rows[i].ChannelType)
	}
	return rows, nil
}

// ===== Platform → Channels =====

type ChannelMetricsRow struct {
	ChannelId     int     `json:"channel_id"`
	ChannelName   string  `json:"channel_name" gorm:"-"`
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate"`
}

func QueryPlatformChannels(ctx context.Context, channelType int, from, to int64, userIdFilter int) ([]ChannelMetricsRow, error) {
	th := setting.GetMetricsThresholds()
	userCond := ""
	args := []any{th.SlowResponseMs, th.SlowTTFTMs, from, to, channelType}
	if userIdFilter > 0 {
		userCond = " AND user_id = ?"
		args = append(args, userIdFilter)
	}
	sql := `
SELECT
    channel_id,
    COUNT(*) AS req_total,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p95_ms,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS slow_resp_rate,
    COALESCE(SUM(CASE WHEN is_stream AND first_token_ms > 0 AND first_token_ms > ?
                 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(SUM(CASE WHEN is_stream AND first_token_ms > 0 THEN 1 ELSE 0 END), 0), 0) AS slow_ttft_rate
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ? AND channel_type = ?` + userCond + `
GROUP BY channel_id
ORDER BY req_total DESC`

	var rows []ChannelMetricsRow
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		nameMap := loadChannelNamesByIDs(rowChannelIDs(rows))
		for i := range rows {
			rows[i].ChannelName = nameMap[rows[i].ChannelId]
		}
	}
	return rows, nil
}

// ===== Channel → Models =====

type ModelMetricsRow struct {
	ModelName     string  `json:"model_name"`
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate"`
}

func QueryChannelModels(ctx context.Context, channelId int, from, to int64, userIdFilter int) ([]ModelMetricsRow, error) {
	th := setting.GetMetricsThresholds()
	userCond := ""
	args := []any{th.SlowResponseMs, th.SlowTTFTMs, from, to, channelId}
	if userIdFilter > 0 {
		userCond = " AND user_id = ?"
		args = append(args, userIdFilter)
	}
	sql := `
SELECT
    model_name,
    COUNT(*) AS req_total,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p95_ms,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS slow_resp_rate,
    COALESCE(SUM(CASE WHEN is_stream AND first_token_ms > 0 AND first_token_ms > ?
                 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(SUM(CASE WHEN is_stream AND first_token_ms > 0 THEN 1 ELSE 0 END), 0), 0) AS slow_ttft_rate
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ? AND channel_id = ?` + userCond + `
GROUP BY model_name
ORDER BY req_total DESC`

	var rows []ModelMetricsRow
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ===== Trend =====

type TrendPoint struct {
	Ts            int64   `json:"ts" gorm:"column:bucket_at"`
	ReqTotal      int64   `json:"req_total"`
	ErrCount      int64   `json:"err_count"`
	ErrorRate     float64 `json:"error_rate"`
	AvgDurationMs int     `json:"avg_duration_ms"`
}

type TrendResult struct {
	BucketSeconds int          `json:"bucket_seconds"`
	Series        []TrendPoint `json:"series"`
}

func QueryTrend(ctx context.Context, from, to int64, bucketSeconds int, userIdFilter int) (*TrendResult, error) {
	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}
	userFilter, args := buildUserFilter(userIdFilter)
	sql := `
SELECT
    (floor(created_at::FLOAT / ?) * ?)::BIGINT AS bucket_at,
    COUNT(*) AS req_total,
    SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS err_count,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ? ` + userFilter + `
GROUP BY bucket_at
ORDER BY bucket_at`
	allArgs := append([]any{bucketSeconds, bucketSeconds, from, to}, args...)
	var points []TrendPoint
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, allArgs...).Scan(&points).Error; err != nil {
		return nil, err
	}
	return &TrendResult{BucketSeconds: bucketSeconds, Series: points}, nil
}

// ===== Errors Top10 =====

type ErrorTopRow struct {
	ErrorCode      string  `json:"error_code"`
	ErrCount       int64   `json:"err_count"`
	AvgDurationMs  int     `json:"avg_duration_ms"`
	ChannelTypes   []int   `json:"channel_types" gorm:"-"`
	ChannelTypeCSV string  `json:"-" gorm:"column:channel_types"`
	PlatformNames  []string `json:"platform_names" gorm:"-"`
	SampleMessage  string  `json:"sample_message"`
}

type ErrorTopFilter struct {
	ChannelType int
	ChannelId   int
	UserId      int
}

func QueryErrorsTop(ctx context.Context, from, to int64, limit int, f ErrorTopFilter) ([]ErrorTopRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	conds := []string{
		"created_at >= ?", "created_at < ?",
		"status_code >= 400", "NOT is_business_error",
	}
	args := []any{from, to}
	if f.ChannelType > 0 {
		conds = append(conds, "channel_type = ?")
		args = append(args, f.ChannelType)
	}
	if f.ChannelId > 0 {
		conds = append(conds, "channel_id = ?")
		args = append(args, f.ChannelId)
	}
	if f.UserId > 0 {
		conds = append(conds, "user_id = ?")
		args = append(args, f.UserId)
	}
	where := strings.Join(conds, " AND ")

	sql := `
SELECT
    COALESCE(NULLIF(error_code, ''), 'unknown') AS error_code,
    COUNT(*) AS err_count,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    STRING_AGG(DISTINCT channel_type::TEXT, ',') AS channel_types,
    COALESCE((array_agg(error_message ORDER BY created_at DESC))[1], '') AS sample_message
FROM request_metrics_logs
WHERE ` + where + `
GROUP BY COALESCE(NULLIF(error_code, ''), 'unknown')
ORDER BY err_count DESC
LIMIT ?`
	args = append(args, limit)
	var rows []ErrorTopRow
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		ids := toIntSlice(rows[i].ChannelTypeCSV)
		rows[i].ChannelTypes = ids
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			names = append(names, ChannelTypeName(id))
		}
		rows[i].PlatformNames = names
	}
	return rows, nil
}

// ===== Errors Detail =====

type ErrorDetailRow struct {
	RequestId     string `json:"request_id"`
	UserId        int    `json:"user_id"`
	Username      string `json:"username"`
	ChannelId     int    `json:"channel_id"`
	ChannelType   int    `json:"channel_type"`
	PlatformName  string `json:"platform_name" gorm:"-"`
	ModelName     string `json:"model_name"`
	StatusCode    int    `json:"status_code"`
	DurationMs    int    `json:"duration_ms"`
	ErrorMessage  string `json:"error_message"`
	CreatedAt     int64  `json:"created_at"`
}

func QueryErrorsDetail(ctx context.Context, from, to int64, errorCode string, limit int) ([]ErrorDetailRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if errorCode == "" {
		return nil, errors.New("error_code is required")
	}
	codeCond := "error_code = ?"
	if errorCode == "unknown" {
		codeCond = "(error_code = '' OR error_code = 'unknown')"
	}
	sql := `
SELECT
    request_id, user_id, username,
    channel_id, channel_type, model_name,
    status_code, duration_ms,
    error_message,
    created_at
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ?
  AND status_code >= 400 AND NOT is_business_error
  AND ` + codeCond + `
ORDER BY created_at DESC
LIMIT ?`
	var rows []ErrorDetailRow
	args := []any{from, to}
	if errorCode != "unknown" {
		args = append(args, errorCode)
	}
	args = append(args, limit)
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].PlatformName = ChannelTypeName(rows[i].ChannelType)
	}
	return rows, nil
}

// ===== 工具函数 =====

func buildUserFilter(userId int) (string, []any) {
	if userId <= 0 {
		return "", nil
	}
	return "AND user_id = ?", []any{userId}
}

func toPlatformNames(csv string) []string {
	if csv == "" {
		return nil
	}
	ids := strings.Split(csv, ",")
	out := make([]string, 0, len(ids))
	for _, s := range ids {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if n, err := strconv.Atoi(s); err == nil {
			out = append(out, ChannelTypeName(n))
		}
	}
	return out
}

func toIntSlice(csv string) []int {
	if csv == "" {
		return nil
	}
	ids := strings.Split(csv, ",")
	out := make([]int, 0, len(ids))
	for _, s := range ids {
		s = strings.TrimSpace(s)
		if n, err := strconv.Atoi(s); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func rowChannelIDs(rows []ChannelMetricsRow) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ChannelId)
	}
	return out
}

func loadChannelNamesByIDs(ids []int) map[int]string {
	out := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return out
	}
	type row struct {
		Id   int
		Name string
	}
	var rows []row
	if err := model.DB.Table("channels").
		Select("id, name").
		Where("id IN ?", ids).
		Scan(&rows).Error; err == nil {
		for _, r := range rows {
			out[r.Id] = r.Name
		}
	}
	return out
}

// ChannelTypeName 把 channel.type 整数枚举翻译成展示名。
func ChannelTypeName(t int) string {
	switch t {
	case constant.ChannelTypeOpenAI:
		return "OpenAI"
	case constant.ChannelTypeMidjourney:
		return "Midjourney"
	case constant.ChannelTypeAzure:
		return "Azure"
	case constant.ChannelTypeOllama:
		return "Ollama"
	case constant.ChannelTypeMidjourneyPlus:
		return "Midjourney Plus"
	case constant.ChannelTypeOpenAIMax:
		return "OpenAI Max"
	case constant.ChannelTypeOhMyGPT:
		return "OhMyGPT"
	case constant.ChannelTypeCustom:
		return "Custom"
	case constant.ChannelTypeAnthropic:
		return "Anthropic"
	case constant.ChannelTypeBaidu:
		return "Baidu"
	case constant.ChannelTypeZhipu:
		return "Zhipu"
	case constant.ChannelTypeAli:
		return "Ali"
	case constant.ChannelTypeXunfei:
		return "Xunfei"
	case constant.ChannelType360:
		return "360"
	case constant.ChannelTypeOpenRouter:
		return "OpenRouter"
	case constant.ChannelTypeTencent:
		return "Tencent"
	case constant.ChannelTypeGemini:
		return "Gemini"
	case constant.ChannelTypeMoonshot:
		return "Moonshot"
	case constant.ChannelTypeZhipu_v4:
		return "Zhipu V4"
	case constant.ChannelTypePerplexity:
		return "Perplexity"
	case constant.ChannelTypeLingYiWanWu:
		return "LingYiWanWu"
	case constant.ChannelTypeAws:
		return "AWS"
	case constant.ChannelTypeCohere:
		return "Cohere"
	case constant.ChannelTypeMiniMax:
		return "MiniMax"
	case constant.ChannelTypeSunoAPI:
		return "Suno"
	case constant.ChannelTypeDify:
		return "Dify"
	case constant.ChannelTypeJina:
		return "Jina"
	case constant.ChannelTypeSiliconFlow:
		return "SiliconFlow"
	case constant.ChannelTypeVertexAi:
		return "Vertex AI"
	case constant.ChannelTypeMistral:
		return "Mistral"
	case constant.ChannelTypeDeepSeek:
		return "DeepSeek"
	case constant.ChannelTypeMokaAI:
		return "Moka"
	case constant.ChannelTypeVolcEngine:
		return "VolcEngine"
	case constant.ChannelTypeBaiduV2:
		return "Baidu V2"
	case constant.ChannelTypeXinference:
		return "Xinference"
	case constant.ChannelTypeXai:
		return "xAI"
	case constant.ChannelTypeCoze:
		return "Coze"
	case constant.ChannelTypeKling:
		return "Kling"
	case constant.ChannelTypeJimeng:
		return "Jimeng"
	case constant.ChannelTypeVidu:
		return "Vidu"
	case constant.ChannelTypeSubmodel:
		return "Submodel"
	case constant.ChannelTypeDoubaoVideo:
		return "Doubao Video"
	case constant.ChannelTypeSora:
		return "Sora"
	case constant.ChannelTypeReplicate:
		return "Replicate"
	case constant.ChannelTypeCodex:
		return "Codex"
	}
	return fmt.Sprintf("Type-%d", t)
}
