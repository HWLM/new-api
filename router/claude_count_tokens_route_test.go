package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSetRelayRouterRegistersClaudeCountTokensRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetRelayRouter(engine)

	found := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/v1/messages/count_tokens" {
			found = true
			assert.Contains(t, route.Handler, "ClaudeCountTokens")
			break
		}
	}
	assert.True(t, found)
}
