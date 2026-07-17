package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// callGetInternalRefundStatus 用 httptest 触发 handler，返回 response recorder + 解码后的 body。
// body 反序列化失败会 t.Fatal，避免测试代码到处堆 err。
func callGetInternalRefundStatus(t *testing.T, requestBody string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/internal/logs/refund-status",
		bytes.NewBufferString(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")

	GetInternalRefundStatus(c)

	var body map[string]any
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "response body must be JSON")
	}
	return w, body
}

func TestInternalRefundStatus_InvalidBody(t *testing.T) {
	w, body := callGetInternalRefundStatus(t, `{not-json`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, body["error"], "invalid request body")
}

func TestInternalRefundStatus_EmptyIDsReturnEmptyResults(t *testing.T) {
	w, body := callGetInternalRefundStatus(t, `{"request_ids": []}`)
	assert.Equal(t, http.StatusOK, w.Code,
		"空数组不算错误：客户端可能刚好这一批没候选，应正常返回空 results 而不是 400")

	results, ok := body["results"].(map[string]any)
	require.True(t, ok, "results 字段应为对象")
	assert.Empty(t, results)
}

func TestInternalRefundStatus_MissingIDsField(t *testing.T) {
	// 缺 request_ids 字段应等价空数组：不 400、返回空 map。
	// 避免下游发送空 batch 时把它当错误处理。
	w, body := callGetInternalRefundStatus(t, `{}`)
	assert.Equal(t, http.StatusOK, w.Code)
	results, ok := body["results"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, results)
}

func TestInternalRefundStatus_TooManyIDs(t *testing.T) {
	// 超过 maxRefundStatusBatch=500 应 400，避免超大 IN () 语句 + 响应体膨胀。
	ids := make([]string, maxRefundStatusBatch+1)
	for i := range ids {
		ids[i] = "req" + strings.Repeat("0", 20) + strconvItoa(i)
	}
	payload, err := json.Marshal(map[string]any{"request_ids": ids})
	require.NoError(t, err)

	w, body := callGetInternalRefundStatus(t, string(payload))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "too many request_ids", body["error"])
	assert.EqualValues(t, maxRefundStatusBatch, body["limit"])
}

func TestDedupNonEmpty_KeepsFirstOccurrence(t *testing.T) {
	got := dedupNonEmpty([]string{"a", "b", "", "a", "c", "b", ""})
	assert.Equal(t, []string{"a", "b", "c"}, got,
		"dedup 应保留首次出现顺序、丢空串；对账时 request_id 顺序对结果 map 无影响，但稳定性方便调试")
}

func TestDedupNonEmpty_EmptyInput(t *testing.T) {
	assert.Nil(t, dedupNonEmpty(nil), "nil 应直接返回 nil，不 panic")
	assert.Nil(t, dedupNonEmpty([]string{}), "空切片应返回 nil")
	assert.Empty(t, dedupNonEmpty([]string{"", "", ""}), "全空串应返回空切片")
}

// strconvItoa 是 strconv.Itoa 的极小内联替身，避免测试文件仅为拼一个数字导入整个 strconv。
func strconvItoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
