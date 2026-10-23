package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QuerySummaryAll(hours, activeGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model: modelName,
		Group: c.Query("group"),
		Hours: hours,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result.Groups = filterGroupsByCurrentUser(c, result.Groups)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// filterGroupsByCurrentUser keeps only groups the current viewer is allowed to
// see. Root users see every active group; everyone else is scoped to their
// user-group visibility (same rules the pricing endpoint uses). Guests get the
// guest/public visibility set.
func filterGroupsByCurrentUser(c *gin.Context, groups []perfmetrics.GroupResult) []perfmetrics.GroupResult {
	userGroup := ""
	userRole := common.RoleGuestUser
	if userId, exists := c.Get("id"); exists {
		if user, err := model.GetUserCache(userId.(int)); err == nil {
			userGroup = user.Group
			userRole = user.Role
		}
	}

	if userRole >= common.RoleRootUser {
		activeRatios := ratio_setting.GetGroupRatioCopy()
		return lo.Filter(groups, func(g perfmetrics.GroupResult, _ int) bool {
			_, ok := activeRatios[g.Group]
			return ok || g.Group == "auto"
		})
	}

	usableGroups := service.GetUserUsableGroups(userGroup, userRole)
	return lo.Filter(groups, func(g perfmetrics.GroupResult, _ int) bool {
		if g.Group == "auto" {
			return true
		}
		_, ok := usableGroups[g.Group]
		return ok
	})
}
