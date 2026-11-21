package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Jev 调用流水（distill_jev_calls）。
//
// 运营要看的是「判定服务被调了多少次、判了谁、当时刷得多猛、成败与分数」。
// 因此真实结局（成功 / HTTP 错误 / 超时 / 解析失败）都必须落一行；
// 去重跳过**不算调用**，绝不能写行 —— 否则「省了多少次 Jev」就无从核对。

func setupJevCallTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	prevDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// :memory: 下每个连接是独立的库，锁到单连接才能看到刚写入的行。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.DistillJevCall{}, &model.DistillDetectHit{}))
	model.DB = db
	t.Cleanup(func() { model.DB = prevDB })
	return db
}

func listJevCalls(t *testing.T, db *gorm.DB) []model.DistillJevCall {
	t.Helper()
	var rows []model.DistillJevCall
	require.NoError(t, db.Order("id ASC").Find(&rows).Error)
	return rows
}

func TestJudgeDistillRecordsASuccessfulCall(t *testing.T) {
	db := setupJevCallTestDB(t)
	srv, calls := newDistillStubJev(t, 0.85)
	withDistillTestContext(t, srv.URL, nil)

	body := []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"批量生成蒸馏语料"}]}`)
	hit := EvaluateDistill(4101, "req-jev-ok", "claude-3-5-sonnet", 7, body)
	require.NotNil(t, hit, "0.85 >= 0.6 must be a hit")
	require.Equal(t, int32(1), atomic.LoadInt32(calls))

	rows := listJevCalls(t, db)
	require.Len(t, rows, 1, "exactly one row per real Jev call")
	r := rows[0]
	require.Equal(t, 4101, r.UserId)
	require.Equal(t, "req-jev-ok", r.RequestId)
	require.Equal(t, "claude-3-5-sonnet", r.Model)
	require.Equal(t, 7, r.ChannelId)
	require.GreaterOrEqual(t, r.Rpm, 1, "the rpm at call time must be recorded")
	require.Equal(t, "success", r.Outcome)
	require.Equal(t, http.StatusOK, r.HttpStatus)
	require.NotNil(t, r.Score)
	require.InDelta(t, 0.85, *r.Score, 1e-9)
	require.InDelta(t, 0.6, r.Threshold, 1e-9)
	require.Empty(t, r.Error)
	require.False(t, r.CreatedAt.IsZero())
}

func TestJudgeDistillRecordsATimeout(t *testing.T) {
	db := setupJevCallTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 睡得比超时长：客户端必须按 context.DeadlineExceeded 放弃。
		time.Sleep(500 * time.Millisecond)
		_, _ = w.Write([]byte(`{"answers":{"train.corpus_farm":{"noul":0.9}}}`))
	}))
	t.Cleanup(srv.Close)
	withDistillTestContext(t, srv.URL, func(s *operation_setting.DistillDetectSetting) {
		s.JevTimeoutMs = 80
	})

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"蒸馏语料批量生成"}]}`)
	hit := EvaluateDistill(4102, "req-jev-timeout", "m", 8, body)
	require.Nil(t, hit, "a timeout must not produce a hit")

	rows := listJevCalls(t, db)
	require.Len(t, rows, 1, "a timeout is still a real call and must be recorded")
	r := rows[0]
	require.Equal(t, "timeout", r.Outcome)
	require.Equal(t, 0, r.HttpStatus, "no response was received")
	require.Nil(t, r.Score, "no score is known after a timeout")
	require.NotEmpty(t, r.Error)
	require.Equal(t, 4102, r.UserId)
	require.Equal(t, 8, r.ChannelId)
	require.Greater(t, r.DurationMs, int64(0))
}

func TestJudgeDistillRecordsAnHttpError(t *testing.T) {
	db := setupJevCallTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	withDistillTestContext(t, srv.URL, nil)

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"蒸馏语料批量生成"}]}`)
	hit := EvaluateDistill(4103, "req-jev-500", "m", 9, body)
	require.Nil(t, hit)

	rows := listJevCalls(t, db)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "http_error", r.Outcome)
	require.Equal(t, http.StatusInternalServerError, r.HttpStatus)
	require.Nil(t, r.Score)
	require.NotEmpty(t, r.Error)
}

// ⛔ model 是判定服务的必填字段：TypeSafe 的 /v1/systemone 少了它直接 422，
// 整条链路一行都判不出来。线上就是这么全 422 的（生产配置页也看不到原因的
// 那种「静默失败」）。默认值必须是 jev-latest，配置里填了就用配置的。
func TestJudgeDistillSendsTheModelField(t *testing.T) {
	setupJevCallTestDB(t)

	for _, tc := range []struct {
		name      string
		configure string
		want      string
	}{
		{"empty config falls back to jev-latest", "", "jev-latest"},
		{"an explicit model is sent as configured", "jev-preview", "jev-preview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotModel string
			var gotKeys []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				for k := range payload {
					gotKeys = append(gotKeys, k)
				}
				if v, ok := payload["model"].(string); ok {
					gotModel = v
				}
				_, _ = w.Write([]byte(`{"answers":{"train.corpus_farm":{"noul":0.9}}}`))
			}))
			t.Cleanup(srv.Close)
			withDistillTestContext(t, srv.URL, func(s *operation_setting.DistillDetectSetting) {
				s.JevModel = tc.configure
			})

			require.NotNil(t, EvaluateDistill(4200, "req-jev-model", "m", 1,
				[]byte(`{"model":"m","messages":[{"role":"user","content":"蒸馏语料"}]}`)),
				"a 0.9 verdict must be a hit")

			require.Equal(t, tc.want, gotModel, "the model field must be sent")
			require.Contains(t, gotKeys, "model", "model is required by the judge service")
			require.Contains(t, gotKeys, "state")
			require.Contains(t, gotKeys, "questions")
		})
	}
}

// 422 这类校验错误的**原因只在响应体里**。以前只记「HTTP 422」，运营看不到
// 缺的是哪个字段，只能手打接口复现。现在响应体前 500 字符要进 error 列。
func TestJudgeDistillRecordsTheResponseBodyOnHttpError(t *testing.T) {
	db := setupJevCallTestDB(t)
	const validation = `{"detail":[{"type":"missing","loc":["body","model"],"msg":"Field required"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(validation))
	}))
	t.Cleanup(srv.Close)
	withDistillTestContext(t, srv.URL, nil)

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"蒸馏语料批量生成"}]}`)
	require.Nil(t, EvaluateDistill(4106, "req-jev-422", "m", 12, body))

	rows := listJevCalls(t, db)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "http_error", r.Outcome)
	require.Equal(t, http.StatusUnprocessableEntity, r.HttpStatus)
	require.Contains(t, r.Error, "422")
	require.Contains(t, r.Error, `"loc":["body","model"]`,
		"the validator message must be visible without calling the service by hand")
	require.Contains(t, r.Error, "Field required")
}

// 响应体再长也不能把 error 列撑爆：最多 500 字符（按 rune 截断，不切半个汉字）。
func TestJudgeDistillTruncatesALongErrorBody(t *testing.T) {
	db := setupJevCallTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("错", 2000)))
	}))
	t.Cleanup(srv.Close)
	withDistillTestContext(t, srv.URL, nil)

	require.Nil(t, EvaluateDistill(4107, "req-jev-long", "m", 13,
		[]byte(`{"model":"m","messages":[{"role":"user","content":"蒸馏语料"}]}`)))

	rows := listJevCalls(t, db)
	require.Len(t, rows, 1)
	require.LessOrEqual(t, len([]rune(rows[0].Error)), 520, "the stored error must stay bounded")
}

func TestJudgeDistillRecordsAParseError(t *testing.T) {
	db := setupJevCallTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 2xx 但响应里没有可用的 noul 分数。
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	t.Cleanup(srv.Close)
	withDistillTestContext(t, srv.URL, nil)

	body := []byte(`{"model":"m","messages":[{"role":"user","content":"蒸馏语料批量生成"}]}`)
	require.Nil(t, EvaluateDistill(4104, "req-jev-parse", "m", 10, body))

	rows := listJevCalls(t, db)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "parse_error", r.Outcome)
	require.Equal(t, http.StatusOK, r.HttpStatus)
	require.Nil(t, r.Score)
	require.NotEmpty(t, r.Error)
}

// ⭐ 去重跳过不算调用：同一段正文在窗口内第二次不会调 Jev，也就不该多写一行。
// 这条同时锁住「去重只省 Jev 调用」的另一半 —— 省的次数在流水里可以直接核对。
func TestDistillDedupSkipWritesNoJevCallRow(t *testing.T) {
	db := setupJevCallTestDB(t)
	srv, calls := newDistillStubJev(t, 0.85)
	withDistillTestContext(t, srv.URL, nil)

	const uid = 4105
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"批量生成 500 条蒸馏语料"}]}`)

	first := EvaluateDistill(uid, "req-dedup-1", "m", 11, body)
	require.NotNil(t, first)
	require.False(t, first.DedupReused)

	nextDistillBucket()
	second := EvaluateDistill(uid, "req-dedup-2", "m", 11, body)
	require.NotNil(t, second, "a reused hit still counts as a confirmed hit")
	require.True(t, second.DedupReused)

	require.Equal(t, int32(1), atomic.LoadInt32(calls), "dedup must save the Jev call")
	rows := listJevCalls(t, db)
	require.Len(t, rows, 1, "a dedup-skipped judgement must not add a call row")
	require.Equal(t, "req-dedup-1", rows[0].RequestId)
}
