/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
package controller

// 「当日统计」导出 Excel —— admin only。
//
//   GET /api/user_stats/details_singleday/export?date=YYYY-MM-DD&<同筛选>
//
// 输出结构（单 sheet）：
//
//	A. 明细表：
//	   表头：日期 / 商务渠道 / 客户ID / 客户名称 / 客户类型 / 当日消耗($) / 账户余额($)
//	   按 business_channel 分组：有渠道在前（每组末尾"汇总"行），无渠道排最后
//	   组内排序：客户类型 ASC, 当日消耗 DESC, id ASC
//	B. 数据汇总：客户数 / 总消耗 / 按 group 分组的"客户数 + 消耗 + 余额"
//	C. 渠道明细：按每个渠道（含"无渠道"）重复 B 的内容

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// ExportUserStatsDetailsSingleDay 导出当日统计 Excel。
func ExportUserStatsDetailsSingleDay(c *gin.Context) {
	f, err := parseDetailsSingleDayFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	rows, err := loadSingleDayAllRows(f)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	file := excelize.NewFile()
	defer file.Close()
	sheet := "Sheet1"

	// 样式：表头、汇总行、标题
	headerStyle, _ := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#E8E8E8"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#BFBFBF", Style: 1},
			{Type: "right", Color: "#BFBFBF", Style: 1},
			{Type: "top", Color: "#BFBFBF", Style: 1},
			{Type: "bottom", Color: "#BFBFBF", Style: 1},
		},
	})
	cellStyle, _ := file.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{Type: "left", Color: "#BFBFBF", Style: 1},
			{Type: "right", Color: "#BFBFBF", Style: 1},
			{Type: "top", Color: "#BFBFBF", Style: 1},
			{Type: "bottom", Color: "#BFBFBF", Style: 1},
		},
	})
	summaryStyle, _ := file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#FFF4CE"}},
		Border: []excelize.Border{
			{Type: "left", Color: "#BFBFBF", Style: 1},
			{Type: "right", Color: "#BFBFBF", Style: 1},
			{Type: "top", Color: "#BFBFBF", Style: 1},
			{Type: "bottom", Color: "#BFBFBF", Style: 1},
		},
	})
	sectionTitleStyle, _ := file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 12},
	})

	// 表头
	headers := []string{"日期", "商务渠道", "客户ID", "客户名称", "客户类型", "当日消耗($)", "账户余额($)"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = file.SetCellValue(sheet, cell, h)
	}
	_ = file.SetCellStyle(sheet, "A1", "G1", headerStyle)

	// 按 business_channel 分组：有渠道在前（按渠道名升序），无渠道排最后
	groups := groupRowsByChannel(rows)
	row := 2
	for _, g := range groups {
		// 组内：客户类型 ASC, 当日消耗 DESC, id ASC
		sort.SliceStable(g.rows, func(i, j int) bool {
			ai, aj := g.rows[i], g.rows[j]
			if ai.UserGroup != aj.UserGroup {
				return ai.UserGroup < aj.UserGroup
			}
			if ai.DailyConsumedUsd != aj.DailyConsumedUsd {
				return ai.DailyConsumedUsd > aj.DailyConsumedUsd
			}
			return ai.UserId < aj.UserId
		})
		var sumConsumed, sumRemaining float64
		for _, r := range g.rows {
			remaining := 0.0
			if r.RemainingUsd != nil {
				remaining = *r.RemainingUsd
			}
			vals := []any{
				r.Date,
				g.label,
				r.UserId,
				orDash(r.Username),
				orDash(r.UserGroup),
				roundFloat(r.DailyConsumedUsd),
				roundFloat(remaining),
			}
			writeRow(file, sheet, row, vals)
			_ = file.SetCellStyle(sheet,
				mustCell(1, row), mustCell(len(headers), row), cellStyle)
			sumConsumed += r.DailyConsumedUsd
			sumRemaining += remaining
			row++
		}
		// 汇总行
		writeRow(file, sheet, row, []any{"汇总", "", "", "", "",
			roundFloat(sumConsumed), roundFloat(sumRemaining)})
		_ = file.SetCellStyle(sheet,
			mustCell(1, row), mustCell(len(headers), row), summaryStyle)
		row++
	}

	// 空行
	row++

	// ===== 数据汇总块 =====
	allStat := computeChannelStat(rows)

	_ = file.SetCellValue(sheet, mustCell(1, row), fmt.Sprintf("%s数据汇总", f.date))
	_ = file.SetCellStyle(sheet, mustCell(1, row), mustCell(1, row), sectionTitleStyle)
	row++
	_ = file.SetCellValue(sheet, mustCell(1, row), fmt.Sprintf("客户数：%d", allStat.totalCustomers))
	row++
	_ = file.SetCellValue(sheet, mustCell(1, row),
		fmt.Sprintf("总消耗：$%s", formatMoney(allStat.totalConsumed)))
	row++
	for _, groupName := range allStat.groupKeys {
		gs := allStat.byGroup[groupName]
		_ = file.SetCellValue(sheet, mustCell(1, row),
			fmt.Sprintf("%s：%d 个，消耗 $%s，剩余余额 $%s",
				groupName, gs.count, formatMoney(gs.consumed), formatMoney(gs.remaining)))
		row++
	}

	// 空行
	row++

	// ===== 渠道明细块 =====
	_ = file.SetCellValue(sheet, mustCell(1, row), fmt.Sprintf("%s渠道明细", f.date))
	_ = file.SetCellStyle(sheet, mustCell(1, row), mustCell(1, row), sectionTitleStyle)
	row++

	for _, g := range groups {
		st := computeChannelStat(g.rows)
		_ = file.SetCellValue(sheet, mustCell(1, row), g.label)
		_ = file.SetCellStyle(sheet, mustCell(1, row), mustCell(1, row), sectionTitleStyle)
		row++
		_ = file.SetCellValue(sheet, mustCell(1, row), fmt.Sprintf("客户数：%d", st.totalCustomers))
		row++
		_ = file.SetCellValue(sheet, mustCell(1, row),
			fmt.Sprintf("总消耗：$%s", formatMoney(st.totalConsumed)))
		row++
		for _, groupName := range st.groupKeys {
			gs := st.byGroup[groupName]
			_ = file.SetCellValue(sheet, mustCell(1, row),
				fmt.Sprintf("%s：%d 个，消耗 $%s，剩余余额 $%s",
					groupName, gs.count, formatMoney(gs.consumed), formatMoney(gs.remaining)))
			row++
		}
		row++
	}

	// 列宽
	_ = file.SetColWidth(sheet, "A", "A", 12)
	_ = file.SetColWidth(sheet, "B", "B", 18)
	_ = file.SetColWidth(sheet, "C", "C", 14)
	_ = file.SetColWidth(sheet, "D", "D", 22)
	_ = file.SetColWidth(sheet, "E", "E", 14)
	_ = file.SetColWidth(sheet, "F", "G", 14)

	filename := fmt.Sprintf("当日统计_%s.xlsx", f.date)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	// RFC 5987：filename* 保证非 ASCII 文件名在所有浏览器下正确显示
	c.Header("Content-Disposition", fmt.Sprintf(
		`attachment; filename="export_%s.xlsx"; filename*=UTF-8''%s`,
		f.date, url.PathEscape(filename),
	))
	if err := file.Write(c.Writer); err != nil {
		common.SysError("export singleday excel write failed: " + err.Error())
	}
}

// channelGroup 一个渠道下的行集合
type channelGroup struct {
	channel string // 原始 business_channel 值（""=无渠道）
	label   string // Excel 显示名（无渠道→"无渠道"）
	rows    []detailsDailyRow
}

// groupRowsByChannel 按 business_channel 分组：有渠道在前（按渠道名升序），无渠道排最后。
func groupRowsByChannel(rows []detailsDailyRow) []channelGroup {
	bucket := map[string][]detailsDailyRow{}
	for _, r := range rows {
		bucket[r.BusinessChannel] = append(bucket[r.BusinessChannel], r)
	}
	keys := make([]string, 0, len(bucket))
	for k := range bucket {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]channelGroup, 0, len(bucket))
	for _, k := range keys {
		out = append(out, channelGroup{channel: k, label: k, rows: bucket[k]})
	}
	if rs, ok := bucket[""]; ok {
		out = append(out, channelGroup{channel: "", label: "无渠道", rows: rs})
	}
	return out
}

// channelStat 汇总块中的统计：总客户数、总消耗，以及按 group 分组的明细
type channelStat struct {
	totalCustomers int
	totalConsumed  float64
	groupKeys      []string // 按字母排序的 group 名
	byGroup        map[string]groupStat
}

type groupStat struct {
	count     int
	consumed  float64
	remaining float64
}

func computeChannelStat(rows []detailsDailyRow) channelStat {
	st := channelStat{byGroup: map[string]groupStat{}}
	st.totalCustomers = len(rows)
	seenGroups := map[string]struct{}{}
	for _, r := range rows {
		st.totalConsumed += r.DailyConsumedUsd
		remaining := 0.0
		if r.RemainingUsd != nil {
			remaining = *r.RemainingUsd
		}
		key := r.UserGroup
		if key == "" {
			key = "(空分组)"
		}
		gs := st.byGroup[key]
		gs.count++
		gs.consumed += r.DailyConsumedUsd
		gs.remaining += remaining
		st.byGroup[key] = gs
		seenGroups[key] = struct{}{}
	}
	for k := range seenGroups {
		st.groupKeys = append(st.groupKeys, k)
	}
	sort.Strings(st.groupKeys)
	return st
}

// ------- 小工具 -------

func mustCell(col, row int) string {
	cell, _ := excelize.CoordinatesToCellName(col, row)
	return cell
}

func writeRow(file *excelize.File, sheet string, row int, vals []any) {
	for i, v := range vals {
		_ = file.SetCellValue(sheet, mustCell(i+1, row), v)
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// roundFloat 保留 2 位小数返回（Excel 会按数字存储，方便后续做合计）
func roundFloat(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func formatMoney(v float64) string {
	return fmt.Sprintf("%.2f", v)
}
