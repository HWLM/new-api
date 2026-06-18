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

// TimeWindow 描述一次查询的"主时段 + 可选对比时段"，所有单位为 unix 秒。
// 区间语义统一为左闭右开 [From, To)；对比时段 CompareFrom/CompareTo 同语义。
type TimeWindow struct {
	From        int64
	To          int64
	CompareFrom int64
	CompareTo   int64
}

// HasCompare 是否启用对比时段。
func (w TimeWindow) HasCompare() bool {
	return w.CompareFrom > 0 && w.CompareTo > w.CompareFrom
}

// Span 主时段时间跨度（秒）。
func (w TimeWindow) Span() int64 {
	if w.To <= w.From {
		return 0
	}
	return w.To - w.From
}

// ParseTimeWindow 解析 from/to/compare_from/compare_to 四个参数，单位均为 unix 秒。
// 任一边界缺失或非法都返回错误；compare_* 全部缺省视为未开启对比。
func ParseTimeWindow(fromStr, toStr, cmpFromStr, cmpToStr string) (TimeWindow, error) {
	from, err := strconv.ParseInt(strings.TrimSpace(fromStr), 10, 64)
	if err != nil || from <= 0 {
		return TimeWindow{}, errors.New("invalid from: " + fromStr)
	}
	to, err := strconv.ParseInt(strings.TrimSpace(toStr), 10, 64)
	if err != nil || to <= from {
		return TimeWindow{}, errors.New("invalid to: " + toStr)
	}
	w := TimeWindow{From: from, To: to}
	cmpFromStr = strings.TrimSpace(cmpFromStr)
	cmpToStr = strings.TrimSpace(cmpToStr)
	if cmpFromStr == "" && cmpToStr == "" {
		return w, nil
	}
	cf, err1 := strconv.ParseInt(cmpFromStr, 10, 64)
	ct, err2 := strconv.ParseInt(cmpToStr, 10, 64)
	if err1 != nil || err2 != nil || cf <= 0 || ct <= cf {
		return TimeWindow{}, errors.New("invalid compare window")
	}
	w.CompareFrom = cf
	w.CompareTo = ct
	return w, nil
}

// BucketSecondsForSpan 根据时间跨度（秒）选择折线时间桶大小。
//
// 与原 BucketSecondsForRange 的映射保持兼容：
//
//	≤ 1h    → 60s
//	≤ 6h    → 300s
//	≤ 24h   → 900s
//	> 24h   → 1800s
func BucketSecondsForSpan(span int64) int {
	switch {
	case span <= 3600:
		return 60
	case span <= 6*3600:
		return 300
	case span <= 24*3600:
		return 900
	default:
		return 1800
	}
}

// ===== Overview =====

type OverviewTotal struct {
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	AvgTTFTMs     int     `json:"avg_ttft_ms" gorm:"column:avg_ttft_ms"`
	TTFTP50Ms     int     `json:"ttft_p50_ms" gorm:"column:ttft_p50_ms"`
	TTFTP95Ms     int     `json:"ttft_p95_ms" gorm:"column:ttft_p95_ms"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate" gorm:"column:slow_ttft_rate"`
}

type OverviewPlatform struct {
	ChannelType   int     `json:"channel_type" gorm:"column:channel_type"`
	PlatformName  string  `json:"platform_name" gorm:"-"`
	ReqTotal      int64   `json:"req_total"`
	ErrCount      int64   `json:"err_count"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	AvgTTFTMs     int     `json:"avg_ttft_ms" gorm:"column:avg_ttft_ms"`
	TTFTP50Ms     int     `json:"ttft_p50_ms" gorm:"column:ttft_p50_ms"`
	TTFTP95Ms     int     `json:"ttft_p95_ms" gorm:"column:ttft_p95_ms"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate" gorm:"column:slow_ttft_rate"`
}

type OverviewResult struct {
	Total            OverviewTotal      `json:"total"`
	Platforms        []OverviewPlatform `json:"platforms"`
	CompareTotal     *OverviewTotal     `json:"compare_total,omitempty"`
	ComparePlatforms []OverviewPlatform `json:"compare_platforms,omitempty"`
}

func QueryOverview(ctx context.Context, win TimeWindow, userIdFilter int) (*OverviewResult, error) {
	var (
		total            OverviewTotal
		platforms        []OverviewPlatform
		compareTotal     OverviewTotal
		comparePlatforms []OverviewPlatform
		eg               errgroup.Group
	)
	eg.Go(func() error {
		t, p, err := queryOverviewSingle(ctx, win.From, win.To, userIdFilter)
		if err != nil {
			return err
		}
		total, platforms = t, p
		return nil
	})
	if win.HasCompare() {
		eg.Go(func() error {
			t, p, err := queryOverviewSingle(ctx, win.CompareFrom, win.CompareTo, userIdFilter)
			if err != nil {
				return err
			}
			compareTotal, comparePlatforms = t, p
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	res := &OverviewResult{Total: total, Platforms: platforms}
	if win.HasCompare() {
		ct := compareTotal
		res.CompareTotal = &ct
		res.ComparePlatforms = comparePlatforms
	}
	return res, nil
}

func queryOverviewSingle(ctx context.Context, from, to int64, userIdFilter int) (OverviewTotal, []OverviewPlatform, error) {
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
    COALESCE(percentile_cont(0.50) WITHIN GROUP (
        ORDER BY CASE WHEN is_stream AND first_token_ms > 0 THEN first_token_ms END
    ), 0)::INT AS ttft_p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (
        ORDER BY CASE WHEN is_stream AND first_token_ms > 0 THEN first_token_ms END
    ), 0)::INT AS ttft_p95_ms,
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
    SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS err_count,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::INT AS p95_ms,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS slow_resp_rate,
    COALESCE(AVG(first_token_ms) FILTER (WHERE is_stream AND first_token_ms > 0), 0)::INT AS avg_ttft_ms,
    COALESCE(percentile_cont(0.50) WITHIN GROUP (
        ORDER BY CASE WHEN is_stream AND first_token_ms > 0 THEN first_token_ms END
    ), 0)::INT AS ttft_p50_ms,
    COALESCE(percentile_cont(0.95) WITHIN GROUP (
        ORDER BY CASE WHEN is_stream AND first_token_ms > 0 THEN first_token_ms END
    ), 0)::INT AS ttft_p95_ms,
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
		return OverviewTotal{}, nil, err
	}
	for i := range platforms {
		platforms[i].PlatformName = ChannelTypeName(platforms[i].ChannelType)
	}
	return total, platforms, nil
}

// ===== Users =====

// UserMetricsCompare 用户行对比时段的同维度指标，字段集与 UserMetricsRow 中的数值字段对齐。
type UserMetricsCompare struct {
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate"`
}

type UserMetricsRow struct {
	UserId        int                 `json:"user_id"`
	Username      string              `json:"username"`
	Platforms     []string            `json:"platforms" gorm:"-"`
	PlatformsCSV  string              `json:"-" gorm:"column:platforms_csv"`
	ReqTotal      int64               `json:"req_total"`
	AvgDurationMs int                 `json:"avg_duration_ms"`
	P50Ms         int                 `json:"p50_ms"`
	P95Ms         int                 `json:"p95_ms"`
	ErrorRate     float64             `json:"error_rate"`
	SlowRespRate  float64             `json:"slow_resp_rate"`
	SlowTTFTRate  float64             `json:"slow_ttft_rate"`
	Compare       *UserMetricsCompare `json:"compare,omitempty" gorm:"-"`
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

func QueryUserMetrics(ctx context.Context, win TimeWindow, page, size int, filter UserMetricsFilter) ([]UserMetricsRow, error) {
	rows, err := queryUserMetricsSingle(ctx, win.From, win.To, page, size, filter)
	if err != nil {
		return nil, err
	}
	if !win.HasCompare() || len(rows) == 0 {
		return rows, nil
	}
	// 对本期 user_id 列表在对比时段做同维度聚合，map 合并；不分页（仅这批 user_id）。
	uids := make([]int, 0, len(rows))
	for _, r := range rows {
		uids = append(uids, r.UserId)
	}
	cmpMap, err := queryUserMetricsCompare(ctx, win.CompareFrom, win.CompareTo, uids, filter)
	if err != nil {
		// 对比查询失败不应阻断主结果展示；返回不带 compare 的行。
		return rows, nil
	}
	for i := range rows {
		if c, ok := cmpMap[rows[i].UserId]; ok {
			cc := c
			rows[i].Compare = &cc
		} else {
			rows[i].Compare = &UserMetricsCompare{}
		}
	}
	return rows, nil
}

func queryUserMetricsSingle(ctx context.Context, from, to int64, page, size int, filter UserMetricsFilter) ([]UserMetricsRow, error) {
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

// queryUserMetricsCompare 对一组指定 user_id 在 [from, to) 时段做同维度聚合，返回 user_id → Compare 映射。
// 仅对查询命中的 user_id 返回 entry；未命中表示对比时段该用户无数据。
func queryUserMetricsCompare(ctx context.Context, from, to int64, userIds []int, filter UserMetricsFilter) (map[int]UserMetricsCompare, error) {
	if len(userIds) == 0 {
		return map[int]UserMetricsCompare{}, nil
	}
	th := setting.GetMetricsThresholds()
	conds := []string{"created_at >= ?", "created_at < ?", "user_id IN ?"}
	args := []any{th.SlowResponseMs, th.SlowTTFTMs, from, to, userIds}
	if filter.ChannelType > 0 {
		conds = append(conds, "channel_type = ?")
		args = append(args, filter.ChannelType)
	}
	sql := `
SELECT
    user_id,
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
GROUP BY user_id`
	type row struct {
		UserId int
		UserMetricsCompare
	}
	var rows []row
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int]UserMetricsCompare, len(rows))
	for _, r := range rows {
		out[r.UserId] = r.UserMetricsCompare
	}
	return out, nil
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

// ChannelMetricsCompare 渠道对比时段的同维度指标，字段集与 ChannelMetricsRow 数值字段对齐。
type ChannelMetricsCompare struct {
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate"`
}

type ChannelMetricsRow struct {
	ChannelId     int                    `json:"channel_id"`
	ChannelName   string                 `json:"channel_name" gorm:"-"`
	ReqTotal      int64                  `json:"req_total"`
	AvgDurationMs int                    `json:"avg_duration_ms"`
	P50Ms         int                    `json:"p50_ms"`
	P95Ms         int                    `json:"p95_ms"`
	ErrorRate     float64                `json:"error_rate"`
	SlowRespRate  float64                `json:"slow_resp_rate"`
	SlowTTFTRate  float64                `json:"slow_ttft_rate"`
	Compare       *ChannelMetricsCompare `json:"compare,omitempty" gorm:"-"`
}

func QueryPlatformChannels(ctx context.Context, channelType int, win TimeWindow, userIdFilter int) ([]ChannelMetricsRow, error) {
	rows, err := queryPlatformChannelsSingle(ctx, channelType, win.From, win.To, userIdFilter)
	if err != nil {
		return nil, err
	}
	if !win.HasCompare() || len(rows) == 0 {
		return rows, nil
	}
	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ChannelId)
	}
	cmpMap, err := queryPlatformChannelsCompare(ctx, channelType, win.CompareFrom, win.CompareTo, userIdFilter, ids)
	if err != nil {
		return rows, nil
	}
	for i := range rows {
		if c, ok := cmpMap[rows[i].ChannelId]; ok {
			cc := c
			rows[i].Compare = &cc
		} else {
			rows[i].Compare = &ChannelMetricsCompare{}
		}
	}
	return rows, nil
}

func queryPlatformChannelsSingle(ctx context.Context, channelType int, from, to int64, userIdFilter int) ([]ChannelMetricsRow, error) {
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

// queryPlatformChannelsCompare 对一组指定 channel_id 在 [from, to) 时段做同维度聚合，
// 返回 channel_id → Compare 映射；用于主期 rows 之后的同期对比合并。
func queryPlatformChannelsCompare(ctx context.Context, channelType int, from, to int64, userIdFilter int, channelIds []int) (map[int]ChannelMetricsCompare, error) {
	if len(channelIds) == 0 {
		return map[int]ChannelMetricsCompare{}, nil
	}
	th := setting.GetMetricsThresholds()
	userCond := ""
	args := []any{th.SlowResponseMs, th.SlowTTFTMs, from, to, channelType, channelIds}
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
WHERE created_at >= ? AND created_at < ? AND channel_type = ? AND channel_id IN ?` + userCond + `
GROUP BY channel_id`
	type row struct {
		ChannelId int
		ChannelMetricsCompare
	}
	var rows []row
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int]ChannelMetricsCompare, len(rows))
	for _, r := range rows {
		out[r.ChannelId] = r.ChannelMetricsCompare
	}
	return out, nil
}

// ===== Channel → Models =====

// ModelMetricsCompare 模型对比时段同维度指标。
type ModelMetricsCompare struct {
	ReqTotal      int64   `json:"req_total"`
	AvgDurationMs int     `json:"avg_duration_ms"`
	P50Ms         int     `json:"p50_ms"`
	P95Ms         int     `json:"p95_ms"`
	ErrorRate     float64 `json:"error_rate"`
	SlowRespRate  float64 `json:"slow_resp_rate"`
	SlowTTFTRate  float64 `json:"slow_ttft_rate"`
}

type ModelMetricsRow struct {
	ModelName     string               `json:"model_name"`
	ReqTotal      int64                `json:"req_total"`
	AvgDurationMs int                  `json:"avg_duration_ms"`
	P50Ms         int                  `json:"p50_ms"`
	P95Ms         int                  `json:"p95_ms"`
	ErrorRate     float64              `json:"error_rate"`
	SlowRespRate  float64              `json:"slow_resp_rate"`
	SlowTTFTRate  float64              `json:"slow_ttft_rate"`
	Compare       *ModelMetricsCompare `json:"compare,omitempty" gorm:"-"`
}

func QueryChannelModels(ctx context.Context, channelId int, win TimeWindow, userIdFilter int) ([]ModelMetricsRow, error) {
	rows, err := queryChannelModelsSingle(ctx, channelId, win.From, win.To, userIdFilter)
	if err != nil {
		return nil, err
	}
	if !win.HasCompare() || len(rows) == 0 {
		return rows, nil
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.ModelName)
	}
	cmpMap, err := queryChannelModelsCompare(ctx, channelId, win.CompareFrom, win.CompareTo, userIdFilter, names)
	if err != nil {
		return rows, nil
	}
	for i := range rows {
		if c, ok := cmpMap[rows[i].ModelName]; ok {
			cc := c
			rows[i].Compare = &cc
		} else {
			rows[i].Compare = &ModelMetricsCompare{}
		}
	}
	return rows, nil
}

func queryChannelModelsSingle(ctx context.Context, channelId int, from, to int64, userIdFilter int) ([]ModelMetricsRow, error) {
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

// queryChannelModelsCompare 对一组指定 model_name 在 [from, to) 时段做同维度聚合。
func queryChannelModelsCompare(ctx context.Context, channelId int, from, to int64, userIdFilter int, modelNames []string) (map[string]ModelMetricsCompare, error) {
	if len(modelNames) == 0 {
		return map[string]ModelMetricsCompare{}, nil
	}
	th := setting.GetMetricsThresholds()
	userCond := ""
	args := []any{th.SlowResponseMs, th.SlowTTFTMs, from, to, channelId, modelNames}
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
WHERE created_at >= ? AND created_at < ? AND channel_id = ? AND model_name IN ?` + userCond + `
GROUP BY model_name`
	type row struct {
		ModelName string
		ModelMetricsCompare
	}
	var rows []row
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]ModelMetricsCompare, len(rows))
	for _, r := range rows {
		out[r.ModelName] = r.ModelMetricsCompare
	}
	return out, nil
}

// ===== Trend =====

type TrendPoint struct {
	Ts            int64   `json:"ts" gorm:"column:bucket_at"`
	ReqTotal      int64   `json:"req_total"`
	ErrCount      int64   `json:"err_count"`
	SlowRespCount int64   `json:"slow_resp_count"`
	SlowTTFTCount int64   `json:"slow_ttft_count"`
	ErrorRate     float64 `json:"error_rate"`
	AvgDurationMs int     `json:"avg_duration_ms"`
}

type TrendResult struct {
	BucketSeconds int          `json:"bucket_seconds"`
	Series        []TrendPoint `json:"series"`
	// CompareSeries 已按主时段同 bucket 起点偏移对齐：对比段第 i 个桶的 ts 已经被替换为主段第 i 个桶起点，
	// 前端可直接按 index 配对绘制对比折线。
	CompareSeries []TrendPoint `json:"compare_series,omitempty"`
}

// QueryTrend 查询主时段折线，并在 win 开启对比时并行查询同期对比序列。
//
// 输出的 Series 桶时间为 win.From 的真实绝对时间；CompareSeries 桶时间已平移到主段对应桶
// 起点，便于前端直接对齐绘图。
func QueryTrend(ctx context.Context, win TimeWindow, bucketSeconds int, userIdFilter int, channelTypeFilter int) (*TrendResult, error) {
	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}
	var (
		series        []TrendPoint
		compareSeries []TrendPoint
		eg            errgroup.Group
	)
	eg.Go(func() error {
		pts, err := queryTrendSingle(ctx, win.From, win.To, bucketSeconds, userIdFilter, channelTypeFilter)
		if err != nil {
			return err
		}
		series = pts
		return nil
	})
	if win.HasCompare() {
		eg.Go(func() error {
			pts, err := queryTrendSingle(ctx, win.CompareFrom, win.CompareTo, bucketSeconds, userIdFilter, channelTypeFilter)
			if err != nil {
				return err
			}
			// 把对比段桶时间平移到主段同位置 bucket，前端按 index 对齐绘图。
			offset := win.From - win.CompareFrom
			for i := range pts {
				pts[i].Ts += offset
			}
			compareSeries = pts
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	out := &TrendResult{BucketSeconds: bucketSeconds, Series: series}
	if win.HasCompare() {
		out.CompareSeries = compareSeries
	}
	return out, nil
}

func queryTrendSingle(ctx context.Context, from, to int64, bucketSeconds int, userIdFilter int, channelTypeFilter int) ([]TrendPoint, error) {
	th := setting.GetMetricsThresholds()
	userFilter, userArgs := buildUserFilter(userIdFilter)
	channelFilter := ""
	var channelArgs []any
	if channelTypeFilter > 0 {
		channelFilter = " AND channel_type = ?"
		channelArgs = []any{channelTypeFilter}
	}
	sql := `
SELECT
    (floor(created_at::FLOAT / ?) * ?)::BIGINT AS bucket_at,
    COUNT(*) AS req_total,
    SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) AS err_count,
    SUM(CASE WHEN duration_ms > ? THEN 1 ELSE 0 END) AS slow_resp_count,
    SUM(CASE WHEN is_stream AND first_token_ms > 0 AND first_token_ms > ?
             THEN 1 ELSE 0 END) AS slow_ttft_count,
    COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END)::FLOAT
        / NULLIF(COUNT(*), 0), 0) AS error_rate,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms
FROM request_metrics_logs
WHERE created_at >= ? AND created_at < ? ` + userFilter + channelFilter + `
GROUP BY bucket_at
ORDER BY bucket_at`
	allArgs := append([]any{bucketSeconds, bucketSeconds, th.SlowResponseMs, th.SlowTTFTMs, from, to}, userArgs...)
	allArgs = append(allArgs, channelArgs...)
	var points []TrendPoint
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, allArgs...).Scan(&points).Error; err != nil {
		return nil, err
	}
	return points, nil
}

// ===== Errors Top10 =====

type ErrorTopRow struct {
	ErrorCode      string   `json:"error_code"`
	ErrCount       int64    `json:"err_count"`
	AvgDurationMs  int      `json:"avg_duration_ms"`
	ChannelTypes   []int    `json:"channel_types" gorm:"-"`
	ChannelTypeCSV string   `json:"-" gorm:"column:channel_types"`
	PlatformNames  []string `json:"platform_names" gorm:"-"`
	SampleMessage  string   `json:"sample_message"`
	CompareCount   int64    `json:"compare_count,omitempty" gorm:"-"`
}

type ErrorTopFilter struct {
	ChannelType int
	ChannelId   int
	UserId      int
}

func QueryErrorsTop(ctx context.Context, win TimeWindow, limit int, f ErrorTopFilter) ([]ErrorTopRow, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := queryErrorsTopSingle(ctx, win.From, win.To, limit, f)
	if err != nil {
		return nil, err
	}
	if !win.HasCompare() {
		return rows, nil
	}
	// 主时段命中的 error_code → 在对比时段查它们的次数，回填 compare_count
	if len(rows) > 0 {
		codes := make([]string, 0, len(rows))
		for _, r := range rows {
			codes = append(codes, r.ErrorCode)
		}
		cmpMap, err := queryErrorsCompareCounts(ctx, win.CompareFrom, win.CompareTo, codes, f)
		if err == nil {
			for i := range rows {
				rows[i].CompareCount = cmpMap[rows[i].ErrorCode]
			}
		}
	}
	// 主时段不足 limit → 用对比时段 top 里"主时段没有"的 error_code 补足；
	// 补充行：err_count=0（主时段未发生），compare_count=对比段实际次数；其他字段（avg_duration_ms /
	// channel_types / sample_message）取对比段的值。
	if len(rows) < limit {
		exist := make(map[string]bool, len(rows))
		for _, r := range rows {
			exist[r.ErrorCode] = true
		}
		cmpRows, err := queryErrorsTopSingle(ctx, win.CompareFrom, win.CompareTo, limit, f)
		if err == nil {
			for _, cr := range cmpRows {
				if len(rows) >= limit {
					break
				}
				if exist[cr.ErrorCode] {
					continue
				}
				supplement := cr
				supplement.CompareCount = cr.ErrCount
				supplement.ErrCount = 0
				rows = append(rows, supplement)
				exist[cr.ErrorCode] = true
			}
		}
	}
	return rows, nil
}

func queryErrorsTopSingle(ctx context.Context, from, to int64, limit int, f ErrorTopFilter) ([]ErrorTopRow, error) {
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

// queryErrorsCompareCounts 返回指定 error_code 列表在 [from, to) 时段的出现次数。
// codes 中如包含 "unknown" 会被翻译为 (error_code = ” OR error_code = 'unknown')。
func queryErrorsCompareCounts(ctx context.Context, from, to int64, codes []string, f ErrorTopFilter) (map[string]int64, error) {
	if len(codes) == 0 {
		return map[string]int64{}, nil
	}
	// 拆分 unknown 与具名错误码：unknown 走特殊条件，具名走 IN 列表。
	hasUnknown := false
	named := make([]string, 0, len(codes))
	for _, c := range codes {
		if c == "unknown" {
			hasUnknown = true
		} else {
			named = append(named, c)
		}
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
	switch {
	case hasUnknown && len(named) > 0:
		conds = append(conds, "(error_code IN ? OR error_code = '' OR error_code = 'unknown')")
		args = append(args, named)
	case hasUnknown:
		conds = append(conds, "(error_code = '' OR error_code = 'unknown')")
	default:
		conds = append(conds, "error_code IN ?")
		args = append(args, named)
	}
	sql := `
SELECT COALESCE(NULLIF(error_code, ''), 'unknown') AS error_code, COUNT(*) AS cnt
FROM request_metrics_logs
WHERE ` + strings.Join(conds, " AND ") + `
GROUP BY COALESCE(NULLIF(error_code, ''), 'unknown')`
	type row struct {
		ErrorCode string
		Cnt       int64
	}
	var rows []row
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.ErrorCode] = r.Cnt
	}
	return out, nil
}

// QueryErrorTrend 返回指定 error_code 列表的折线（主时段 + 对比时段，对比段桶时间已平移对齐）。
// 仅统计 status_code >= 400 且非业务错误的请求；series 每桶只携带 err_count 一个值。
//
// errorCodes 长度：
//
//	1 个   → 单错误码趋势（行点击联动）
//	>1 个  → 汇总趋势（默认状态，常用于"top10 全部错误"汇总）
//	0 个   → 报错
func QueryErrorTrend(ctx context.Context, win TimeWindow, bucketSeconds int, errorCodes []string, f ErrorTopFilter) (*TrendResult, error) {
	if len(errorCodes) == 0 {
		return nil, errors.New("error_code is required")
	}
	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}
	var (
		series        []TrendPoint
		compareSeries []TrendPoint
		eg            errgroup.Group
	)
	eg.Go(func() error {
		pts, err := queryErrorTrendSingle(ctx, win.From, win.To, bucketSeconds, errorCodes, f)
		if err != nil {
			return err
		}
		series = pts
		return nil
	})
	if win.HasCompare() {
		eg.Go(func() error {
			pts, err := queryErrorTrendSingle(ctx, win.CompareFrom, win.CompareTo, bucketSeconds, errorCodes, f)
			if err != nil {
				return err
			}
			offset := win.From - win.CompareFrom
			for i := range pts {
				pts[i].Ts += offset
			}
			compareSeries = pts
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	out := &TrendResult{BucketSeconds: bucketSeconds, Series: series}
	if win.HasCompare() {
		out.CompareSeries = compareSeries
	}
	return out, nil
}

func queryErrorTrendSingle(ctx context.Context, from, to int64, bucketSeconds int, errorCodes []string, f ErrorTopFilter) ([]TrendPoint, error) {
	conds := []string{
		"created_at >= ?", "created_at < ?",
		"status_code >= 400", "NOT is_business_error",
	}
	args := []any{bucketSeconds, bucketSeconds, from, to}
	// 拆分 "unknown" 与具名 code：unknown 走 (error_code = '' OR = 'unknown')，
	// 其余走 IN 列表。两者并存时用 OR 合并。
	hasUnknown := false
	named := make([]string, 0, len(errorCodes))
	for _, c := range errorCodes {
		if c == "unknown" {
			hasUnknown = true
		} else if c != "" {
			named = append(named, c)
		}
	}
	switch {
	case hasUnknown && len(named) > 0:
		conds = append(conds, "(error_code IN ? OR error_code = '' OR error_code = 'unknown')")
		args = append(args, named)
	case hasUnknown:
		conds = append(conds, "(error_code = '' OR error_code = 'unknown')")
	case len(named) == 1:
		// 单元素时不用 IN，便于 EXPLAIN 走 index 等值匹配
		conds = append(conds, "error_code = ?")
		args = append(args, named[0])
	default:
		conds = append(conds, "error_code IN ?")
		args = append(args, named)
	}
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
	sql := `
SELECT
    (floor(created_at::FLOAT / ?) * ?)::BIGINT AS bucket_at,
    COUNT(*) AS err_count,
    COUNT(*) AS req_total,
    COALESCE(AVG(duration_ms), 0)::INT AS avg_duration_ms
FROM request_metrics_logs
WHERE ` + strings.Join(conds, " AND ") + `
GROUP BY bucket_at
ORDER BY bucket_at`
	var pts []TrendPoint
	if err := model.LOG_DB.WithContext(ctx).Raw(sql, args...).Scan(&pts).Error; err != nil {
		return nil, err
	}
	return pts, nil
}

// ===== Errors Detail =====

type ErrorDetailRow struct {
	RequestId    string `json:"request_id"`
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	ChannelId    int    `json:"channel_id"`
	ChannelType  int    `json:"channel_type"`
	PlatformName string `json:"platform_name" gorm:"-"`
	ModelName    string `json:"model_name"`
	StatusCode   int    `json:"status_code"`
	DurationMs   int    `json:"duration_ms"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    int64  `json:"created_at"`
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
