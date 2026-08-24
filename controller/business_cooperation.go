package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ListBusinessCooperations(c *gin.Context) {
	page := common.GetPageQuery(c)
	items, total, err := model.ListBusinessCooperations(c.GetInt("id"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	responses := make([]dto.BusinessCooperationResponse, 0, len(items))
	for _, item := range items {
		response, err := service.BusinessCooperationResponse(item)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		responses = append(responses, response)
	}
	page.SetTotal(int(total))
	page.SetItems(responses)
	common.ApiSuccess(c, page)
}

func GetBusinessCooperation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid application id")
		return
	}
	item, err := model.GetBusinessCooperation(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if item == nil || item.UserID != c.GetInt("id") {
		common.ApiErrorMsg(c, "application not found")
		return
	}
	response, err := service.BusinessCooperationResponse(item)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func ListBusinessCooperationBenchmarks(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid application id")
		return
	}
	application, err := model.GetBusinessCooperation(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if application == nil || application.UserID != c.GetInt("id") {
		common.ApiErrorMsg(c, "application not found")
		return
	}
	items, err := service.ListUpstreamBenchmarkResponses(application.UpstreamID, 100, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func CreateBusinessCooperation(c *gin.Context) {
	var input dto.BusinessCooperationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := service.CreateBusinessCooperation(c.GetInt("id"), input)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	response, err := service.BusinessCooperationResponse(item)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func DiscoverBusinessCooperationModels(c *gin.Context) {
	var input dto.UpstreamCandidateRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	models, err := service.DiscoverUpstreamModelsFromInput(c.Request.Context(), input)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	common.ApiSuccess(c, dto.UpstreamModelsResponse{Models: models})
}

func UpdateBusinessCooperation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid application id")
		return
	}
	var input dto.BusinessCooperationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := service.UpdateBusinessCooperation(c.GetInt("id"), id, input, false)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	response, err := service.BusinessCooperationResponse(item)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func ResubmitBusinessCooperation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid application id")
		return
	}
	var input dto.BusinessCooperationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	item, err := service.UpdateBusinessCooperation(c.GetInt("id"), id, input, true)
	if err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	response, err := service.BusinessCooperationResponse(item)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func DeleteBusinessCooperation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiErrorMsg(c, "invalid application id")
		return
	}
	if err := service.DeleteBusinessCooperation(c.GetInt("id"), id); err != nil {
		common.ApiError(c, service.SanitizeUpstreamError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
