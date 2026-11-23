package controller

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 蒸馏设置页面回显：jev_model 已配时必须原样返回。
//
// 缺陷形态（fix 之前）：DistillSettingView 声明了 JevModel，但构造视图时
// 只赋了 JevBaseURL，GET 恒为空串——保存成功、运营却以为没保存。

func setupDistillViewTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	model.DB = db

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		model.DB = previousDB
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})
}

func readDistillSettingView(t *testing.T) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	GetDistillDetectSetting(c)
	require.Equal(t, 200, w.Code)

	var body struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success)
	return body.Data
}

func TestDistillSettingViewEchoesJevModel(t *testing.T) {
	s := operation_setting.GetDistillDetectSetting()
	previous := s.JevModel
	t.Cleanup(func() { s.JevModel = previous })

	s.JevModel = "jev-preview"

	data := readDistillSettingView(t)
	require.Equal(t, "jev-preview", data["jev_model"])
}

// 保存后从库里灌回内存，GET 仍能读到——模拟重启/多实例同步后的页面。
func TestDistillSettingJevModelSurvivesSaveAndReload(t *testing.T) {
	setupDistillViewTestDB(t)
	s := operation_setting.GetDistillDetectSetting()
	previous := s.JevModel
	t.Cleanup(func() { s.JevModel = previous })

	s.JevModel = "jev-stub"
	require.NoError(t, persistDistillSetting())

	var option model.Option
	require.NoError(t, model.DB.Where("key = ?", "distill_detect_setting.jev_model").First(&option).Error)
	require.Equal(t, "jev-stub", option.Value)

	// 模拟进程重启后 loadOptionsFromDatabase 的同一条路径：库值经配置系统写回注册对象。
	cfg := config.GlobalConfig.Get("distill_detect_setting")
	require.NotNil(t, cfg)
	require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{"jev_model": option.Value}))

	data := readDistillSettingView(t)
	require.Equal(t, "jev-stub", data["jev_model"])
}
