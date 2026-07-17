// Package controller 中的内部对账接口。
//
// 该文件提供的 /internal/logs/refund-status 端点由**下游服务**（例如 sub2api）
// 用来判断某个 request_id 是否在 newApi 侧触发了退费/未计费，从而在其自身的
// 统计口径中排除这类被下游退了款的请求。
//
// # 安全注意
//
// 这些端点故意**不带鉴权**（不校验 token / user auth）：它们只做只读查询、
// 不返回敏感字段（用户名 / IP / prompt 内容全部剥离），只暴露对账所需的最少
// 字段（type / quota / refund_reason）。前提是部署时把 /internal/* 绑定在
// **内网监听 / K8s ClusterIP / 反向代理白名单** 之内，禁止公网直连。
//
// 若未来需要暴露到公网，应在此处加 IP 白名单 / 内部 JWT / mTLS。
package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// maxRefundStatusBatch 单次请求最大 request_id 数量。
// 上限用来避免超大 IN () 语句 + 响应体膨胀。sub2api 侧默认每轮对账
// 5000 条候选，分 10 批就够；也留了余地方便手动补跑。
const maxRefundStatusBatch = 500

// InternalRefundStatusRequest 请求体：待查的 request_id 列表。
type InternalRefundStatusRequest struct {
	RequestIDs []string `json:"request_ids"`
}

// InternalRefundStatusEntry 单个 request_id 的对账结果。
// Found=false 时其他字段无意义（客户端应据此判定「newApi 侧尚无记录」）。
type InternalRefundStatusEntry struct {
	Found        bool   `json:"found"`
	LogType      int    `json:"log_type,omitempty"`
	Quota        int    `json:"quota,omitempty"`
	RefundReason string `json:"refund_reason,omitempty"`
}

// InternalRefundStatusResponse 批量响应：以 request_id 为 key 的结果 map。
// 未在 map 中出现的 request_id 视为 found=false（等价明确返回 {found:false}）。
type InternalRefundStatusResponse struct {
	Results map[string]InternalRefundStatusEntry `json:"results"`
}

// GetInternalRefundStatus 处理 POST /internal/logs/refund-status。
//
// 请求体：{"request_ids": ["...", "..."]}（最多 maxRefundStatusBatch 个）
// 响应体：{"results": {"<request_id>": {found, log_type, quota, refund_reason}}}
//
// refund_reason 取自 logs.other.refund_reason；不存在时为空串。
func GetInternalRefundStatus(c *gin.Context) {
	var req InternalRefundStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}
	ids := dedupNonEmpty(req.RequestIDs)
	if len(ids) == 0 {
		c.JSON(http.StatusOK, InternalRefundStatusResponse{Results: map[string]InternalRefundStatusEntry{}})
		return
	}
	if len(ids) > maxRefundStatusBatch {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "too many request_ids",
			"limit": maxRefundStatusBatch,
			"given": len(ids),
		})
		return
	}

	rows, err := model.LookupRefundStatusByRequestIDs(c.Request.Context(), ids)
	if err != nil {
		common.SysError("internal refund status lookup failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed"})
		return
	}

	results := make(map[string]InternalRefundStatusEntry, len(ids))
	for _, id := range ids {
		results[id] = InternalRefundStatusEntry{Found: false}
	}
	for _, row := range rows {
		reason := ""
		if row.Other != "" {
			reason = gjson.Get(row.Other, "refund_reason").String()
		}
		results[row.RequestID] = InternalRefundStatusEntry{
			Found:        true,
			LogType:      row.Type,
			Quota:        row.Quota,
			RefundReason: reason,
		}
	}

	c.JSON(http.StatusOK, InternalRefundStatusResponse{Results: results})
}

// dedupNonEmpty 去重 + 去空串，保持稳定顺序。
func dedupNonEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
