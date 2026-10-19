package controller

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type userQuotaHistoryItem struct {
	Id                       int      `json:"id"`
	CreatedAt                int64    `json:"created_at"`
	Type                     int      `json:"type"`
	DeltaQuota               *int64   `json:"delta_quota"`
	BeforeQuota              *int64   `json:"before_quota"`
	AfterQuota               *int64   `json:"after_quota"`
	Content                  string   `json:"content"`
	ModelName                string   `json:"model_name"`
	TokenName                string   `json:"token_name"`
	RequestId                string   `json:"request_id"`
	OperationType            *string  `json:"operation_type,omitempty"`
	QuotaType                *string  `json:"quota_type,omitempty"`
	RechargeInputAmount      *float64 `json:"recharge_input_amount,omitempty"`
	RechargeAfterRatioAmount *float64 `json:"recharge_after_ratio_amount,omitempty"`
	// Remark is the admin-entered note for a type=3 (manage) quota adjustment.
	// Pulled from Other.admin_info.remark so the frontend can render it in a
	// dedicated column instead of parsing the content suffix.
	Remark string `json:"remark,omitempty"`
}

// quotaAdjustRemarkSuffixRe matches the "，备注：xxx" suffix that the
// add_quota handler appends to the log content; stripped from the content
// field so remark isn't shown twice when it already has its own column.
var quotaAdjustRemarkSuffixRe = regexp.MustCompile(`，备注：[\s\S]*$`)

func extractQuotaHistoryRemark(log *model.Log) string {
	if log.Type != model.LogTypeManage || log.Other == "" {
		return ""
	}
	other, err := common.StrToMap(log.Other)
	if err != nil || other == nil {
		return ""
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		return ""
	}
	remark, _ := adminInfo["remark"].(string)
	return remark
}

func buildUserQuotaHistoryItems(logs []*model.Log, currentQuota int, newerDelta int64, hasUnknownNewer bool) []userQuotaHistoryItem {
	items := make([]userQuotaHistoryItem, 0, len(logs))
	runningQuota := int64(currentQuota) - newerDelta
	canDeriveBalance := !hasUnknownNewer

	for _, log := range logs {
		content := log.Content
		remark := extractQuotaHistoryRemark(log)
		if remark != "" {
			content = quotaAdjustRemarkSuffixRe.ReplaceAllString(content, "")
		}
		item := userQuotaHistoryItem{
			Id:                       log.Id,
			CreatedAt:                log.CreatedAt,
			Type:                     log.Type,
			Content:                  content,
			ModelName:                log.ModelName,
			TokenName:                log.TokenName,
			RequestId:                log.RequestId,
			OperationType:            log.OperationType,
			QuotaType:                log.QuotaType,
			RechargeInputAmount:      log.RechargeInputAmount,
			RechargeAfterRatioAmount: log.RechargeAfterRatioAmount,
			Remark:                   remark,
		}

		if log.BeforeQuota != nil && log.AfterQuota != nil {
			beforeQuota := *log.BeforeQuota
			afterQuota := *log.AfterQuota
			delta := afterQuota - beforeQuota
			item.DeltaQuota = &delta
			item.BeforeQuota = &beforeQuota
			item.AfterQuota = &afterQuota
			runningQuota = beforeQuota
			canDeriveBalance = true
			items = append(items, item)
			continue
		}

		var delta *int64
		switch log.Type {
		case model.LogTypeConsume:
			value := -int64(log.Quota)
			delta = &value
		case model.LogTypeRefund:
			value := int64(log.Quota)
			delta = &value
		case model.LogTypeManage, model.LogTypeTopup:
			if log.Quota != 0 {
				value := int64(log.Quota)
				delta = &value
			}
		}
		item.DeltaQuota = delta

		if delta == nil {
			canDeriveBalance = false
		} else if canDeriveBalance {
			afterQuota := runningQuota
			beforeQuota := afterQuota - *delta
			item.BeforeQuota = &beforeQuota
			item.AfterQuota = &afterQuota
			runningQuota = beforeQuota
		}

		items = append(items, item)
	}

	return items
}

func GetUserQuotaHistory(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}

	// Optional type filter: topup / consume / refund / manage. 0 or missing = no filter.
	var filterType int
	if raw := c.Query("type"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || !model.IsUserQuotaHistoryFilterType(parsed) {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		filterType = parsed
	}

	pageInfo := common.GetPageQuery(c)
	logs, total, err := model.GetUserQuotaHistoryFiltered(userId, filterType, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Running-balance derivation assumes we see every history entry; once a
	// filter is applied we can no longer reconstruct balances from deltas, so
	// only stored snapshots are trusted (newerDelta=0, hasUnknownNewer=true).
	var newerDelta int64
	hasUnknownNewer := filterType != 0
	if filterType == 0 && len(logs) > 0 && (logs[0].BeforeQuota == nil || logs[0].AfterQuota == nil) {
		newerDelta, hasUnknownNewer, err = model.GetUserQuotaHistoryNewerDelta(userId, logs[0].Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	items := buildUserQuotaHistoryItems(logs, user.Quota, newerDelta, hasUnknownNewer)

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": struct {
			*common.PageInfo
			CurrentQuota int `json:"current_quota"`
		}{
			PageInfo:     pageInfo,
			CurrentQuota: user.Quota,
		},
	})
}
