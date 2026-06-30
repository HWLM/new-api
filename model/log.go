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

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		pattern, err := sanitizeLikePattern(value)
		if err != nil {
			return nil, err
		}
		return tx.Where(column+" LIKE ? ESCAPE '!'", pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
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
)

// OperationType 子类型标记：用于在 LogTypeManage 等大类下进一步区分具体业务动作。
// 写入到 logs.operation_type 列，未标记的历史/其他日志为 NULL。
const (
	OperationTypeQuota = "额度" // 管理员调整用户额度（add / subtract / override）
)

// QuotaType 进一步区分「调整额度（add 模式）」的来源分类：充值 vs 赠送。
// 仅在 add 模式写入；subtract / override 留 NULL。
const (
	QuotaTypeRecharge = "充值"
	QuotaTypeGift     = "赠送"
)

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = startIdx + i + 1
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
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
	err := LOG_DB.Create(log).Error
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
	if err := LOG_DB.Create(log).Error; err != nil {
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
	err := LOG_DB.Create(log).Error
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
	err := LOG_DB.Create(log).Error
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
		CreatedAt:        common.GetTimestamp(),
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
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		gopool.Go(func() {
			ts := common.GetTimestamp()
			totalTokens := params.PromptTokens + params.CompletionTokens
			LogQuotaData(userId, username, params.ModelName, params.Quota, ts, totalTokens)
			LogTokenQuotaData(userId, params.TokenId, params.TokenName, params.Group, params.Quota, ts, totalTokens)
		})
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
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
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
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
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
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
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

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, excludeManageType bool) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}
	// 非管理员在「全部类型」下也不应看到管理类（type=3）日志，这里统一兜底过滤。
	// 已显式指定 logType 时本就不会命中 type=3，因此该条件不影响其他场景。
	if excludeManageType && logType == LogTypeUnknown {
		tx = tx.Where("logs.type <> ?", LogTypeManage)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
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
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
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
	if common.UsingPostgreSQL {
		// PostgreSQL：other 是 text，先用正则确认形如 JSON 对象再 cast，避免对非 JSON 行直接 cast 报错。
		return `CASE WHEN other ~ '^\s*\{' THEN COALESCE(NULLIF(other::jsonb->>'cache_tokens','')::bigint, 0) ELSE 0 END` +
			` + CASE WHEN other ~ '^\s*\{' THEN COALESCE(NULLIF(other::jsonb->>'cache_creation_tokens','')::bigint, 0) ELSE 0 END`
	}
	if common.UsingMySQL {
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

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("sum(quota) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

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
		subQuery := LOG_DB.Table("logs").
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

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
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

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		if nil != result.Error {
			return total, result.Error
		}

		total += result.RowsAffected

		if result.RowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}
