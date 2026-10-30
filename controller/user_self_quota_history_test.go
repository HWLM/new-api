package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func performSelfQuotaHistoryRequest(t *testing.T, userId int, query string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/quota-history?"+query, nil)
	c.Set("id", userId)
	GetSelfQuotaHistory(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

func seedSelfQuotaHistory(t *testing.T) (self model.User, other model.User) {
	t.Helper()
	db := setupManageUserTestDB(t)
	self = model.User{Username: "self-history-user", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: 3000, AffCode: "selfhist1"}
	other = model.User{Username: "other-history-user", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", Quota: 9000, AffCode: "otherhist1"}
	require.NoError(t, db.Create(&self).Error)
	require.NoError(t, db.Create(&other).Error)

	before, after := int64(1000), int64(3000)
	opType := model.OperationTypeQuota
	gift := model.QuotaTypeGift
	other_ := `{"admin_info":{"remark":"secret note"}}`
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: self.Id, Type: model.LogTypeTopup, Quota: 2000, BeforeQuota: &before, AfterQuota: &after, Content: "topup detail", RequestId: "req-1"},
		{UserId: self.Id, Type: model.LogTypeManage, Quota: 500, OperationType: &opType, QuotaType: &gift, Content: "admin adjust，备注：secret note", Other: other_},
		{UserId: self.Id, Type: model.LogTypeConsume, Quota: 100, ModelName: "gpt-4o"},
		{UserId: self.Id, Type: model.LogTypeRefund, Quota: 50},
		{UserId: other.Id, Type: model.LogTypeTopup, Quota: 7000},
	}).Error)
	return self, other
}

func TestGetSelfQuotaHistoryTopupFilterReturnsOwnTopupsWithoutAdminDetails(t *testing.T) {
	self, _ := seedSelfQuotaHistory(t)

	body := performSelfQuotaHistoryRequest(t, self.Id, "type=1&p=1&page_size=10")
	require.Equal(t, true, body["success"])
	data := body["data"].(map[string]any)
	assert.EqualValues(t, 1, data["total"])
	assert.EqualValues(t, 3000, data["current_quota"])

	items := data["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	assert.EqualValues(t, model.LogTypeTopup, item["type"])
	assert.EqualValues(t, 2000, item["delta_quota"])
	assert.EqualValues(t, 1000, item["before_quota"])
	assert.EqualValues(t, 3000, item["after_quota"])
	for _, hidden := range []string{"remark", "content", "model_name", "token_name", "request_id", "operation_type", "recharge_input_amount"} {
		assert.NotContains(t, item, hidden)
	}
}

func TestGetSelfQuotaHistoryDefaultsToQuotaAdjustments(t *testing.T) {
	self, _ := seedSelfQuotaHistory(t)

	body := performSelfQuotaHistoryRequest(t, self.Id, "p=1&page_size=10")
	require.Equal(t, true, body["success"])
	items := body["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.EqualValues(t, model.LogTypeManage, items[0].(map[string]any)["type"])
}

func TestGetSelfQuotaHistoryManageFilterHidesRemark(t *testing.T) {
	self, _ := seedSelfQuotaHistory(t)

	body := performSelfQuotaHistoryRequest(t, self.Id, "type=3&quota_type="+model.QuotaTypeGift)
	require.Equal(t, true, body["success"])
	items := body["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	item := items[0].(map[string]any)
	assert.Equal(t, model.QuotaTypeGift, item["quota_type"])
	assert.NotContains(t, item, "remark")
	assert.NotContains(t, item, "content")
}

func TestGetSelfQuotaHistoryRejectsUnsupportedFilters(t *testing.T) {
	self, _ := seedSelfQuotaHistory(t)

	for _, query := range []string{
		"type=2",
		"type=6",
		"type=0",
		"type=all",
		"type=1&quota_type=" + model.QuotaTypeGift,
		"type=3&quota_type=回款",
	} {
		body := performSelfQuotaHistoryRequest(t, self.Id, query)
		assert.Equal(t, false, body["success"], query)
		assert.Nil(t, body["data"], query)
	}
}
