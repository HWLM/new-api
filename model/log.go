package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		condition, pattern, err := buildLogLikeCondition(column, value)
		if err != nil {
			return nil, err
		}
		return tx.Where(condition, pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

func buildLogLikeCondition(column string, value string) (string, string, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		pattern, err := sanitizeClickHouseLikePattern(value)
		if err != nil {
			return "", "", err
		}
		return column + " LIKE ?", pattern, nil
	}

	pattern, err := sanitizeLikePattern(value)
	if err != nil {
		return "", "", err
	}
	return column + " LIKE ? ESCAPE '!'", pattern, nil
}

func sanitizeClickHouseLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `_`, `\_`)

	if err := validateLikePattern(input); err != nil {
		return "", err
	}
	return input, nil
}

type Log struct {
	Id                int     `json:"id" gorm:"index:idx_created_at_id,priority:1;index:idx_user_id_id,priority:2"`
	UserId            int     `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt         int64   `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:2;index:idx_created_at_type"`
	Type              int     `json:"type" gorm:"index:idx_created_at_type"`
	Content           string  `json:"content"`
	Username          string  `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string  `json:"token_name" gorm:"index;default:''"`
	ModelName         string  `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int     `json:"quota" gorm:"default:0"`
	OperationType     *string `json:"operation_type,omitempty" gorm:"type:varchar(32);index"`
	QuotaType         *string `json:"quota_type,omitempty" gorm:"type:varchar(16);index"`
	PromptTokens      int     `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int     `json:"completion_tokens" gorm:"default:0"`
	UseTime           int     `json:"use_time" gorm:"default:0"`
	IsStream          bool    `json:"is_stream"`
	ChannelId         int     `json:"channel" gorm:"index"`
	ChannelName       string  `json:"channel_name" gorm:"->"`
	TokenId           int     `json:"token_id" gorm:"default:0;index"`
	Group             string  `json:"group" gorm:"index"`
	Ip                string  `json:"ip" gorm:"index;default:''"`
	RequestId         string  `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string  `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string  `json:"other"`
	// AccountId 由 sub2api 侧对账任务回填：sub2api usage_logs.account_id。
	// 未同步 / 非 sub2api 渠道 / 上游未选到号 时为 0。
	AccountId int64 `json:"account_id" gorm:"default:0;index:idx_logs_account_id"`
	// RechargeInputAmount 记录管理员在前端「调整额度」页面实际填写的金额（人民币 ¥）。
	// 仅在 action=add_quota + mode=add 时写入；其他场景为 NULL。
	RechargeInputAmount *float64 `json:"recharge_input_amount,omitempty" gorm:"default:NULL"`
	// RechargeAfterRatioAmount 记录按充值比例换算之后的金额（人民币 ¥），即 RechargeInputAmount / ratio。
	// 仅在 action=add_quota + mode=add 时写入；其他场景为 NULL。
	RechargeAfterRatioAmount *float64 `json:"recharge_after_ratio_amount,omitempty" gorm:"default:NULL"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
	LogTypeLogin   = 7
)

// OperationType 子类型标记：用于在 LogTypeManage 等大类下进一步区分具体业务动作。
// 写入到 logs.operation_type 列，未标记的历史/其他日志为 NULL。
// QuotaType 进一步区分「调整额度（add 模式）」的来源分类：充值 vs 赠送。
// 仅在 add 模式写入；subtract / override 留 NULL。
const (
	OperationTypeQuota = "额度" // 管理员调整用户额度（add / subtract / override）
	QuotaTypeRecharge  = "充值"
	QuotaTypeGift      = "赠送"
)

// NetQuotaSumTypes 返回"净消耗"统计涉及的日志 type 列表。
// 视频等异步任务在完成时若实际扣费小于预扣，会写一条 LogTypeRefund 抵消差额；
// 因此运营向的消耗统计需要同时纳入 LogTypeConsume 和 LogTypeRefund，再靠符号累加。
func NetQuotaSumTypes() []int {
	return []int{LogTypeConsume, LogTypeRefund}
}

// NetQuotaSumExpr 返回"净消耗"求和 SQL 表达式（不含别名）：
//
//	COALESCE(SUM(CASE WHEN type=LogTypeConsume THEN quota
//	                  WHEN type=LogTypeRefund  THEN -quota END), 0)
//
// 调用方需自行在 WHERE 中限定 type IN NetQuotaSumTypes()，
// 并配合 request_count/tokens 的 type=LogTypeConsume 过滤（退款日志不算新请求、tokens 恒为 0）。
// CASE WHEN 是 SQL 标准语法，SQLite/MySQL/PostgreSQL 均支持。
func NetQuotaSumExpr() string {
	return fmt.Sprintf(
		"COALESCE(SUM(CASE WHEN type = %d THEN quota WHEN type = %d THEN -quota ELSE 0 END), 0)",
		LogTypeConsume, LogTypeRefund,
	)
}

func ensureLogRequestId(log *Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func createLog(log *Log) error {
	ensureLogRequestId(log)
	return LOG_DB.Create(log).Error
}

// RefundStatusRow 是 LookupRefundStatusByRequestIDs 返回的最小字段集合，
// 只暴露对账所需字段，避免把 prompt 内容 / username / ip 等敏感字段带出。
// Other 会保留原始 JSON 字符串；调用方用 gjson 提取 refund_reason 或其他字段。
type RefundStatusRow struct {
	RequestID string `gorm:"column:request_id"`
	Type      int    `gorm:"column:type"`
	Quota     int    `gorm:"column:quota"`
	Other     string `gorm:"column:other"`
}

// LookupRefundStatusByRequestIDs 批量查询 logs 表，用于下游服务（sub2api）
// 对账判定「newApi 是否已计费/已退费」。
//
// 语义：
//   - 对每个 request_id 取「创建时间最早」的一行（历史约定：正常一次请求只
//     写一条日志；重试/边界场景可能多条，取最早的作为主判定，避免旧退费日志
//     被后写入的错误日志覆盖）。
//   - 只返回 request_id 匹配的行；未匹配的 request_id 由 controller 层
//     显式填 Found=false。
//
// 该函数不加限流；调用方（controller）自己控制批大小。
func LookupRefundStatusByRequestIDs(ctx context.Context, requestIDs []string) ([]RefundStatusRow, error) {
	if len(requestIDs) == 0 {
		return nil, nil
	}
	// 只 SELECT 对账所需的最少字段，避免把 prompt / ip / username 等敏感数据带出。
	// ORDER BY id ASC 表达「取最早写入的一条」，配合分组语义在应用层去重。
	var rows []RefundStatusRow
	err := LOG_DB.WithContext(ctx).
		Table("logs").
		Select("request_id, type, quota, other").
		Where("request_id IN ?", requestIDs).
		Order("id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("lookup refund status by request_ids (n=%d): %w", len(requestIDs), err)
	}

	// 同一 request_id 若存在多条，只保留最早的一条（前面 ORDER BY id ASC）。
	seen := make(map[string]struct{}, len(rows))
	deduped := make([]RefundStatusRow, 0, len(rows))
	for _, r := range rows {
		if _, ok := seen[r.RequestID]; ok {
			continue
		}
		seen[r.RequestID] = struct{}{}
		deduped = append(deduped, r)
	}
	return deduped, nil
}

// LogAccountIDPatch 描述一条待回填的 (request_id → account_id) 映射。
// 由 sub2api 侧的 push_account_to_newapi 任务批量投递。
type LogAccountIDPatch struct {
	RequestID string
	AccountID int64
}

// patchLogAccountIDsChunkSize 单条 UPDATE 语句最多带多少个 request_id。
// 每条命中 3 个占位符（CASE WHEN request_id THEN account_id + IN(request_id)），
// 200 → 600 占位符，在 SQLite/MySQL/PostgreSQL 参数上限内保守。
const patchLogAccountIDsChunkSize = 200

// PatchLogAccountIDs 批量回填 logs 表的 account_id 字段。
//
// 语义：
//   - 只更新已存在的 request_id；未找到的通过 notFound 返回给调用方。
//   - 同一 request_id 若在 logs 中有多条，全部改为同一个 account_id（幂等）。
//   - ClickHouse 日志库不支持在线 UPDATE，直接返回错误，调用方（sub2api）
//     应把这类部署跳过。
//
// 单次调用最多不超过 sub2api 端约定的批大小；内部再按 patchLogAccountIDsChunkSize
// 分批执行以避免单条 SQL 参数过多。
func PatchLogAccountIDs(ctx context.Context, patches []LogAccountIDPatch) (matched, notFound []string, err error) {
	if len(patches) == 0 {
		return nil, nil, nil
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return nil, nil, errors.New("clickhouse log db does not support account_id patch")
	}

	dedup := make(map[string]int64, len(patches))
	for _, p := range patches {
		rid := strings.TrimSpace(p.RequestID)
		if rid == "" {
			continue
		}
		dedup[rid] = p.AccountID
	}
	if len(dedup) == 0 {
		return nil, nil, nil
	}

	ids := make([]string, 0, len(dedup))
	for rid := range dedup {
		ids = append(ids, rid)
	}

	type existingRow struct {
		RequestID string `gorm:"column:request_id"`
	}
	var existing []existingRow
	if err := LOG_DB.WithContext(ctx).
		Table("logs").
		Select("DISTINCT request_id").
		Where("request_id IN ?", ids).
		Scan(&existing).Error; err != nil {
		return nil, nil, fmt.Errorf("lookup existing logs for account patch: %w", err)
	}
	existSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existSet[e.RequestID] = struct{}{}
	}

	matched = make([]string, 0, len(existSet))
	for rid := range dedup {
		if _, ok := existSet[rid]; ok {
			matched = append(matched, rid)
		} else {
			notFound = append(notFound, rid)
		}
	}
	if len(matched) == 0 {
		return matched, notFound, nil
	}

	for offset := 0; offset < len(matched); offset += patchLogAccountIDsChunkSize {
		end := offset + patchLogAccountIDsChunkSize
		if end > len(matched) {
			end = len(matched)
		}
		chunk := matched[offset:end]

		var b strings.Builder
		args := make([]any, 0, len(chunk)*3)
		b.WriteString("UPDATE logs SET account_id = CASE request_id ")
		for _, rid := range chunk {
			b.WriteString("WHEN ? THEN ? ")
			args = append(args, rid, dedup[rid])
		}
		b.WriteString("END WHERE request_id IN (")
		for i, rid := range chunk {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("?")
			args = append(args, rid)
		}
		b.WriteString(")")

		if err := LOG_DB.WithContext(ctx).Exec(b.String(), args...).Error; err != nil {
			return nil, nil, fmt.Errorf("update logs account_id (chunk offset=%d n=%d): %w", offset, len(chunk), err)
		}
	}
	return matched, notFound, nil
}

func clickHouseLogOrder(prefix string) string {
	return prefix + "created_at desc, " + prefix + "request_id desc"
}

func assignDisplayLogIds(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].Id = startIdx + i + 1
	}
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// Remove operation-audit details (operator/route info), admin-only.
			delete(otherMap, "audit_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
	}
	assignDisplayLogIds(logs, startIdx)
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	order := "id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order(order).Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordManageLog 记录管理员操作类日志：type 固定为 LogTypeManage。
// opType 写入 operation_type 列作为子类型标记（如 OperationTypeQuota），便于按子类型筛选。
// quotaType 进一步区分 add 模式下的来源（充值/赠送），仅在 add 模式传入，否则传空。
// rechargeInputAmount / rechargeAfterRatioAmount 仅在「调整额度 add 模式」下传入有效值，
// 分别对应前端页面填写的原始金额与按充值比例换算之后的金额；其他场景传 nil 留空。
func RecordManageLog(userId int, content string, opType string, quotaType string, adminInfo map[string]interface{}, rechargeInputAmount *float64, rechargeAfterRatioAmount *float64) {
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:                   userId,
		Username:                 username,
		CreatedAt:                common.GetTimestamp(),
		Type:                     LogTypeManage,
		Content:                  content,
		RechargeInputAmount:      rechargeInputAmount,
		RechargeAfterRatioAmount: rechargeAfterRatioAmount,
	}
	if opType != "" {
		t := opType
		log.OperationType = &t
	}
	if quotaType != "" {
		q := quotaType
		log.QuotaType = &q
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// buildOpField 构建语言无关的操作描述（写入 Other.op）。
// 前端依据 action(稳定操作标识) + params(结构化参数) 在渲染期用 i18n 本地化展示，
// 因此不在数据库中存储自然语言句子。
func buildOpField(action string, params map[string]interface{}) map[string]interface{} {
	op := map[string]interface{}{
		"action": action,
	}
	if len(params) > 0 {
		op["params"] = params
	}
	return op
}

// RecordLoginLog 记录用户登录成功的审计日志（type=LogTypeLogin）。
// username 由调用方传入（登录流程已持有用户对象），避免额外的数据库查询。
// content 为英文兜底文本（用于导出/经典前端）；action+params 供前端本地化渲染。
// extra 可携带 login_method、user_agent 等附加信息（普通用户可见）。
func RecordLoginLog(userId int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	other := map[string]interface{}{}
	for k, v := range extra {
		other[k] = v
	}
	other["op"] = buildOpField(action, params)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeLogin,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record login log: " + err.Error())
	}
}

// RecordOperationAuditLog 记录管理/高危操作审计日志（type=LogTypeManage）。
// logUserId 为日志归属者，管理审计日志应归属实际操作者；目标资源/用户放入
// action params。username 内部按 logUserId 查询。content 为英文兜底文本（导出/经典前端用）。
// action+params 写入 Other.op，供前端本地化渲染（普通用户可见，不含敏感信息）。
// adminInfo 存放操作者身份（写入 Other.admin_info，普通用户查询时剥离）；
// auditInfo 存放路由/方法/结果等中间件兜底信息（写入 Other.audit_info，普通用户查询时剥离）。
func RecordOperationAuditLog(logUserId int, content string, ip string, action string, params map[string]interface{}, adminInfo map[string]interface{}, auditInfo map[string]interface{}) {
	username, _ := GetUsernameById(logUserId, false)
	other := map[string]interface{}{
		"op": buildOpField(action, params),
	}
	if len(adminInfo) > 0 {
		other["admin_info"] = adminInfo
	}
	if len(auditInfo) > 0 {
		other["audit_info"] = auditInfo
	}
	log := &Log{
		UserId:    logUserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        ip,
		Other:     common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record operation audit log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, common.LocalLogPreview(content)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	createdAt := common.GetTimestamp()
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        createdAt,
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		tokenUsed := params.PromptTokens + params.CompletionTokens
		LogQuotaData(QuotaDataLogParams{
			UserID:    userId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			TokenUsed: tokenUsed,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  common.NodeName,
		})
		LogTokenQuotaData(userId, params.TokenId, params.TokenName, params.Group, params.Quota, createdAt, tokenUsed)
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
	NodeName  string // 任务发起节点；为空时回退当前节点
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		LogQuotaData(QuotaDataLogParams{
			UserID:    params.UserId,
			Username:  username,
			ModelName: params.ModelName,
			Quota:     params.Quota,
			CreatedAt: createdAt,
			UseGroup:  params.Group,
			TokenID:   params.TokenId,
			ChannelID: params.ChannelId,
			NodeName:  nodeName,
		})
		LogTokenQuotaData(params.UserId, params.TokenId, tokenName, params.Group, params.Quota, createdAt, 0)
	}
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func applyLogUserScope(tx *gorm.DB, column string, userIds []int) *gorm.DB {
	if userIds == nil {
		return tx
	}
	if len(userIds) == 0 {
		return tx.Where("1 = 0")
	}
	return tx.Where(column+" IN ?", userIds)
}

func GetUserLogs(userIds []int, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, excludeManageType bool) (logs []*Log, total int64, err error) {
	if excludeManageType && logType == LogTypeManage {
		return []*Log{}, 0, nil
	}

	tx := applyLogUserScope(LOG_DB, "logs.user_id", userIds)
	if logType == LogTypeUnknown {
		if excludeManageType {
			tx = tx.Where("logs.type <> ?", LogTypeManage)
		}
	} else {
		tx = tx.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota     int   `json:"quota"`
	SubQuota  int   `json:"sub_quota"`
	SubTokens int64 `json:"sub_tokens"`
	Rpm       int   `json:"rpm"`
	Tpm       int   `json:"tpm"`
}

// cacheTokensInnerExpr 返回 other.cache_tokens + other.cache_creation_tokens
// 在三种数据库下的等价 SQL 片段（不含外层 SUM/COALESCE）。
func cacheTokensInnerExpr() string {
	if common.UsingLogDatabase(common.DatabaseTypePostgreSQL) {
		// PostgreSQL：other 是 text，先用正则确认形如 JSON 对象再 cast，避免对非 JSON 行直接 cast 报错。
		return `CASE WHEN other ~ '^\s*\{' THEN COALESCE(NULLIF(other::jsonb->>'cache_tokens','')::bigint, 0) ELSE 0 END` +
			` + CASE WHEN other ~ '^\s*\{' THEN COALESCE(NULLIF(other::jsonb->>'cache_creation_tokens','')::bigint, 0) ELSE 0 END`
	}
	if common.UsingLogDatabase(common.DatabaseTypeMySQL) {
		// MySQL 5.7.8+：用 JSON_VALID 保护，JSON_EXTRACT 通过 + 0 隐式转 number。
		return `CASE WHEN JSON_VALID(other) THEN COALESCE(JSON_EXTRACT(other, '$.cache_tokens') + 0, 0) ELSE 0 END` +
			` + CASE WHEN JSON_VALID(other) THEN COALESCE(JSON_EXTRACT(other, '$.cache_creation_tokens') + 0, 0) ELSE 0 END`
	}
	// SQLite：json_extract 对非 JSON 字符串返回 NULL，IFNULL 处理即可。
	return `IFNULL(CAST(json_extract(other, '$.cache_tokens') AS INTEGER), 0)` +
		` + IFNULL(CAST(json_extract(other, '$.cache_creation_tokens') AS INTEGER), 0)`
}

// subTokensExpr 返回按 channel 类型分支聚合 token 的 SQL 表达式。
// openaiIds 命中的行（channels.type=1，OpenAI 系）只计 prompt+completion，避免与上游已含的 cache 双倍叠加；
// 其余 sub 渠道仍计 prompt+completion+cache_tokens+cache_creation_tokens。
// 仅在 sub 渠道过滤后的子集上调用。channel_id 是 int，直接拼接安全。
func subTokensExpr(openaiIds, otherIds []int) string {
	cacheExpr := cacheTokensInnerExpr()
	if len(openaiIds) == 0 {
		return `COALESCE(SUM(prompt_tokens + completion_tokens + ` + cacheExpr + `), 0) AS sub_tokens`
	}
	if len(otherIds) == 0 {
		return `COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS sub_tokens`
	}
	return `COALESCE(SUM(CASE WHEN channel_id IN (` + intsToCSV(openaiIds) +
		`) THEN prompt_tokens + completion_tokens` +
		` ELSE prompt_tokens + completion_tokens + ` + cacheExpr +
		` END), 0) AS sub_tokens`
}

// intsToCSV 把 int 切片拼成逗号分隔字符串，用于直接嵌入 SQL 的 IN 列表。
func intsToCSV(ids []int) string {
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d", id)
	}
	return b.String()
}

// ResolveSubChannelIds 解析当前配置中 sub 渠道 tag 对应的渠道 ID 列表。
// 配置为空或没有任何匹配渠道时返回 nil（调用方据此跳过 sub 统计）。
func ResolveSubChannelIds() ([]int, error) {
	tags := operation_setting.GetSubChannelTags()
	if len(tags) == 0 {
		return nil, nil
	}
	var ids []int
	if err := DB.Model(&Channel{}).
		Where("tag IN ?", tags).
		Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// ResolveSubChannelIdsByType 解析 sub 渠道 ID 列表，并按 channels.type 拆成
// OpenAI 系（type=1）和其他两组。供 sub_tokens 按渠道类型差异化计算使用。
// 配置为空或没有任何匹配渠道时两个返回值均为 nil。
func ResolveSubChannelIdsByType() (openaiIds []int, otherIds []int, err error) {
	tags := operation_setting.GetSubChannelTags()
	if len(tags) == 0 {
		return nil, nil, nil
	}
	type row struct {
		Id   int
		Type int
	}
	var rows []row
	if err = DB.Model(&Channel{}).
		Where("tag IN ?", tags).
		Select("id, type").
		Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	const channelTypeOpenAI = 1
	for _, r := range rows {
		if r.Type == channelTypeOpenAI {
			openaiIds = append(openaiIds, r.Id)
		} else {
			otherIds = append(otherIds, r.Id)
		}
	}
	return openaiIds, otherIds, nil
}

func SumUsedQuota(userIds []int, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := applyLogUserScope(LOG_DB.Table("logs"), "user_id", userIds).
		Select("COALESCE(sum(quota), 0) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := applyLogUserScope(LOG_DB.Table("logs"), "user_id", userIds).
		Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm")

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	// sub 渠道统计：复用相同筛选条件，再叠加 channel_id IN sub_ids。
	// 按 channels.type 拆开是为了让 OpenAI 系（type=1）只算 prompt+completion，
	// 其他渠道仍叠加 cache_tokens + cache_creation_tokens。
	openaiSubIds, otherSubIds, subErr := ResolveSubChannelIdsByType()
	if subErr != nil {
		common.SysError("failed to resolve sub channel ids: " + subErr.Error())
	} else if len(openaiSubIds)+len(otherSubIds) > 0 {
		subIds := make([]int, 0, len(openaiSubIds)+len(otherSubIds))
		subIds = append(subIds, openaiSubIds...)
		subIds = append(subIds, otherSubIds...)
		subQuery := applyLogUserScope(LOG_DB.Table("logs"), "user_id", userIds).
			Select("COALESCE(SUM(quota), 0) AS sub_quota, " + subTokensExpr(openaiSubIds, otherSubIds))
		if subQuery, err = applyExplicitLogTextFilter(subQuery, "username", username); err != nil {
			return stat, err
		}
		if tokenName != "" {
			subQuery = subQuery.Where("token_name = ?", tokenName)
		}
		if startTimestamp != 0 {
			subQuery = subQuery.Where("created_at >= ?", startTimestamp)
		}
		if endTimestamp != 0 {
			subQuery = subQuery.Where("created_at <= ?", endTimestamp)
		}
		if subQuery, err = applyExplicitLogTextFilter(subQuery, "model_name", modelName); err != nil {
			return stat, err
		}
		if channel != 0 {
			subQuery = subQuery.Where("channel_id = ?", channel)
		}
		if group != "" {
			subQuery = subQuery.Where(logGroupCol+" = ?", group)
		}
		subQuery = subQuery.Where("type = ?", LogTypeConsume).
			Where("channel_id IN ?", subIds)

		var subStat struct {
			SubQuota  int   `gorm:"column:sub_quota"`
			SubTokens int64 `gorm:"column:sub_tokens"`
		}
		if err := subQuery.Scan(&subStat).Error; err != nil {
			common.SysError("failed to query sub channel stat: " + err.Error())
			return stat, errors.New("查询统计数据失败")
		}
		stat.SubQuota = subStat.SubQuota
		stat.SubTokens = subStat.SubTokens
	}

	return stat, nil
}

// AccountQuotaEntry 单个账号在时间窗内的 quota 聚合结果。
// 只在 SumUsedQuotaByAccountIDs 返回值里使用,给 sub2api ROI 统计做「按账号看当日实际消耗」用。
type AccountQuotaEntry struct {
	AccountID int64 `gorm:"column:account_id" json:"account_id"`
	Quota     int64 `gorm:"column:quota" json:"quota"`
}

// statByAccountsChunkSize 单批查询的 account_id 上限。
// 避免超大 IN 列表让 PG 优化器变慢、同时规避 pgx 单条 SQL 参数上限;
// sub2api 侧一次调用可能传几千甚至上万个 id,内部分批更稳。
const statByAccountsChunkSize = 1000

// SumUsedQuotaByAccountIDs 按 account_id 集合聚合 [start, end] 时间窗内的消耗 quota。
// 返回 (总 quota, 每个账号的 quota) — 后者只包含 quota > 0 的账号(对齐 sub2api HAVING SUM > 0 语义)。
//
// 只统计 type = LogTypeConsume 的日志,与 SumUsedQuota 保持一致。
// startTimestamp / endTimestamp 传 0 表示不加下/上界。
//
// 内部按 statByAccountsChunkSize 分批查库;不同 chunk 的 account_id 天然互不相交(sub2api 传的是
// 唯一集合),Go 层直接 append 无需 dedup。
func SumUsedQuotaByAccountIDs(ctx context.Context, accountIDs []int64, startTimestamp, endTimestamp int64) (int64, []AccountQuotaEntry, error) {
	if len(accountIDs) == 0 {
		return 0, nil, nil
	}

	perAccount := make([]AccountQuotaEntry, 0, len(accountIDs))
	var total int64

	for i := 0; i < len(accountIDs); i += statByAccountsChunkSize {
		end := i + statByAccountsChunkSize
		if end > len(accountIDs) {
			end = len(accountIDs)
		}
		batch := accountIDs[i:end]

		tx := LOG_DB.WithContext(ctx).Table("logs").
			Select("account_id, COALESCE(SUM(quota), 0) AS quota").
			Where("account_id IN ?", batch).
			Where("type = ?", LogTypeConsume)
		if startTimestamp != 0 {
			tx = tx.Where("created_at >= ?", startTimestamp)
		}
		if endTimestamp != 0 {
			tx = tx.Where("created_at <= ?", endTimestamp)
		}
		tx = tx.Group("account_id").Having("COALESCE(SUM(quota), 0) > 0")

		var batchRows []AccountQuotaEntry
		if err := tx.Scan(&batchRows).Error; err != nil {
			return 0, nil, fmt.Errorf("stat by accounts chunk offset=%d n=%d: %w", i, len(batch), err)
		}
		for _, r := range batchRows {
			total += r.Quota
			perAccount = append(perAccount, r)
		}
	}
	return total, perAccount, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func CountOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if nil != ctx.Err() {
		return 0, ctx.Err()
	}

	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// ClickHouse DELETE is a heavy mutation that rewrites data parts, so
		// per-batch mutations would be pathologically slow. Remove all matching
		// rows in a single synchronous mutation regardless of limit; the reported
		// count lets the caller's progress loop complete in one pass.
		total, err := CountOldLog(ctx, targetTimestamp)
		if err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.WithContext(ctx).Exec(
			"ALTER TABLE logs DELETE WHERE created_at < ? SETTINGS mutations_sync = 1",
			targetTimestamp,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
	if nil != result.Error {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}

	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		rowsAffected, err := DeleteOldLogBatch(ctx, targetTimestamp, limit)
		if nil != err {
			return total, err
		}

		total += rowsAffected

		if rowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}
