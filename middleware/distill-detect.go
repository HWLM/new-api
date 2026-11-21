package middleware

import (
	"bytes"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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

// DistillDetect 在分发前抄一份请求正文，供 Distribute 选完渠道后再决定要不要判。
//
// ⛔ 为什么不在这里直接判（规格 §16）：判定范围是「本次请求**分发到的渠道**」，
// 而渠道要到 Distribute 里才选出来。这里只做便宜的配置级预筛（全局开关、
// 白名单非空、指定渠道、文本类路径），命中预筛就抄一份正文副本放上下文，
// 真正的判定在 runDistillDetection 里、拿到渠道之后才跑。
//
// ⛔ 用户三态（豁免 / 强制启用）不在这里查：那是一次用户缓存读取，放在每个
// relay 请求的热路径上不划算，而 runDistillDetection 与 EvaluateDistill 都会查。
//
// ⛔ 预筛必须尽可能严：抄正文是按请求计的内存开销，没可能被检测的请求不该抄。
func DistillDetect() gin.HandlerFunc {
	return func(c *gin.Context) {
		s := operation_setting.GetDistillDetectSetting()
		// 白名单为空 = 检测整体关闭：连正文都不抄。这条要在最前面，比任何
		// 每请求查询都便宜。
		if s == nil || !s.Enabled || len(s.DetectChannelIds) == 0 {
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
		if c.GetInt("id") <= 0 {
			c.Next()
			return
		}
		if body, ok := readBodyForDistill(c); ok {
			common.SetContextKey(c, constant.ContextKeyDistillRequestBody, body)
		}
		c.Next()
	}
}

// runDistillDetection 在 Distribute 选完渠道、做兜底重导之前执行（规格 §16）。
//
// 两条模式，边界很清楚：
//
//   - 观察模式（ObserveOnly=true，默认）：判定在后台 goroutine 里跑，不阻塞
//     用户请求、不改路由，只写 hits 与告警。
//   - 重导模式（ObserveOnly=false）：判定必须在这里**同步**跑完，因为「凑够
//     确认次数的那一次请求」本身就要立刻走兜底 —— 若放到响应之后，这一条
//     已经走原渠道回完了，等于漏掉。
//
// 返回本次新落库的命中记录（观察模式、未命中、没判一律 nil），调用方在重导
// 完成后用它回填「原渠道 → 兜底渠道」。
//
// ⛔ 白名单渠道之外一律零开销：不累加每分钟请求量、不抢时间桶、不去重、不调
// Jev、不写 hits、不告警。计数一旦提前做，非白名单渠道就会蹭掉白名单渠道的
// 激活阈值，白名单也就白配了。
func runDistillDetection(c *gin.Context, channel *model.Channel, modelName string) *model.DistillDetectHit {
	if channel == nil {
		return nil
	}
	s := operation_setting.GetDistillDetectSetting()
	if s == nil || !s.DetectsChannel(channel.Id) {
		return nil
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		return nil
	}
	// 用户三态（豁免 / 跟随全局 / 强制启用）在这里判，而不是在中间件里：
	// 中间件在每个 relay 请求上都会跑，那次用户缓存读取放那儿不划算；
	// 而这里只在「渠道命中白名单」之后才走到，频率低一个数量级。
	// ⛔ 必须排在降级态判断之前：被豁免的用户连已确认的降级标记也不该生效。
	if !service.IsDistillDetectEffectiveForUser(userID) {
		return nil
	}
	requestID := c.GetString(common.RequestIdKey)

	if s.ObserveOnly {
		body, ok := distillBodyFromContext(c)
		if !ok {
			return nil
		}
		if modelName == "" {
			modelName = distillModelNameFromBody(body)
		}
		// 这些值必须在 c 还活着的时候取出来：gin.Context 在请求结束后会被复用，
		// 后台 goroutine 不能再回读它。
		channelID := channel.Id
		gopool.Go(func() {
			hit := service.EvaluateDistill(userID, requestID, modelName, channelID, body)
			if hit == nil {
				return
			}
			// ⛔ 观察模式的记录也要能看出「这次落在哪个渠道」：判定挪到选完渠道
			// 之后，这个信息已经拿得到，以前恒为 0。用同步落库拿到自增 id 再回填
			// ——这里本来就在后台 goroutine 里，不影响请求延迟。
			service.PersistDistillHitSync(hit)
			service.UpdateDistillHitRouting(hit, channelID, 0)
		})
		return nil
	}

	// ── 重导模式 ────────────────────────────────────────────────
	// 已在降级态：不再判定（一轮只花一次 Jev），直接标记本次请求走兜底。
	// 这一步不需要正文，正文读失败也不该让已确认的用户逃掉。
	if service.IsUserDistillDemoted(c.Request.Context(), userID) {
		common.SetContextKey(c, constant.ContextKeyDistillDemoted, true)
		return nil
	}

	body, ok := distillBodyFromContext(c)
	if !ok {
		return nil
	}
	if modelName == "" {
		modelName = distillModelNameFromBody(body)
	}
	hit := service.EvaluateDistill(userID, requestID, modelName, channel.Id, body)
	if hit == nil {
		return nil
	}
	// 命中记录先落库：运营要靠 request_id 去追这次请求，不能等响应结束。
	service.PersistDistillHitSync(hit)
	if hit.DemoteUntil != nil {
		// 本条的命中让确认次数达标 ⇒ 降级从**这一条**开始生效。
		common.SetContextKey(c, constant.ContextKeyDistillDemoted, true)
		common.SetContextKey(c, constant.ContextKeyDistillDemotedScore, hit.Score)
	}
	return hit
}

// distillBodyFromContext 取中间件抄下来的正文副本。
func distillBodyFromContext(c *gin.Context) ([]byte, bool) {
	raw, ok := common.GetContextKey(c, constant.ContextKeyDistillRequestBody)
	if !ok {
		return nil, false
	}
	body, ok := raw.([]byte)
	if !ok || len(body) == 0 {
		return nil, false
	}
	return body, true
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
