package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callPatchInternalLogAccountIDs 用 httptest 触发 handler。
func callPatchInternalLogAccountIDs(t *testing.T, requestBody string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/internal/logs/patch-account",
		bytes.NewBufferString(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")

	PatchInternalLogAccountIDs(c)

	var body map[string]any
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "response body must be JSON")
	}
	return w, body
}

func TestPatchInternalLogAccountIDs_InvalidBody(t *testing.T) {
	w, body := callPatchInternalLogAccountIDs(t, `{not-json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, body["error"], "invalid request body")
}

func TestPatchInternalLogAccountIDs_EmptyItemsReturnsEmptyLists(t *testing.T) {
	// 空数组应返回空 matched / not_found，不做任何 DB 操作、也不算错误。
	// 这样 sub2api 侧空 batch 场景不会被误判成失败。
	w, body := callPatchInternalLogAccountIDs(t, `{"items": []}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, body["matched"])
	assert.Empty(t, body["not_found"])
}

func TestPatchInternalLogAccountIDs_MissingItemsField(t *testing.T) {
	// 与 refund-status 一致：完全缺字段等价空数组。
	w, body := callPatchInternalLogAccountIDs(t, `{}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, body["matched"])
	assert.Empty(t, body["not_found"])
}

func TestPatchInternalLogAccountIDs_TooManyItems(t *testing.T) {
	// 超过 maxPatchAccountBatch=500 应 400，避免单条 SQL 参数膨胀 + 响应体过大。
	items := make([]map[string]any, maxPatchAccountBatch+1)
	for i := range items {
		items[i] = map[string]any{"request_id": "req" + strconvItoa(i), "account_id": int64(i + 1)}
	}
	payload, err := json.Marshal(map[string]any{"items": items})
	require.NoError(t, err)

	w, body := callPatchInternalLogAccountIDs(t, string(payload))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "too many items", body["error"])
	assert.EqualValues(t, maxPatchAccountBatch, body["limit"])
}

func TestPatchInternalLogAccountIDs_EmptyRequestIDIgnoredAndDeduped(t *testing.T) {
	// 空 request_id 应被丢弃；重复 request_id 应去重（controller 层的第一道防线，
	// 防止 sub2api 侧偶发重复导致 UPDATE ... CASE WHEN 出现同 key 多分支）。
	// 全部条目都被去掉后，等价空 batch，200 + 空 matched/not_found，不触达 DB。
	items := []map[string]any{
		{"request_id": "", "account_id": 1},
		{"request_id": "", "account_id": 2},
	}
	payload, err := json.Marshal(map[string]any{"items": items})
	require.NoError(t, err)

	w, body := callPatchInternalLogAccountIDs(t, string(payload))
	assert.Equal(t, http.StatusOK, w.Code, "全空 request_id 应视为空 batch")
	assert.Empty(t, body["matched"])
	assert.Empty(t, body["not_found"])
}
