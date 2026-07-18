package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// maxPatchAccountBatch 单次请求最大 (request_id, account_id) 条数。
// 与 refund-status 端点保持一致的上限。
const maxPatchAccountBatch = 500

// InternalPatchLogAccountItem 单条待回填条目。
type InternalPatchLogAccountItem struct {
	RequestID string `json:"request_id"`
	AccountID int64  `json:"account_id"`
}

// InternalPatchLogAccountRequest 批量回填请求体。
type InternalPatchLogAccountRequest struct {
	Items []InternalPatchLogAccountItem `json:"items"`
}

// InternalPatchLogAccountResponse 批量回填响应体。
//   - matched   实际命中并已 UPDATE 的 request_id 列表
//   - not_found 在 logs 表中查不到的 request_id 列表（sub2api 侧可决定重推 / 丢弃）
type InternalPatchLogAccountResponse struct {
	Matched  []string `json:"matched"`
	NotFound []string `json:"not_found"`
}

// PatchInternalLogAccountIDs 处理 POST /internal/logs/patch-account。
//
// 请求体：{"items": [{"request_id": "...", "account_id": 123}, ...]}
// 响应体：{"matched": [...], "not_found": [...]}
//
// 语义与安全约束见 router/internal-router.go 文件注释。
func PatchInternalLogAccountIDs(c *gin.Context) {
	var req InternalPatchLogAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	patches := make([]model.LogAccountIDPatch, 0, len(req.Items))
	seen := make(map[string]struct{}, len(req.Items))
	for _, it := range req.Items {
		if it.RequestID == "" {
			continue
		}
		if _, ok := seen[it.RequestID]; ok {
			continue
		}
		seen[it.RequestID] = struct{}{}
		patches = append(patches, model.LogAccountIDPatch{RequestID: it.RequestID, AccountID: it.AccountID})
	}
	if len(patches) == 0 {
		c.JSON(http.StatusOK, InternalPatchLogAccountResponse{Matched: []string{}, NotFound: []string{}})
		return
	}
	if len(patches) > maxPatchAccountBatch {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "too many items",
			"limit": maxPatchAccountBatch,
			"given": len(patches),
		})
		return
	}

	matched, notFound, err := model.PatchLogAccountIDs(c.Request.Context(), patches)
	if err != nil {
		common.SysError("internal patch log account_ids failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "patch failed"})
		return
	}
	if matched == nil {
		matched = []string{}
	}
	if notFound == nil {
		notFound = []string{}
	}
	c.JSON(http.StatusOK, InternalPatchLogAccountResponse{Matched: matched, NotFound: notFound})
}
