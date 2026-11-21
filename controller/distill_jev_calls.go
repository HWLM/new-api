package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ListDistillJevCalls 是 Jev 判定服务调用记录的只读查询。
//
// 用途：运营要看「判定服务到底调了多少次、判了谁的请求、当时那个用户刷得多猛
// （rpm）、成功还是失败、分数多少」——命中记录只覆盖「判中了」的那几条，
// 未命中与失败的调用只有这张表能看到。
//
// 权限与命中记录一致：管理员看全部并可按 user_id 过滤；普通用户强制只看自己，
// 传别人的 user_id 会被直接覆盖（不报错，不告诉对方这个参数是否有效）。
// 普通用户看不到 channel_id（内部拓扑）。
type DistillJevCallView struct {
	Id         int64    `json:"id"`
	UserId     int      `json:"user_id"`
	Username   string   `json:"username,omitempty"`
	RequestId  string   `json:"request_id"`
	Model      string   `json:"model"`
	ChannelId  int      `json:"channel_id"`
	Rpm        int      `json:"rpm"`
	Outcome    string   `json:"outcome"`
	HttpStatus int      `json:"http_status"`
	Score      *float64 `json:"score"`
	Threshold  float64  `json:"threshold"`
	DurationMs int64    `json:"duration_ms"`
	Error      string   `json:"error"`
	CreatedAt  string   `json:"created_at"`
}

// DistillJevCallUserView 是普通用户看到的行：去掉 channel_id 与错误细节。
type DistillJevCallUserView struct {
	Id         int64    `json:"id"`
	RequestId  string   `json:"request_id"`
	Model      string   `json:"model"`
	Rpm        int      `json:"rpm"`
	Outcome    string   `json:"outcome"`
	Score      *float64 `json:"score"`
	Threshold  float64  `json:"threshold"`
	DurationMs int64    `json:"duration_ms"`
	CreatedAt  string   `json:"created_at"`
}

// DistillJevCallSummary 是顶部汇总卡片；口径与列表相同的筛选条件。
type DistillJevCallSummary struct {
	Total         int64    `json:"total"`
	Success       int64    `json:"success"`
	Failed        int64    `json:"failed"`
	AvgScore      *float64 `json:"avg_score"`
	AvgDurationMs *float64 `json:"avg_duration_ms"`
}

func ListDistillJevCalls(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	role := c.GetInt("role")
	isAdmin := role >= common.RoleAdminUser
	scopeUserID, err := distillHitUserScope(role, c.GetInt("id"), c.Query("user_id"))
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if !isAdmin && scopeUserID <= 0 {
		// 正常不会走到：UserAuth 一定带上 id。宁可拒绝，也不要退化成「不加过滤」。
		common.ApiErrorMsg(c, "未登录")
		return
	}

	// 每次 finisher（Count / Scan / Find）都从 Session 重新起飞：GORM 复用同一个
	// Statement 做多次终结会把上一次的 SELECT/ORDER 带进下一次，这里显式隔离。
	base := func() *gorm.DB {
		q := model.DB.Session(&gorm.Session{}).Model(&model.DistillJevCall{})
		if scopeUserID > 0 {
			q = q.Where("user_id = ?", scopeUserID)
		}
		if outcome := strings.TrimSpace(c.Query("outcome")); outcome != "" {
			q = q.Where("outcome = ?", outcome)
		}
		if from := strings.TrimSpace(c.Query("from")); from != "" {
			if t, err := time.Parse(time.RFC3339, from); err == nil {
				q = q.Where("created_at >= ?", t)
			}
		}
		if to := strings.TrimSpace(c.Query("to")); to != "" {
			if t, err := time.Parse(time.RFC3339, to); err == nil {
				q = q.Where("created_at <= ?", t)
			}
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	// 汇总与列表同一套筛选，不受分页影响。
	var summary DistillJevCallSummary
	if err := base().Select(
		"COALESCE(SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END), 0) AS success, " +
			"COALESCE(SUM(CASE WHEN outcome <> 'success' THEN 1 ELSE 0 END), 0) AS failed, " +
			"AVG(score) AS avg_score, AVG(duration_ms) AS avg_duration_ms",
	).Scan(&summary).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	summary.Total = total

	var rows []model.DistillJevCall
	if err := base().Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	items := make([]any, 0, len(rows))
	for _, r := range rows {
		created := r.CreatedAt.Format(time.RFC3339)
		if !isAdmin {
			items = append(items, DistillJevCallUserView{
				Id:         r.Id,
				RequestId:  r.RequestId,
				Model:      r.Model,
				Rpm:        r.Rpm,
				Outcome:    r.Outcome,
				Score:      r.Score,
				Threshold:  r.Threshold,
				DurationMs: r.DurationMs,
				CreatedAt:  created,
			})
			continue
		}
		view := DistillJevCallView{
			Id:         r.Id,
			UserId:     r.UserId,
			RequestId:  r.RequestId,
			Model:      r.Model,
			ChannelId:  r.ChannelId,
			Rpm:        r.Rpm,
			Outcome:    r.Outcome,
			HttpStatus: r.HttpStatus,
			Score:      r.Score,
			Threshold:  r.Threshold,
			DurationMs: r.DurationMs,
			Error:      r.Error,
			CreatedAt:  created,
		}
		if cache, err := model.GetUserCache(r.UserId); err == nil && cache != nil {
			view.Username = cache.Username
		}
		items = append(items, view)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":   items,
			"total":   total,
			"page":    page,
			"summary": summary,
		},
	})
}
