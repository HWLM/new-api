package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserQuotaHistoryFiltersAndPaginatesQuotaChanges(t *testing.T) {
	truncateTables(t)

	quotaOperation := OperationTypeQuota
	otherOperation := "security"
	beforeQuota := int64(8000)
	afterQuota := int64(3000)
	require.NoError(t, LOG_DB.Create([]Log{
		{Id: 1, UserId: 201, Type: LogTypeConsume, Quota: 5000, BeforeQuota: &beforeQuota, AfterQuota: &afterQuota, Content: "consume"},
		{Id: 2, UserId: 201, Type: LogTypeRefund, Quota: 2000, Content: "refund"},
		{Id: 3, UserId: 201, Type: LogTypeManage, Quota: -1000, OperationType: &quotaOperation, Content: "adjust"},
		{Id: 4, UserId: 201, Type: LogTypeTopup, Content: "topup"},
		{Id: 5, UserId: 201, Type: LogTypeManage, OperationType: &otherOperation, Content: "security"},
		{Id: 6, UserId: 201, Type: LogTypeError, Content: "error"},
		{Id: 7, UserId: 202, Type: LogTypeConsume, Quota: 9000, Content: "other user"},
	}).Error)

	firstPage, total, err := GetUserQuotaHistory(201, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, firstPage, 2)
	assert.Equal(t, []int{4, 3}, []int{firstPage[0].Id, firstPage[1].Id})

	secondPage, total, err := GetUserQuotaHistory(201, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, secondPage, 2)
	assert.Equal(t, []int{2, 1}, []int{secondPage[0].Id, secondPage[1].Id})
	require.NotNil(t, secondPage[1].BeforeQuota)
	require.NotNil(t, secondPage[1].AfterQuota)
	assert.Equal(t, beforeQuota, *secondPage[1].BeforeQuota)
	assert.Equal(t, afterQuota, *secondPage[1].AfterQuota)

	newerDelta, hasUnknown, err := GetUserQuotaHistoryNewerDelta(201, secondPage[0].Id)
	require.NoError(t, err)
	assert.Equal(t, int64(-1000), newerDelta)
	assert.True(t, hasUnknown)
}
