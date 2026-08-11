package service

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

func TestSplitUsageLogExportDayRanges(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Taipei")
	require.NoError(t, err)

	start := time.Date(2026, 7, 8, 10, 30, 0, 0, loc).Unix()
	end := time.Date(2026, 7, 10, 1, 15, 0, 0, loc).Unix()

	ranges := splitUsageLogExportDayRanges(start, end, loc)
	require.Len(t, ranges, 3)

	assert.Equal(t, "2026-07-08T00:00:00+08:00", ranges[0].dayStart.Format(time.RFC3339))
	assert.Equal(t, "2026-07-09T00:00:00+08:00", ranges[0].dayEnd.Format(time.RFC3339))
	assert.Equal(t, "2026-07-09T00:00:00+08:00", ranges[1].dayStart.Format(time.RFC3339))
	assert.Equal(t, "2026-07-10T00:00:00+08:00", ranges[1].dayEnd.Format(time.RFC3339))
	assert.Equal(t, "2026-07-10T00:00:00+08:00", ranges[2].dayStart.Format(time.RFC3339))
	assert.Equal(t, "2026-07-10T01:15:00+08:00", ranges[2].dayEnd.Format(time.RFC3339))
}

func TestBuildUsageLogExportArchiveBaseName(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Taipei")
	require.NoError(t, err)

	payload := UsageLogExportTaskPayload{
		StartTimestamp: time.Date(2026, 7, 8, 0, 0, 0, 0, loc).Unix(),
		EndTimestamp:   time.Date(2026, 7, 15, 0, 0, 0, 0, loc).Unix(),
		Username:       "mr li",
	}

	assert.Equal(t, "mr_li_2026-07-08_to_2026-07-14_usage", buildUsageLogExportArchiveBaseName(payload, loc))
}

func TestSanitizeUsageLogExportTableNameAddsValidPrefix(t *testing.T) {
	assert.Equal(t, "usage_2026_07_30_part01", sanitizeUsageLogExportTableName("2026_07_30_part01"))
	assert.Equal(t, "usage_export", sanitizeUsageLogExportTableName(""))
}

func TestBuildUsageLogExportFilesForDayIncludesConsumeAndRefundRows(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	model.DB, model.LOG_DB = nil, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
	})

	loc, err := time.LoadLocation("Asia/Taipei")
	require.NoError(t, err)
	createdAt := time.Date(2026, 7, 30, 12, 0, 0, 0, loc).Unix()
	require.NoError(t, db.Create(&[]model.Log{
		{
			UserId:           1,
			Username:         "mr_li",
			CreatedAt:        createdAt,
			Type:             model.LogTypeConsume,
			RequestId:        "req_consume",
			TokenName:        "test-token",
			ModelName:        "test-model",
			Group:            "default",
			PromptTokens:     120,
			CompletionTokens: 30,
			Quota:            500000,
			Other:            `{"input_tokens_total":120,"cache_tokens":20,"admin_info":{"usage_billing_path":"billing-usage-openai"}}`,
		},
		{
			UserId:    1,
			Username:  "mr_li",
			CreatedAt: createdAt,
			Type:      model.LogTypeRefund,
			RequestId: "req_refund",
			TokenName: "test-token",
			ModelName: "test-model",
			Group:     "default",
			Quota:     100000,
			Other:     "{}",
		},
		{
			UserId:    1,
			Username:  "mr_li",
			CreatedAt: createdAt,
			Type:      model.LogTypeTopup,
			RequestId: "req_topup",
			TokenName: "test-token",
			ModelName: "test-model",
			Group:     "default",
			Quota:     999999,
			Other:     "{}",
		},
	}).Error)

	files, rowCount, err := buildUsageLogExportFilesForDay(
		t.Context(),
		UsageLogExportTaskPayload{
			StartTimestamp: time.Date(2026, 7, 30, 0, 0, 0, 0, loc).Unix(),
			EndTimestamp:   time.Date(2026, 7, 31, 0, 0, 0, 0, loc).Unix(),
			Username:       "mr_li",
		},
		usageLogExportDayRange{
			dayStart: time.Date(2026, 7, 30, 0, 0, 0, 0, loc),
			dayEnd:   time.Date(2026, 7, 31, 0, 0, 0, 0, loc),
		},
		loc,
		"Asia/Taipei",
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, rowCount)
	require.Len(t, files, 1)

	xlsx, err := excelize.OpenReader(bytes.NewReader(files[0].content))
	require.NoError(t, err)
	defer xlsx.Close()
	sheetName := xlsx.GetSheetName(0)
	rows, err := xlsx.GetRows(sheetName)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 8)
	require.GreaterOrEqual(t, len(rows[5]), 20)
	require.GreaterOrEqual(t, len(rows[6]), 20)
	require.GreaterOrEqual(t, len(rows[7]), 20)
	assert.Equal(t, "类型", rows[5][1])
	assert.Equal(t, "req_consume", rows[6][0])
	assert.Equal(t, "消耗", rows[6][1])
	assert.Equal(t, "test-model", rows[6][3])
	assert.Equal(t, "100", rows[6][7])
	assert.Equal(t, "20", rows[6][11])
	assert.Equal(t, priceOrBlank(exportQuotaToUSD(500000)), rows[6][19])
	assert.Equal(t, "req_refund", rows[7][0])
	assert.Equal(t, "退款", rows[7][1])
	assert.Equal(t, "test-model", rows[7][3])
	assert.Equal(t, priceOrBlank(exportQuotaToUSD(-100000)), rows[7][19])
	totalQuotaCell, err := xlsx.GetCellValue(sheetName, "R3")
	require.NoError(t, err)
	assert.Equal(t, priceOrBlank(exportQuotaToUSD(400000)), totalQuotaCell)
	promptTokensCell, err := xlsx.GetCellValue(sheetName, "F3")
	require.NoError(t, err)
	assert.Equal(t, "100", promptTokensCell)

	zipBytes, _, err := zipUsageLogExportFiles(files, UsageLogExportTaskPayload{Username: "mr_li"}, loc)
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	rc, err := zr.File[0].Open()
	require.NoError(t, err)
	_, err = io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}
