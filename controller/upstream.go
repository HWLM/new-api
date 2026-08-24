package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ListUpstreams(c *gin.Context) {
	page := common.GetPageQuery(c)
	items, total, err := model.ListUpstreamCandidates(page.GetStartIdx(), page.GetPageSize(), c.Query("keyword"), c.Query("source"), c.Query("benchmark_status"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]dto.UpstreamCandidateResponse, 0, len(items))
	for _, item := range items {
		result = append(result, service.CandidateResponse(item))
	}
	page.SetTotal(int(total))
	page.SetItems(result)
	common.ApiSuccess(c, page)
}

func GetUpstream(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	item, err := model.GetUpstreamCandidate(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if item == nil {
		common.ApiErrorMsg(c, "upstream not found")
		return
	}
	common.ApiSuccess(c, service.CandidateResponse(item))
}

func ListUpstreamBenchmarks(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	if item, err := model.GetUpstreamCandidate(id); err != nil {
		common.ApiError(c, err)
		return
	} else if item == nil {
		common.ApiErrorMsg(c, "upstream not found")
		return
	}
	items, err := service.ListUpstreamBenchmarkResponses(id, 100, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func ListUpstreamModels(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	candidate, err := model.GetUpstreamCandidate(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	models, err := service.DiscoverUpstreamModels(c.Request.Context(), candidate)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	common.ApiSuccess(c, dto.UpstreamModelsResponse{Models: models})
}

func GetUpstreamBenchmarkProfile(c *gin.Context) {
	profile, err := service.GetUpstreamBenchmarkProfileResponse()
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	common.ApiSuccess(c, profile)
}

func UpdateUpstreamBenchmarkProfile(c *gin.Context) {
	var input dto.UpstreamBenchmarkProfileRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	profile, err := service.SaveUpstreamBenchmarkProfile(input)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	recordManageAudit(c, "upstream.profile_update", map[string]interface{}{"profile": profile.Name, "version": profile.Version})
	common.ApiSuccess(c, profile)
}

func GetUpstreamAutoSync(c *gin.Context) {
	common.ApiSuccess(c, service.GetUpstreamAutoSyncConfig())
}

func UpdateUpstreamAutoSync(c *gin.Context) {
	var input dto.UpstreamAutoSyncConfig
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	config, err := service.UpdateUpstreamAutoSyncConfig(input)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	recordManageAudit(c, "upstream.auto_sync_update", map[string]interface{}{"enabled": config.Enabled, "min_score": config.MinScore})
	common.ApiSuccess(c, config)
}

func CreateUpstream(c *gin.Context) {
	var input dto.UpstreamCandidateRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := service.CreateUpstreamCandidate(input, "admin", nil, nil)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	recordManageAudit(c, "upstream.create", map[string]interface{}{"upstream_id": item.ID, "type": item.Type})
	common.ApiSuccess(c, service.CandidateResponse(item))
}

func UpdateUpstream(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	var input dto.UpstreamCandidateRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := service.UpdateUpstreamCandidate(id, input)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	recordManageAudit(c, "upstream.update", map[string]interface{}{"upstream_id": id, "type": input.Type})
	common.ApiSuccess(c, service.CandidateResponse(item))
}

func StartUpstreamBenchmark(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	var input dto.UpstreamBenchmarkRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&input); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	task, err := service.EnqueueUpstreamBenchmarkWithOptions(id, input.Model, input.Concurrency)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	recordManageAudit(c, "upstream.benchmark", map[string]interface{}{"upstream_id": id, "model": input.Model, "concurrency": input.Concurrency})
	common.ApiSuccess(c, gin.H{"task_id": task.TaskID, "status": task.Status})
}

func CancelUpstreamBenchmark(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	if err := service.CancelUpstreamBenchmark(id); err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	recordManageAudit(c, "upstream.benchmark_cancel", map[string]interface{}{"upstream_id": id})
	common.ApiSuccess(c, nil)
}

func SyncUpstream(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	channel, err := service.SyncUpstreamCandidate(id)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	recordManageAudit(c, "upstream.sync", map[string]interface{}{"upstream_id": id, "channel_id": channel.Id})
	common.ApiSuccess(c, gin.H{"channel_id": channel.Id})
}

func SyncUpstreams(c *gin.Context) {
	var input dto.UpstreamSyncRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	channels, err := service.SyncUpstreamCandidates(input.IDs, input.Tag)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	channelIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.Id)
	}
	recordManageAudit(c, "upstream.sync_batch", map[string]interface{}{"count": len(channelIDs)})
	common.ApiSuccess(c, gin.H{"channel_ids": channelIDs})
}

func RejectUpstream(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid upstream id")
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.RejectUpstreamCandidate(id, strings.TrimSpace(input.Reason)); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "upstream.reject", map[string]interface{}{"upstream_id": id})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
