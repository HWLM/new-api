package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 蒸馏命中告警的用户侧部分：发给被判定用户本人的文案、三态开关与纯文本通道的
// 正文转换。这三处都会直接影响「用户看到什么」和「会不会被通知」，因此各自钉住
// 行为而不是实现细节。

func setupDistillNotifyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousRedis := model.DB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// :memory: 下每个连接是独立的库，锁到单连接才能读到刚写入的行。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	// 关掉 Redis，让 GetUserCache 回落数据库，测试不依赖外部缓存。
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		_ = sqlDB.Close()
	})
	return db
}

// TestIsDistillNotifyEffectiveForUserFollowsTriState 钉住三态语义：
// -1 永不通知（压过全局开）、0 跟随全局、1 强制通知（压过全局关）。
func TestIsDistillNotifyEffectiveForUserFollowsTriState(t *testing.T) {
	db := setupDistillNotifyTestDB(t)

	cases := []struct {
		name     string
		override int
		global   bool
		want     bool
	}{
		{name: "never notify beats enabled global", override: -1, global: true, want: false},
		{name: "never notify with disabled global", override: -1, global: false, want: false},
		{name: "follow global while enabled", override: 0, global: true, want: true},
		{name: "follow global while disabled", override: 0, global: false, want: false},
		{name: "force notify beats disabled global", override: 1, global: false, want: true},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := &model.User{
				Username: fmt.Sprintf("distill-notify-%d", i),
				// aff_code 带唯一索引，空串在 SQLite 下会被当成重复值。
				AffCode:               fmt.Sprintf("aff-distill-notify-%d", i),
				Password:              "unused-password-hash",
				Role:                  common.RoleCommonUser,
				Status:                common.UserStatusEnabled,
				Group:                 "default",
				DistillNotifyOverride: tc.override,
			}
			require.NoError(t, db.Create(user).Error)
			withDistillTestSetting(t, func(s *operation_setting.DistillDetectSetting) {
				s.NotifyUserEnabled = tc.global
			})

			assert.Equal(t, tc.want, IsDistillNotifyEffectiveForUser(user.Id))
		})
	}
}

// TestBuildDistillUserAlertTextKeepsOperatorDetailsOut 钉住「用户可见模板不得含运营
// 口径」这条红线：分数、阈值、渠道 id、兜底渠道 id 都不许出现在正文里。
func TestBuildDistillUserAlertTextKeepsOperatorDetailsOut(t *testing.T) {
	hit := &model.DistillDetectHit{
		RequestId:  "req-distill-alert-1",
		Model:      "claude-3-5-sonnet",
		Score:      0.9731,
		ChannelId:  4242,
		FallbackId: 7777,
		SampledAt:  time.Date(2026, 11, 26, 15, 4, 5, 0, time.UTC),
	}

	for _, observeOnly := range []bool{true, false} {
		for _, lang := range []string{"en", "zh", "zh-TW"} {
			name := fmt.Sprintf("%s/observe=%v", lang, observeOnly)
			t.Run(name, func(t *testing.T) {
				title, body := BuildDistillUserAlertText(hit, "alice", observeOnly, lang)

				require.NotEmpty(t, title)
				// 用户要能对上自己的那一笔请求。
				assert.Contains(t, body, "req-distill-alert-1")
				assert.Contains(t, body, "claude-3-5-sonnet")
				assert.Contains(t, body, "alice")
				// 运营口径一个都不能带。
				assert.NotContains(t, body, "0.97")
				assert.NotContains(t, body, "4242")
				assert.NotContains(t, body, "7777")
			})
		}
	}
}

// TestBuildDistillUserAlertTextSeparatesObserveAndReroute 钉住两种语气：观察期只提示
// 自查，重导模式才说明流量已被切换。措辞混用会打草惊蛇或造成虚假指控。
func TestBuildDistillUserAlertTextSeparatesObserveAndReroute(t *testing.T) {
	hit := &model.DistillDetectHit{
		RequestId: "req-distill-alert-2",
		SampledAt: time.Date(2026, 11, 26, 15, 4, 5, 0, time.UTC),
	}

	_, observeBody := BuildDistillUserAlertText(hit, "alice", true, "en")
	_, rerouteBody := BuildDistillUserAlertText(hit, "alice", false, "en")

	assert.Contains(t, observeBody, "look like batch training-data generation")
	assert.NotContains(t, observeBody, "has been temporarily routed")
	assert.Contains(t, rerouteBody, "has been temporarily routed")
	assert.NotContains(t, rerouteBody, "look like batch training-data generation")
}

// TestBuildDistillUserAlertTextLocalizesTitle 钉住语言选择：en / zh / zh-TW 各自命中，
// 未知语言（平台支持但没写模板的 fr/ja/ru/vi）回落简体中文。
func TestBuildDistillUserAlertTextLocalizesTitle(t *testing.T) {
	hit := &model.DistillDetectHit{
		RequestId: "req-distill-alert-3",
		SampledAt: time.Date(2026, 11, 26, 15, 4, 5, 0, time.UTC),
	}

	cases := []struct {
		lang      string
		wantTitle string
	}{
		{lang: "en", wantTitle: "Distillation Detection Notice"},
		{lang: "en-US", wantTitle: "Distillation Detection Notice"},
		{lang: "zh", wantTitle: "【蒸馏检测】提示"},
		{lang: "zh-TW", wantTitle: "【蒸餾檢測】提示"},
		{lang: "zh-Hant", wantTitle: "【蒸餾檢測】提示"},
		{lang: "ja", wantTitle: "【蒸馏检测】提示"},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			title, _ := BuildDistillUserAlertText(hit, "alice", true, tc.lang)
			assert.Equal(t, tc.wantTitle, title)
		})
	}
}

// TestDistillUserNotifyTargetMatchesNotifyUser 钉住「渠道是否配好」的判定。
//
// 这段判定必须与 NotifyUser 内部的跳过条件逐条一致：两边漂移就会出现
// 「记录成已发送、实际什么都没发」的假象，而审计字段正是运营判断的依据。
func TestDistillUserNotifyTargetMatchesNotifyUser(t *testing.T) {
	cases := []struct {
		name      string
		setting   dto.UserSetting
		email     string
		wantType  string
		wantReady bool
	}{
		{
			name:      "empty type falls back to email and uses the account email",
			email:     "alice@example.com",
			wantType:  dto.NotifyTypeEmail,
			wantReady: true,
		},
		{
			name:      "email without any address is not ready",
			setting:   dto.UserSetting{NotifyType: dto.NotifyTypeEmail},
			wantType:  dto.NotifyTypeEmail,
			wantReady: false,
		},
		{
			name:      "email prefers the notification address",
			setting:   dto.UserSetting{NotifyType: dto.NotifyTypeEmail, NotificationEmail: "notify@example.com"},
			wantType:  dto.NotifyTypeEmail,
			wantReady: true,
		},
		{
			name:      "webhook needs a url",
			setting:   dto.UserSetting{NotifyType: dto.NotifyTypeWebhook},
			wantType:  dto.NotifyTypeWebhook,
			wantReady: false,
		},
		{
			name:      "webhook with a url is ready",
			setting:   dto.UserSetting{NotifyType: dto.NotifyTypeWebhook, WebhookUrl: "https://example.com/hook"},
			wantType:  dto.NotifyTypeWebhook,
			wantReady: true,
		},
		{
			name:      "bark needs a url",
			setting:   dto.UserSetting{NotifyType: dto.NotifyTypeBark},
			wantType:  dto.NotifyTypeBark,
			wantReady: false,
		},
		{
			name:      "gotify needs both url and token",
			setting:   dto.UserSetting{NotifyType: dto.NotifyTypeGotify, GotifyUrl: "https://gotify.example.com"},
			wantType:  dto.NotifyTypeGotify,
			wantReady: false,
		},
		{
			name: "gotify with url and token is ready",
			setting: dto.UserSetting{
				NotifyType:  dto.NotifyTypeGotify,
				GotifyUrl:   "https://gotify.example.com",
				GotifyToken: "token",
			},
			wantType:  dto.NotifyTypeGotify,
			wantReady: true,
		},
		{
			name:      "feishu needs a webhook url",
			setting:   dto.UserSetting{NotifyType: dto.NotifyTypeFeishu},
			wantType:  dto.NotifyTypeFeishu,
			wantReady: false,
		},
		{
			name: "feishu with a webhook url is ready",
			setting: dto.UserSetting{
				NotifyType:       dto.NotifyTypeFeishu,
				FeishuWebhookUrl: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx",
			},
			wantType:  dto.NotifyTypeFeishu,
			wantReady: true,
		},
		{
			name: "dingtalk with a webhook url is ready",
			setting: dto.UserSetting{
				NotifyType:         dto.NotifyTypeDingTalk,
				DingTalkWebhookUrl: "https://oapi.dingtalk.com/robot/send?access_token=xxx",
			},
			wantType:  dto.NotifyTypeDingTalk,
			wantReady: true,
		},
		{
			name:      "unknown type is never ready",
			setting:   dto.UserSetting{NotifyType: "pigeon"},
			wantType:  "pigeon",
			wantReady: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotReady := distillUserNotifyTarget(tc.setting, tc.email)
			assert.Equal(t, tc.wantType, gotType, "channel must be reported even when not ready")
			assert.Equal(t, tc.wantReady, gotReady)
		})
	}
}

// TestNotifyPlainTextContentFlattensHTML 钉住飞书 / 钉钉的正文转换：历史模板里给
// Email / Webhook 拼的 <br/>、<a href> 必须变成纯文本，纯文本正文保持原样。
func TestNotifyPlainTextContentFlattensHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "quota warning template keeps its two lines",
			in:   "您的额度即将用尽，当前剩余额度为 $1，请及时充值。<br/>充值链接：<a href='https://example.com/wallet'>https://example.com/wallet</a>",
			want: "您的额度即将用尽，当前剩余额度为 $1，请及时充值。\n充值链接：https://example.com/wallet",
		},
		{
			name: "br without slash still breaks the line",
			in:   "第一行<br>第二行",
			want: "第一行\n第二行",
		},
		{
			name: "plain text passes through unchanged",
			in:   "账号：alice\n请求编号：req-1\n请自查使用方式。",
			want: "账号：alice\n请求编号：req-1\n请自查使用方式。",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, notifyPlainTextContent(tc.in))
		})
	}
}
