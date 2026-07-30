package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoProxyTaskAccessByRole(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, db.Create(&model.Task{
		TaskID: "task_other_user",
		UserId: 6,
		Status: model.TaskStatusInProgress,
	}).Error)

	tests := []struct {
		name       string
		role       int
		wantStatus int
		wantBody   string
	}{
		{
			name:       "common user cannot access another user's task",
			role:       common.RoleCommonUser,
			wantStatus: http.StatusNotFound,
			wantBody:   "Task not found",
		},
		{
			name:       "admin can access another user's task",
			role:       common.RoleAdminUser,
			wantStatus: http.StatusBadRequest,
			wantBody:   "Task is not completed yet",
		},
		{
			name:       "root can access another user's task",
			role:       common.RoleRootUser,
			wantStatus: http.StatusBadRequest,
			wantBody:   "Task is not completed yet",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_other_user/content", nil)
			ctx.Params = gin.Params{{Key: "task_id", Value: "task_other_user"}}
			ctx.Set("id", 1)
			ctx.Set("role", test.role)

			VideoProxy(ctx)

			require.Equal(t, test.wantStatus, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.wantBody)
		})
	}
}
