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
// ⚠️ 观察模式（本轮实现）：判定在**后台 goroutine** 里跑，命中只写 hits 与告警，
// 既不阻塞用户请求，也不改变路由。等观察期数据回来确认误伤后，再打开重导 ——
// 那时需要把判定挪到分发之前同步执行（规格 §12 第 3 步），因为「命中的这一次
// 请求就要走兜底」。
//
// ⛔ 不判的情形全部短路，一次外部调用都不发：
//   - 全局关闭 / 该用户被豁免；
//   - 非文本类 relay；
//   - 该用户当前分钟请求量未达激活阈值；
//   - 本轮未抽中（抽样间隔）；
//   - 已处于一轮降级中（一轮只花一次）；
//   - 同一正文在去重窗口内已判过。
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

		// 这些值必须在 c 还活着的时候取出来：gin.Context 在请求结束后会被复用，
		// 后台 goroutine 不能再回读它。
		requestID := c.GetString(common.RequestIdKey)
		modelName := c.GetString("original_model")
		if modelName == "" {
			modelName = c.GetString("model")
		}

		c.Next()

		gopool.Go(func() {
			hit := service.EvaluateDistill(userID, requestID, modelName, body)
			service.RecordDistillHit(hit)
		})
	}
}

// readBodyForDistill 复制一份请求正文供后台判定使用。
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
