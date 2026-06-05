package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// OpenAPIAuth 基于系统设置 OpenAPIToken 的静态 Token 鉴权（管理后台可编辑）。
// 若后台未配置（值为空），回落到环境变量 OPENAPI_TOKEN 作为初始化兜底；
// 两者都为空则拒绝所有请求（fail-closed）。
//
// 请求侧支持两种 Header：
//   - X-OpenAPI-Token: <token>          （优先）
//   - Authorization: Bearer <token>
//
// 使用 crypto/subtle.ConstantTimeCompare 做恒定时间比较，避免 timing 攻击。
func OpenAPIAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := strings.TrimSpace(system_setting.GetOpenAPIToken())
		if expected == "" {
			expected = strings.TrimSpace(common.GetEnvOrDefaultString("OPENAPI_TOKEN", ""))
		}
		if expected == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "OpenAPI 未启用：请在系统设置中配置 OpenAPIToken",
			})
			c.Abort()
			return
		}

		provided := strings.TrimSpace(c.GetHeader("X-OpenAPI-Token"))
		if provided == "" {
			auth := c.GetHeader("Authorization")
			if strings.HasPrefix(auth, "Bearer ") || strings.HasPrefix(auth, "bearer ") {
				provided = strings.TrimSpace(auth[7:])
			}
		}

		if provided == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "缺少 OpenAPI Token",
			})
			c.Abort()
			return
		}

		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "OpenAPI Token 校验失败",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
