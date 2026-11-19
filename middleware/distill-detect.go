package middleware

import (
	"bytes"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// distillBodyReadLimit 判定最多读这么多字节的请求正文。
//
// 批处理语料请求常常是几十 MB 的 JSON，但判断用途只需要前几百 KB：
// 头部就是「任务说明 + rubric + 正负例」，尾部只是同一批任务的重复。
const distillBodyReadLimit = 2 << 20 // 2 MiB

// distillTextClassPaths 只有这些路径参与判定 —— 与拒答降级的
// IsTextClassRelayFormat 同一作用域：其它 relay（图像/音频/任务/embedding）
// 的正文形状完全不同，判了只会浪费调用。
var distillTextClassPaths = []string{
	"/v1/messages",
	"/v1/chat/completions",
	"/v1/completions",
	"/v1/responses",
	"/v1/responses/compact",
	"/pg/chat/completions",
}

// isDistillTextClassPath 判断本次请求是否属于「文本类」relay。
//
// ⛔ 用路径而不是 relayFormat：这个中间件挂在 Distribute 之前，那时
// relayFormat 还没进上下文；而路径是此刻唯一稳定可用的信号。
func isDistillTextClassPath(path string) bool {
	for _, p := range distillTextClassPaths {
		if strings.HasSuffix(path, p) {
			return true
		}
	}
	return false
}

// DistillDetect 在分发前对请求做一次「蒸馏用途」抽样判定。
//
// 两种模式，边界很清楚：
//
//   - 观察模式（ObserveOnly=true，默认）：判定在**后台 goroutine** 里跑，
//     不阻塞用户请求、不改路由，只写 hits 与告警。
//   - 重导模式（ObserveOnly=false）：判定必须在 Distribute **之前同步**跑完，
//     因为「凑够确认次数的那一次请求」本身就要立刻走兜底 —— 若放到
//     c.Next() 之后，这一条已经走原渠道回完了，等于漏掉。
//
// ⛔ 重导模式下判定只花在「可能真的命中」的请求上：先看该用户是否已在降级态
// （直接标记，不再调 Jev），再看阈值/抽样/去重，与观察模式同一套口径。
//
// ⛔ 不判的情形全部短路，一次外部调用都不发（含：全局关闭、用户豁免、非文本类
// relay、未达激活阈值、本轮未抽中、已在降级中、正文去重命中）。
func DistillDetect() gin.HandlerFunc {
	return func(c *gin.Context) {
		s := operation_setting.GetDistillDetectSetting()
		if s == nil || !s.Enabled {
			c.Next()
			return
		}
		if _, pinned := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); pinned {
			// 管理员指定渠道的请求不参与，与并发/降级的行为一致。
			c.Next()
			return
		}
		if !isDistillTextClassPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		userID := c.GetInt("id")
		if userID <= 0 {
			c.Next()
			return
		}
		if !service.IsDistillDetectEffectiveForUser(userID) {
			c.Next()
			return
		}
		body, ok := readBodyForDistill(c)
		if !ok {
			c.Next()
			return
		}

		requestID := c.GetString(common.RequestIdKey)
		modelName := distillModelNameFromBody(body)

		if s.ObserveOnly {
			// 这些值必须在 c 还活着的时候取出来：gin.Context 在请求结束后会被复用，
			// 后台 goroutine 不能再回读它。
			if modelName == "" {
				modelName = c.GetString("original_model")
				if modelName == "" {
					modelName = c.GetString("model")
				}
			}
			c.Next()
			gopool.Go(func() {
				hit := service.EvaluateDistill(userID, requestID, modelName, body)
				service.RecordDistillHit(hit)
			})
			return
		}

		// ── 重导模式 ────────────────────────────────────────────────
		// 已在降级态：不再判定（一轮只花一次 Jev），直接标记本次请求走兜底。
		if service.IsUserDistillDemoted(c.Request.Context(), userID) {
			common.SetContextKey(c, constant.ContextKeyDistillDemoted, true)
			c.Next()
			return
		}

		hit := service.EvaluateDistill(userID, requestID, modelName, body)
		if hit == nil {
			c.Next()
			return
		}
		// 命中记录先落库：运营要靠 request_id 去追这次请求，不能等响应结束。
		service.PersistDistillHitSync(hit)
		if hit.DemoteUntil != nil {
			// 本条的命中让确认次数达标 ⇒ 降级从**这一条**开始生效。
			common.SetContextKey(c, constant.ContextKeyDistillDemoted, true)
			common.SetContextKey(c, constant.ContextKeyDistillDemotedScore, hit.Score)
		}
		c.Next()

		// 路由结果此时才确定：回填「原渠道 → 兜底渠道」，供运营对账。
		served := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		fallbackServed := common.GetContextKeyInt(c, constant.ContextKeyDistillFallbackChannelId)
		original := served
		fallbackID := 0
		if fallbackServed > 0 {
			original = common.GetContextKeyInt(c, constant.ContextKeyFallbackFromChannelId)
			fallbackID = fallbackServed
		}
		service.UpdateDistillHitRouting(hit, original, fallbackID)
	}
}

// distillModelNameFromBody 从请求正文里取模型名。
//
// ⛔ 不能只靠 c.GetString("original_model")：那个键由 Distribute 里的
// SetupContextForSelectedChannel 写入，而这个中间件运行在 Distribute 之前，
// 那时它还是空的（观察模式早期就是这样导致 hits.model 一直为空）。
func distillModelNameFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	return strings.TrimSpace(gjson.GetBytes(body, "model").String())
}

// readBodyForDistill 复制一份请求正文供判定使用。
//
// ⛔ 必须复制：请求结束后 `BodyStorageCleanup` 会释放存储，后台 goroutine
// 再读就是空内容（甚至读到已关闭的临时文件）。
func readBodyForDistill(c *gin.Context) ([]byte, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return nil, false
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	buf, err := io.ReadAll(io.LimitReader(storage, distillBodyReadLimit))
	// 读完把位置还原，后面的中间件/relay 还要正常读它。
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, false
	}
	if err != nil || len(buf) == 0 {
		return nil, false
	}
	return bytes.Clone(buf), true
}
