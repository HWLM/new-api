package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenOrUserAuthWritesSessionRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("token-or-user-auth-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 1)
		session.Set("role", common.RoleAdminUser)
		session.Set("status", common.UserStatusEnabled)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/content", TokenOrUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.GetInt("id"), "role": c.GetInt("role")})
	})

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	require.Equal(t, http.StatusNoContent, loginRecorder.Code)

	request := httptest.NewRequest(http.MethodGet, "/content", nil)
	for _, sessionCookie := range loginRecorder.Result().Cookies() {
		request.AddCookie(sessionCookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"id":1,"role":10}`, recorder.Body.String())
}
