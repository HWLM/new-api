package service

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/objectstore"

	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const (
	usageLogExportTaskType        = model.SystemTaskTypeLogExport
	usageLogExportContentType     = "application/zip"
	usageLogExportDefaultPrefix   = "usage-logs-export"
	usageLogExportMaxRowsPerFile  = 20000
	usageLogExportQueryBatchSize  = 1000
	usageLogExportSheetTitleLabel = "消耗明细"
)

type UsageLogExportTaskPayload struct {
	StartTimestamp    int64  `json:"start_timestamp"`
	EndTimestamp      int64  `json:"end_timestamp"`
	Username          string `json:"username,omitempty"`
	TokenName         string `json:"token_name,omitempty"`
	ModelName         string `json:"model_name,omitempty"`
	Group             string `json:"group,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	UpstreamRequestID string `json:"upstream_request_id,omitempty"`
	Channel           int    `json:"channel,omitempty"`
}

type UsageLogExportTaskState struct {
	TotalDays     int    `json:"total_days"`
	ProcessedDays int    `json:"processed_days"`
	FileCount     int    `json:"file_count"`
	RowCount      int64  `json:"row_count"`
	CurrentDay    string `json:"current_day,omitempty"`
	CurrentFile   string `json:"current_file,omitempty"`
	Progress      int    `json:"progress"`
}

type UsageLogExportTaskResult struct {
	FileName    string `json:"file_name"`
	ObjectKey   string `json:"object_key"`
	DownloadURL string `json:"download_url"`
	FileCount   int    `json:"file_count"`
	RowCount    int64  `json:"row_count"`
}

type usageLogExportRow struct {
	ID               int64  `gorm:"column:id"`
	CreatedAt        int64  `gorm:"column:created_at"`
	Type             int    `gorm:"column:type"`
	RequestID        string `gorm:"column:request_id"`
	TokenName        string `gorm:"column:token_name"`
	ModelName        string `gorm:"column:model_name"`
	Username         string `gorm:"column:username"`
	Group            string `gorm:"column:group"`
	PromptTokens     int    `gorm:"column:prompt_tokens"`
	CompletionTokens int    `gorm:"column:completion_tokens"`
	Quota            int    `gorm:"column:quota"`
	Other            string `gorm:"column:other"`
}

type usageLogExportCursor struct {
	CreatedAt int64
	RequestID string
	ID        int64
}

type usageLogExportDayRange struct {
	dayStart time.Time
	dayEnd   time.Time
}

type usageLogExportArchiveFile struct {
	name    string
	content []byte
}

type usageLogExportOther struct {
	BillingMode           string   `json:"billing_mode"`
	ExprB64               string   `json:"expr_b64"`
	MatchedTier           string   `json:"matched_tier"`
	Claude                bool     `json:"claude,omitempty"`
	InputTokensTotal      int64    `json:"input_tokens_total,omitempty"`
	ModelRatio            *float64 `json:"model_ratio,omitempty"`
	CompletionRatio       *float64 `json:"completion_ratio,omitempty"`
	ModelPrice            *float64 `json:"model_price,omitempty"`
	GroupRatio            *float64 `json:"group_ratio,omitempty"`
	UserGroupRatio        *float64 `json:"user_group_ratio,omitempty"`
	CacheRatio            *float64 `json:"cache_ratio,omitempty"`
	CacheCreationRatio    *float64 `json:"cache_creation_ratio,omitempty"`
	CacheCreationRatio5m  *float64 `json:"cache_creation_ratio_5m,omitempty"`
	CacheCreationRatio1h  *float64 `json:"cache_creation_ratio_1h,omitempty"`
	CacheTokens           int64    `json:"cache_tokens,omitempty"`
	CacheCreationTokens   int64    `json:"cache_creation_tokens,omitempty"`
	CacheCreationTokens5m int64    `json:"cache_creation_tokens_5m,omitempty"`
	CacheCreationTokens1h int64    `json:"cache_creation_tokens_1h,omitempty"`
	AdminInfo             struct {
		UsageBillingPath string `json:"usage_billing_path"`
	} `json:"admin_info"`
}

type usageLogExportTotals struct {
	RequestCount       int64
	TotalTokens        int64
	PromptTokens       int64
	CompletionTokens   int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	CacheWrite5mTokens int64
	CacheWrite1hTokens int64
	TotalQuota         int64
}

type usageLogExportStyles struct {
	title        int
	header       int
	cell         int
	summary      int
	sectionTitle int
}

type usageLogExportWorkbook struct {
	file      *excelize.File
	sheetName string
	title     string
	partName  string
	rowIndex  int
	totals    usageLogExportTotals
	styles    usageLogExportStyles
}

func StartUsageLogExportTask(payload UsageLogExportTaskPayload) (*model.SystemTask, bool, error) {
	activeTask, err := model.GetActiveSystemTask(usageLogExportTaskType)
	if err != nil {
		return nil, false, err
	}
	if activeTask != nil {
		return activeTask, false, nil
	}

	task, err := model.CreateSystemTask(usageLogExportTaskType, payload, UsageLogExportTaskState{})
	if err != nil {
		activeTask, activeErr := model.GetActiveSystemTask(usageLogExportTaskType)
		if activeErr == nil && activeTask != nil {
			return activeTask, false, nil
		}
		return nil, false, err
	}
	notifySystemTaskRunner()
	return task, true, nil
}

func RunUsageLogExportTask(ctx context.Context, task *model.SystemTask, runnerID string) error {
	if task == nil {
		return fmt.Errorf("usage log export task is required")
	}
	payload := UsageLogExportTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		return err
	}
	if payload.StartTimestamp <= 0 || payload.EndTimestamp <= 0 {
		return fmt.Errorf("start_timestamp and end_timestamp are required")
	}
	if payload.EndTimestamp <= payload.StartTimestamp {
		return fmt.Errorf("end_timestamp must be greater than start_timestamp")
	}

	loc, locLabel := usageLogExportLocation()
	dayRanges := splitUsageLogExportDayRanges(payload.StartTimestamp, payload.EndTimestamp, loc)
	if len(dayRanges) == 0 {
		return fmt.Errorf("no exportable day range found")
	}

	state := UsageLogExportTaskState{TotalDays: len(dayRanges)}
	if err := updateUsageLogExportState(task.TaskID, runnerID, state); err != nil {
		return err
	}

	files := make([]usageLogExportArchiveFile, 0, len(dayRanges))
	totalRows := int64(0)
	totalFiles := 0

	for _, dayRange := range dayRanges {
		if err := ctx.Err(); err != nil {
			return err
		}

		dayFiles, dayRows, err := buildUsageLogExportFilesForDay(
			ctx,
			payload,
			dayRange,
			loc,
			locLabel,
		)
		if err != nil {
			return err
		}
		if len(dayFiles) > 0 {
			files = append(files, dayFiles...)
			totalFiles += len(dayFiles)
		}
		totalRows += dayRows

		state.ProcessedDays++
		state.FileCount = totalFiles
		state.RowCount = totalRows
		state.CurrentDay = dayRange.dayStart.Format("2006-01-02")
		if len(dayFiles) > 0 {
			state.CurrentFile = dayFiles[len(dayFiles)-1].name
		}
		state.Progress = logExportProgress(state.ProcessedDays, state.TotalDays)
		if err := updateUsageLogExportState(task.TaskID, runnerID, state); err != nil {
			return err
		}
	}

	if len(files) == 0 {
		fallbackDay := dayRanges[0].dayStart
		emptyFile, err := buildUsageLogExportWorkbookFile(
			payload,
			fallbackDay,
			fallbackDay.AddDate(0, 0, 1),
			locLabel,
			1,
			[]usageLogExportRow{},
		)
		if err != nil {
			return err
		}
		files = append(files, emptyFile)
		totalFiles = 1
	}

	zipBytes, archiveFileName, err := zipUsageLogExportFiles(files, payload, loc)
	if err != nil {
		return err
	}

	uploader, prefix, err := getUsageLogExportUploader()
	if err != nil {
		return err
	}
	objectKey := buildUsageLogExportObjectKey(prefix, task.TaskID, archiveFileName)
	downloadURL, err := uploader.Upload(ctx, objectstore.Object{
		Key:         objectKey,
		Content:     zipBytes,
		ContentType: usageLogExportContentType,
	})
	if err != nil {
		return err
	}

	result := UsageLogExportTaskResult{
		FileName:    archiveFileName,
		ObjectKey:   objectKey,
		DownloadURL: downloadURL,
		FileCount:   totalFiles,
		RowCount:    totalRows,
	}
	state.CurrentFile = archiveFileName
	state.Progress = 100
	if err := updateUsageLogExportState(task.TaskID, runnerID, state); err != nil {
		return err
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		return err
	}
	logger.LogInfo(ctx, fmt.Sprintf("usage log export succeeded: task_id=%s files=%d rows=%d key=%s", task.TaskID, totalFiles, totalRows, objectKey))
	return nil
}

func updateUsageLogExportState(taskID string, runnerID string, state UsageLogExportTaskState) error {
	return model.UpdateSystemTaskState(taskID, runnerID, state)
}

func usageLogExportLocation() (*time.Location, string) {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.Local, time.Local.String()
	}
	return loc, loc.String()
}

func splitUsageLogExportDayRanges(startTimestamp, endTimestamp int64, loc *time.Location) []usageLogExportDayRange {
	start := time.Unix(startTimestamp, 0).In(loc)
	end := time.Unix(endTimestamp, 0).In(loc)
	if !end.After(start) {
		return nil
	}

	dayStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	out := make([]usageLogExportDayRange, 0, 8)
	for current := dayStart; current.Before(end); current = current.AddDate(0, 0, 1) {
		nextDay := current.AddDate(0, 0, 1)
		rangeStart := current
		if start.After(rangeStart) {
			rangeStart = start
		}
		rangeEnd := nextDay
		if end.Before(rangeEnd) {
			rangeEnd = end
		}
		if rangeEnd.After(rangeStart) {
			out = append(out, usageLogExportDayRange{dayStart: current, dayEnd: rangeEnd})
		}
	}
	return out
}

func buildUsageLogExportFilesForDay(
	ctx context.Context,
	payload UsageLogExportTaskPayload,
	dayRange usageLogExportDayRange,
	loc *time.Location,
	locLabel string,
) ([]usageLogExportArchiveFile, int64, error) {
	baseTx, err := buildUsageLogExportQuery(payload, dayRange.dayStart.Unix(), dayRange.dayEnd.Unix())
	if err != nil {
		return nil, 0, err
	}

	cursor := usageLogExportCursor{}
	files := make([]usageLogExportArchiveFile, 0, 1)
	var totalRows int64
	partIndex := 1
	var workbook *usageLogExportWorkbook

	finalizeCurrent := func() error {
		if workbook == nil || workbook.rowIndex <= 7 {
			workbook = nil
			return nil
		}
		content, err := workbook.finish()
		if err != nil {
			return err
		}
		files = append(files, usageLogExportArchiveFile{
			name:    workbook.fileName(),
			content: content,
		})
		workbook = nil
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, totalRows, err
		}

		rows, err := fetchUsageLogExportRows(baseTx, cursor, usageLogExportQueryBatchSize)
		if err != nil {
			return nil, totalRows, err
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			if workbook == nil {
				workbook, err = newUsageLogExportWorkbook(
					payload,
					dayRange.dayStart,
					locLabel,
					partIndex,
				)
				if err != nil {
					return nil, totalRows, err
				}
			}
			if workbook.rowCount() >= usageLogExportMaxRowsPerFile {
				if err := finalizeCurrent(); err != nil {
					return nil, totalRows, err
				}
				partIndex++
				workbook, err = newUsageLogExportWorkbook(
					payload,
					dayRange.dayStart,
					locLabel,
					partIndex,
				)
				if err != nil {
					return nil, totalRows, err
				}
			}
			if err := workbook.addRow(row); err != nil {
				return nil, totalRows, err
			}
			totalRows++
			cursor = usageLogExportCursor{
				CreatedAt: row.CreatedAt,
				RequestID: row.RequestID,
				ID:        row.ID,
			}
		}

		if len(rows) < usageLogExportQueryBatchSize {
			break
		}
	}

	if err := finalizeCurrent(); err != nil {
		return nil, totalRows, err
	}
	return files, totalRows, nil
}

func buildUsageLogExportQuery(payload UsageLogExportTaskPayload, startTimestamp, endTimestamp int64) (*gorm.DB, error) {
	if model.LOG_DB == nil {
		return nil, fmt.Errorf("log database is not initialized")
	}

	groupCol := usageLogExportQualifiedLogGroupCol()
	selectColumns := strings.Join([]string{
		"logs.id",
		"logs.created_at",
		"logs.type",
		"logs.request_id",
		"logs.token_name",
		"logs.model_name",
		"logs.username",
		groupCol,
		"logs.prompt_tokens",
		"logs.completion_tokens",
		"logs.quota",
		"logs.other",
	}, ", ")
	tx := model.LOG_DB.Model(&model.Log{}).
		Select(selectColumns).
		Where("logs.type IN ?", model.NetQuotaSumTypes()).
		Where("logs.created_at >= ? AND logs.created_at < ?", startTimestamp, endTimestamp)

	var err error
	if tx, err = model.ApplyExplicitLogTextFilter(tx, "logs.model_name", payload.ModelName); err != nil {
		return nil, err
	}
	if tx, err = model.ApplyLogUsernameFilter(tx, "logs.username", payload.Username); err != nil {
		return nil, err
	}
	if payload.TokenName != "" {
		tx = tx.Where("logs.token_name = ?", payload.TokenName)
	}
	if payload.RequestID != "" {
		tx = tx.Where("logs.request_id = ?", payload.RequestID)
	}
	if payload.UpstreamRequestID != "" {
		tx = tx.Where("logs.upstream_request_id = ?", payload.UpstreamRequestID)
	}
	if payload.Channel != 0 {
		tx = tx.Where("logs.channel_id = ?", payload.Channel)
	}
	if payload.Group != "" {
		tx = tx.Where(groupCol+" = ?", payload.Group)
	}
	return tx, nil
}

func usageLogExportQualifiedLogGroupCol() string {
	groupCol := strings.TrimSpace(model.LogGroupCol())
	if groupCol == "" {
		if common.LogDatabaseType() == common.DatabaseTypePostgreSQL {
			groupCol = `"group"`
		} else {
			groupCol = "`group`"
		}
	}
	return "logs." + groupCol
}

func fetchUsageLogExportRows(tx *gorm.DB, cursor usageLogExportCursor, limit int) ([]usageLogExportRow, error) {
	if tx == nil {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 {
		limit = usageLogExportQueryBatchSize
	}

	query := tx.Order("logs.created_at asc, logs.request_id asc, logs.id asc").Limit(limit)
	if cursor.CreatedAt != 0 || cursor.RequestID != "" || cursor.ID != 0 {
		query = query.Where(
			"(logs.created_at > ?) OR (logs.created_at = ? AND logs.request_id > ?) OR (logs.created_at = ? AND logs.request_id = ? AND logs.id > ?)",
			cursor.CreatedAt,
			cursor.CreatedAt, cursor.RequestID,
			cursor.CreatedAt, cursor.RequestID, cursor.ID,
		)
	}

	var rows []usageLogExportRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func newUsageLogExportWorkbook(payload UsageLogExportTaskPayload, dayStart time.Time, locLabel string, partIndex int) (*usageLogExportWorkbook, error) {
	file := excelize.NewFile()
	sheetName := dayStart.Format("01-02")
	if partIndex > 1 {
		sheetName = fmt.Sprintf("%s_%02d", sheetName, partIndex)
	}
	defaultSheet := file.GetSheetName(0)
	if defaultSheet != sheetName {
		file.SetSheetName(defaultSheet, sheetName)
	}

	styles, err := newUsageLogExportStyles(file)
	if err != nil {
		return nil, err
	}

	title := fmt.Sprintf("%s %s | %s | %s", usageLogExportDisplayName(payload), usageLogExportSheetTitleLabel, dayStart.Format("2006-01-02"), locLabel)
	builder := &usageLogExportWorkbook{
		file:      file,
		sheetName: sheetName,
		title:     title,
		partName:  fmt.Sprintf("%s_%s", dayStart.Format("2006-01-02"), fmt.Sprintf("part%02d", partIndex)),
		rowIndex:  7,
		styles:    styles,
	}
	if err := builder.initSheet(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return builder, nil
}

func newUsageLogExportStyles(file *excelize.File) (usageLogExportStyles, error) {
	titleStyle, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 12},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return usageLogExportStyles{}, err
	}
	headerStyle, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#E8E8E8"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#BFBFBF", Style: 1},
			{Type: "right", Color: "#BFBFBF", Style: 1},
			{Type: "top", Color: "#BFBFBF", Style: 1},
			{Type: "bottom", Color: "#BFBFBF", Style: 1},
		},
	})
	if err != nil {
		return usageLogExportStyles{}, err
	}
	cellStyle, err := file.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#D9D9D9", Style: 1},
			{Type: "right", Color: "#D9D9D9", Style: 1},
			{Type: "top", Color: "#D9D9D9", Style: 1},
			{Type: "bottom", Color: "#D9D9D9", Style: 1},
		},
	})
	if err != nil {
		return usageLogExportStyles{}, err
	}
	summaryStyle, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#FFF4CE"}},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#D9D9D9", Style: 1},
			{Type: "right", Color: "#D9D9D9", Style: 1},
			{Type: "top", Color: "#D9D9D9", Style: 1},
			{Type: "bottom", Color: "#D9D9D9", Style: 1},
		},
	})
	if err != nil {
		return usageLogExportStyles{}, err
	}
	sectionTitleStyle, err := file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 12},
	})
	if err != nil {
		return usageLogExportStyles{}, err
	}
	return usageLogExportStyles{
		title:        titleStyle,
		header:       headerStyle,
		cell:         cellStyle,
		summary:      summaryStyle,
		sectionTitle: sectionTitleStyle,
	}, nil
}

func (b *usageLogExportWorkbook) initSheet() error {
	titleCell := "A1"
	if err := b.file.SetCellValue(b.sheetName, titleCell, b.title); err != nil {
		return err
	}
	if err := b.file.MergeCell(b.sheetName, "A1", "T1"); err != nil {
		return err
	}
	if err := b.file.SetCellStyle(b.sheetName, "A1", "T1", b.styles.title); err != nil {
		return err
	}

	summaryGroups := [][2]string{
		{"A2", "B2"},
		{"C2", "E2"},
		{"F2", "H2"},
		{"I2", "K2"},
		{"L2", "N2"},
		{"O2", "Q2"},
		{"R2", "T2"},
	}
	summaryTitles := []string{"请求数", "总 Token", "输入 Token", "输出 Token", "缓存读取 Token", "缓存写入 Token", "总费用 (USD)"}
	for i, group := range summaryGroups {
		if err := b.file.MergeCell(b.sheetName, group[0], group[1]); err != nil {
			return err
		}
		if err := b.file.SetCellValue(b.sheetName, group[0], summaryTitles[i]); err != nil {
			return err
		}
		if err := b.file.SetCellStyle(b.sheetName, group[0], group[1], b.styles.header); err != nil {
			return err
		}
	}

	summaryValueGroups := [][2]string{
		{"A3", "B3"},
		{"C3", "E3"},
		{"F3", "H3"},
		{"I3", "K3"},
		{"L3", "N3"},
		{"O3", "Q3"},
		{"R3", "T3"},
	}
	for _, group := range summaryValueGroups {
		if err := b.file.MergeCell(b.sheetName, group[0], group[1]); err != nil {
			return err
		}
		if err := b.file.SetCellStyle(b.sheetName, group[0], group[1], b.styles.summary); err != nil {
			return err
		}
	}

	groupRow := 5
	groupMergeRanges := []struct {
		start string
		end   string
		text  string
	}{
		{"A5", "G5", "请求与计费"},
		{"H5", "I5", "输入"},
		{"J5", "K5", "输出"},
		{"L5", "M5", "缓存读取"},
		{"N5", "O5", "缓存写入（通用）"},
		{"P5", "Q5", "缓存写入（5 分钟）"},
		{"R5", "S5", "缓存写入（1 小时）"},
		{"T5", "T5", "汇总"},
	}
	for _, item := range groupMergeRanges {
		if item.start != item.end {
			if err := b.file.MergeCell(b.sheetName, item.start, item.end); err != nil {
				return err
			}
		}
		if err := b.file.SetCellValue(b.sheetName, item.start, item.text); err != nil {
			return err
		}
		if err := b.file.SetCellStyle(b.sheetName, item.start, item.end, b.styles.header); err != nil {
			return err
		}
	}
	_ = groupRow

	headers := []string{
		"请求 ID",
		"类型",
		"令牌",
		"模型名称",
		"分组",
		"计费阶梯",
		"费用倍率",
		"输入 Token",
		"输入有效单价 (USD/百万 Token)",
		"输出 Token",
		"输出有效单价 (USD/百万 Token)",
		"缓存读取 Token",
		"缓存读取有效单价 (USD/百万 Token)",
		"缓存写入通用 Token",
		"缓存写入通用有效单价 (USD/百万 Token)",
		"缓存写入 5m Token",
		"缓存写入 5m 有效单价 (USD/百万 Token)",
		"缓存写入 1h Token",
		"缓存写入 1h 有效单价 (USD/百万 Token)",
		"总费用 (USD)",
	}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 6)
		if err := b.file.SetCellValue(b.sheetName, cell, header); err != nil {
			return err
		}
		if err := b.file.SetCellStyle(b.sheetName, cell, cell, b.styles.header); err != nil {
			return err
		}
	}

	widths := map[string]float64{
		"A": 34,
		"B": 10,
		"C": 16,
		"D": 18,
		"E": 16,
		"F": 14,
		"G": 10,
		"H": 14,
		"I": 16,
		"J": 14,
		"K": 16,
		"L": 14,
		"M": 16,
		"N": 16,
		"O": 18,
		"P": 16,
		"Q": 18,
		"R": 16,
		"S": 18,
		"T": 16,
	}
	for col, width := range widths {
		if err := b.file.SetColWidth(b.sheetName, col, col, width); err != nil {
			return err
		}
	}
	showGridLines := false
	topLeftCell := "E1"
	if err := b.file.SetSheetView(b.sheetName, 0, &excelize.ViewOptions{
		ShowGridLines: &showGridLines,
		TopLeftCell:   &topLeftCell,
	}); err != nil {
		return err
	}
	sheetIndex, err := b.file.GetSheetIndex(b.sheetName)
	if err != nil {
		return err
	}
	b.file.SetActiveSheet(sheetIndex)
	return nil
}

func (b *usageLogExportWorkbook) addRow(row usageLogExportRow) error {
	if b == nil || b.file == nil {
		return fmt.Errorf("usage log export workbook is not initialized")
	}
	exportOther := usageLogExportOther{}
	if row.Other != "" {
		_ = common.UnmarshalJsonStr(row.Other, &exportOther)
	}

	effectiveRatio := exportEffectiveRatio(exportOther)
	baseInputPrice := exportBaseInputPrice(exportOther)
	outputPrice := exportOutputPrice(exportOther, baseInputPrice)
	cacheReadPrice := exportCacheReadPrice(exportOther, baseInputPrice)
	cacheWritePrice := exportCacheWritePrice(exportOther, baseInputPrice)
	cacheWrite5mPrice := exportCacheWrite5mPrice(exportOther, baseInputPrice)
	cacheWrite1hPrice := exportCacheWrite1hPrice(exportOther, baseInputPrice)

	billTier := exportBillingTier(exportOther)
	promptTokens := exportPromptTokens(row, exportOther)
	totalTokens := promptTokens + int64(row.CompletionTokens)
	totalTokens += exportOther.CacheTokens
	totalTokens += exportOther.CacheCreationTokens
	totalTokens += exportOther.CacheCreationTokens5m
	totalTokens += exportOther.CacheCreationTokens1h
	displayType := "消耗"
	displayQuota := int64(row.Quota)
	if row.Type == model.LogTypeRefund {
		displayType = "退款"
		displayQuota = -displayQuota
	}

	values := []any{
		trimExportText(row.RequestID),
		displayType,
		trimExportText(row.TokenName),
		trimExportText(row.ModelName),
		trimExportText(firstNonEmpty(row.Group, row.Username)),
		billTier,
		formatExportFloat(effectiveRatio),
		promptTokens,
		priceOrBlank(baseInputPrice),
		row.CompletionTokens,
		priceOrBlank(outputPrice),
		exportOther.CacheTokens,
		priceOrBlank(cacheReadPrice),
		exportCacheWriteTotal(row, exportOther),
		priceOrBlank(cacheWritePrice),
		exportOther.CacheCreationTokens5m,
		priceOrBlank(cacheWrite5mPrice),
		exportOther.CacheCreationTokens1h,
		priceOrBlank(cacheWrite1hPrice),
		priceOrBlank(exportQuotaToUSD(displayQuota)),
	}

	for i, value := range values {
		cell, _ := excelize.CoordinatesToCellName(i+1, b.rowIndex)
		if err := b.file.SetCellValue(b.sheetName, cell, value); err != nil {
			return err
		}
		if err := b.file.SetCellStyle(b.sheetName, cell, cell, b.styles.cell); err != nil {
			return err
		}
	}

	b.totals.RequestCount++
	b.totals.PromptTokens += promptTokens
	b.totals.CompletionTokens += int64(row.CompletionTokens)
	b.totals.CacheReadTokens += exportOther.CacheTokens
	b.totals.CacheWriteTokens += exportCacheWriteTotal(row, exportOther)
	b.totals.CacheWrite5mTokens += exportOther.CacheCreationTokens5m
	b.totals.CacheWrite1hTokens += exportOther.CacheCreationTokens1h
	b.totals.TotalQuota += displayQuota
	b.totals.TotalTokens += totalTokens
	b.rowIndex++
	return nil
}

func (b *usageLogExportWorkbook) rowCount() int {
	if b == nil {
		return 0
	}
	return b.rowIndex - 7
}

func (b *usageLogExportWorkbook) finish() ([]byte, error) {
	if b == nil || b.file == nil {
		return nil, fmt.Errorf("usage log export workbook is not initialized")
	}
	_ = b.file.SetCellValue(b.sheetName, "A3", b.totals.RequestCount)
	_ = b.file.SetCellValue(b.sheetName, "C3", b.totals.TotalTokens)
	_ = b.file.SetCellValue(b.sheetName, "F3", b.totals.PromptTokens)
	_ = b.file.SetCellValue(b.sheetName, "I3", b.totals.CompletionTokens)
	_ = b.file.SetCellValue(b.sheetName, "L3", b.totals.CacheReadTokens)
	_ = b.file.SetCellValue(b.sheetName, "O3", b.totals.CacheWriteTokens)
	_ = b.file.SetCellValue(b.sheetName, "R3", priceOrBlank(exportQuotaToUSD(b.totals.TotalQuota)))

	lastRow := b.rowIndex - 1
	if lastRow >= 7 {
		tableRange := fmt.Sprintf("A6:T%d", lastRow)
		tableName := sanitizeUsageLogExportTableName(b.partName)
		showRowStripes := true
		if err := b.file.AddTable(b.sheetName, &excelize.Table{
			Range:             tableRange,
			Name:              tableName,
			StyleName:         "TableStyleMedium2",
			ShowFirstColumn:   false,
			ShowLastColumn:    false,
			ShowRowStripes:    &showRowStripes,
			ShowColumnStripes: false,
		}); err != nil {
			return nil, err
		}
	}

	buf, err := b.file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	_ = b.file.Close()
	return buf.Bytes(), nil
}

func (b *usageLogExportWorkbook) fileName() string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("%s.xlsx", sanitizeFileNamePart(b.partName))
}

func buildUsageLogExportWorkbookFile(
	payload UsageLogExportTaskPayload,
	dayStart time.Time,
	dayEnd time.Time,
	locLabel string,
	partIndex int,
	rows []usageLogExportRow,
) (usageLogExportArchiveFile, error) {
	workbook, err := newUsageLogExportWorkbook(payload, dayStart, locLabel, partIndex)
	if err != nil {
		return usageLogExportArchiveFile{}, err
	}
	for _, row := range rows {
		if err := workbook.addRow(row); err != nil {
			return usageLogExportArchiveFile{}, err
		}
	}
	content, err := workbook.finish()
	if err != nil {
		return usageLogExportArchiveFile{}, err
	}
	return usageLogExportArchiveFile{
		name:    workbook.fileName(),
		content: content,
	}, nil
}

func zipUsageLogExportFiles(files []usageLogExportArchiveFile, payload UsageLogExportTaskPayload, loc *time.Location) ([]byte, string, error) {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, file := range files {
		if file.name == "" || len(file.content) == 0 {
			continue
		}
		entry, err := zw.Create(file.name)
		if err != nil {
			_ = zw.Close()
			return nil, "", err
		}
		if _, err := entry.Write(file.content); err != nil {
			_ = zw.Close()
			return nil, "", err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}

	archiveName := fmt.Sprintf("%s.zip", buildUsageLogExportArchiveBaseName(payload, loc))
	return buf.Bytes(), archiveName, nil
}

func buildUsageLogExportArchiveBaseName(payload UsageLogExportTaskPayload, loc *time.Location) string {
	start := time.Unix(payload.StartTimestamp, 0).In(loc)
	end := time.Unix(payload.EndTimestamp, 0).In(loc)
	if end.After(start) {
		end = end.Add(-time.Second)
	}
	username := sanitizeFileNamePart(payload.Username)
	if username == "" {
		username = "all-users"
	}
	return fmt.Sprintf("%s_%s_to_%s_usage", username, start.Format("2006-01-02"), end.Format("2006-01-02"))
}

func buildUsageLogExportObjectKey(prefix string, taskID string, archiveName string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = usageLogExportDefaultPrefix
	}
	return path.Join(prefix, taskID, archiveName)
}

func getUsageLogExportUploader() (objectstore.Uploader, string, error) {
	settings := getImageResultObjectStoreConfig()
	provider := strings.ToLower(strings.TrimSpace(settings.Provider))
	if provider == "" {
		provider = "s3"
	}
	var (
		uploader objectstore.Uploader
		err      error
	)
	switch provider {
	case "", "s3", "aws_s3":
		uploader, err = getImageResultS3Uploader(settings)
	default:
		err = fmt.Errorf("unsupported usage log export object store provider: %s", provider)
	}
	if err != nil {
		return nil, "", err
	}
	prefix := strings.Trim(strings.TrimSpace(settings.Prefix), "/")
	if prefix != "" {
		prefix = path.Join(prefix, usageLogExportDefaultPrefix)
	} else {
		prefix = usageLogExportDefaultPrefix
	}
	return uploader, prefix, nil
}

func exportUsageLogExportStateProgress(processedDays, totalDays int) int {
	return logExportProgress(processedDays, totalDays)
}

func logExportProgress(processedDays, totalDays int) int {
	if totalDays <= 0 {
		return 100
	}
	progress := processedDays * 100 / totalDays
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func usageLogExportDisplayName(payload UsageLogExportTaskPayload) string {
	name := strings.TrimSpace(payload.Username)
	if name == "" {
		return "全部用户"
	}
	return name
}

func exportBillingTier(other usageLogExportOther) string {
	if other.BillingMode == "tiered_expr" {
		if strings.TrimSpace(other.MatchedTier) != "" {
			return other.MatchedTier
		}
		return "阶梯"
	}
	if other.ModelPrice != nil && *other.ModelPrice > 0 {
		return "按次"
	}
	return "普通"
}

func exportBaseInputPrice(other usageLogExportOther) float64 {
	if other.ModelRatio == nil {
		return 0
	}
	return *other.ModelRatio * 2.0
}

func exportOutputPrice(other usageLogExportOther, baseInputPrice float64) float64 {
	if other.CompletionRatio == nil {
		return 0
	}
	return baseInputPrice * *other.CompletionRatio
}

func exportCacheReadPrice(other usageLogExportOther, baseInputPrice float64) float64 {
	if other.CacheRatio == nil {
		return 0
	}
	return baseInputPrice * *other.CacheRatio
}

func exportCacheWritePrice(other usageLogExportOther, baseInputPrice float64) float64 {
	if other.CacheCreationRatio == nil {
		return 0
	}
	return baseInputPrice * *other.CacheCreationRatio
}

func exportCacheWrite5mPrice(other usageLogExportOther, baseInputPrice float64) float64 {
	if other.CacheCreationRatio5m == nil {
		return 0
	}
	return baseInputPrice * *other.CacheCreationRatio5m
}

func exportCacheWrite1hPrice(other usageLogExportOther, baseInputPrice float64) float64 {
	if other.CacheCreationRatio1h == nil {
		return 0
	}
	return baseInputPrice * *other.CacheCreationRatio1h
}

func exportEffectiveRatio(other usageLogExportOther) float64 {
	if other.UserGroupRatio != nil && *other.UserGroupRatio >= 0 {
		return *other.UserGroupRatio
	}
	if other.GroupRatio != nil && *other.GroupRatio >= 0 {
		return *other.GroupRatio
	}
	return 1
}

func exportPromptTokens(row usageLogExportRow, other usageLogExportOther) int64 {
	promptTokens := int64(row.PromptTokens)
	if other.InputTokensTotal > 0 {
		promptTokens = other.InputTokensTotal
	}
	if promptTokens < 0 {
		promptTokens = 0
	}
	if !exportShouldSubtractCacheTokens(other) || other.CacheTokens <= 0 {
		return promptTokens
	}
	promptTokens -= other.CacheTokens
	if promptTokens < 0 {
		return 0
	}
	return promptTokens
}

func exportShouldSubtractCacheTokens(other usageLogExportOther) bool {
	return !other.Claude
}

func exportCacheWriteTotal(row usageLogExportRow, other usageLogExportOther) int64 {
	total := other.CacheCreationTokens
	if total == 0 {
		total = int64(row.PromptTokens) - int64(row.PromptTokens)
	}
	total += other.CacheCreationTokens5m
	total += other.CacheCreationTokens1h
	return total
}

func exportQuotaToUSD(quota int64) float64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(quota) / common.QuotaPerUnit
}

func priceOrBlank(v float64) any {
	if v == 0 {
		return ""
	}
	return formatExportFloat(v)
}

func formatExportFloat(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	value := math.Round(v*1e6) / 1e6
	s := strconv.FormatFloat(value, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

func trimExportText(v string) string {
	return strings.TrimSpace(v)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sanitizeUsageLogExportTableName(name string) string {
	name = sanitizeFileNamePart(name)
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	var tableName strings.Builder
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			tableName.WriteByte(ch)
			continue
		}
		tableName.WriteByte('_')
	}
	name = tableName.String()
	if name == "" {
		name = "export"
	}
	name = "usage_" + name
	if len(name) > 240 {
		return name[:240]
	}
	return name
}

func sanitizeFileNamePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"@", "_",
		"#", "_",
		"$", "_",
		"%", "_",
		"&", "_",
		"+", "_",
		",", "_",
		";", "_",
		"=", "_",
		"(", "_",
		")", "_",
		"[", "_",
		"]", "_",
		"{", "_",
		"}", "_",
		" ", "_",
	)
	value = replacer.Replace(value)
	var b strings.Builder
	b.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '-' || r == '_':
			if !lastUnderscore {
				b.WriteRune(r)
				lastUnderscore = true
			}
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	value = b.String()
	value = strings.Trim(value, "._")
	value = strings.Trim(value, "_-")
	if len(value) > 120 {
		value = value[:120]
	}
	return value
}
