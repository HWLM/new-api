package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// 异步埋点 writer:
//   - 请求路径调用 SubmitRequestMetrics 非阻塞投递到 buffer chan
//   - 后台 worker 每 5s 或满 100 条 flush 一次,批量 INSERT 到 request_metrics_logs
//   - buffer 满直接 drop,只增加计数器,绝不阻塞业务
//
// 进程退出时由 StartRequestMetricsWriter 的 ctx 触发,先把残余批次落盘再返回。

const (
	metricsBufferCap     = 4096
	metricsFlushInterval = 5 * time.Second
	metricsFlushBatch    = 100
)

var (
	metricsBuffer       = make(chan *model.RequestMetricsLog, metricsBufferCap)
	metricsDroppedCount atomic.Int64
	metricsWrittenCount atomic.Int64
	metricsWriterOnce   sync.Once
)

// SubmitRequestMetrics 在 controller/relay.go 的 Relay 函数 defer 中调用。
// statusCode 是最终返回给客户端的 HTTP 状态码;apiErr 为 nil 表示成功。
// 非阻塞:buffer 满时 drop 并计数,不影响业务请求。
func SubmitRequestMetrics(c *gin.Context, info *relaycommon.RelayInfo, statusCode int, apiErr *types.NewAPIError) {
	if info == nil || c == nil {
		return
	}
	row := buildRequestMetricsRow(c, info, statusCode, apiErr)
	if row == nil {
		return
	}
	select {
	case metricsBuffer <- row:
	default:
		metricsDroppedCount.Add(1)
	}
}

func buildRequestMetricsRow(c *gin.Context, info *relaycommon.RelayInfo, statusCode int, apiErr *types.NewAPIError) *model.RequestMetricsLog {
	now := time.Now()

	var durationMs int
	if !info.StartTime.IsZero() {
		durationMs = int(now.Sub(info.StartTime).Milliseconds())
	}

	var firstTokenMs int
	if !info.StartTime.IsZero() && info.FirstResponseTime.After(info.StartTime) {
		firstTokenMs = int(info.FirstResponseTime.Sub(info.StartTime).Milliseconds())
	}

	var (
		errType         int16
		isBusiness      bool
		errCode         string
		errMessage      string
		finalStatusCode = statusCode
	)
	if apiErr != nil {
		if finalStatusCode == 0 {
			finalStatusCode = apiErr.StatusCode
		}
		if finalStatusCode == 0 {
			finalStatusCode = 500
		}
		errMessage = TruncateErrorMessage(apiErr.Error(), 512)
		rawCode := string(apiErr.GetErrorCode())
		errType, isBusiness = ClassifyError(finalStatusCode, errMessage)
		errCode = NormalizeErrorCode(rawCode, finalStatusCode)
	} else if finalStatusCode == 0 {
		finalStatusCode = 200
	}

	modelName := info.OriginModelName
	if modelName == "" {
		modelName = info.UpstreamModelName
	}

	return &model.RequestMetricsLog{
		RequestId:        c.GetString(common.RequestIdKey),
		UserId:           info.UserId,
		Username:         c.GetString("username"),
		TokenId:          info.TokenId,
		ChannelId:        info.ChannelId,
		ChannelType:      int16(info.ChannelType),
		ModelName:        modelName,
		Group:            info.UsingGroup,
		StatusCode:       int16(finalStatusCode),
		DurationMs:       durationMs,
		FirstTokenMs:     firstTokenMs,
		IsStream:         info.IsStream,
		ErrorType:        errType,
		IsBusinessError:  isBusiness,
		ErrorCode:        errCode,
		ErrorMessage:     errMessage,
		CreatedAt:        now.Unix(),
	}
}

// StartRequestMetricsWriter 在 main.go 启动时调用一次,run-forever。
// ctx 取消时,worker 把 buffer 剩余项 flush 后返回。
func StartRequestMetricsWriter(ctx context.Context) {
	metricsWriterOnce.Do(func() {
		go runMetricsWriter(ctx)
	})
}

func runMetricsWriter(ctx context.Context) {
	common.SysLog("request metrics writer started")
	batch := make([]*model.RequestMetricsLog, 0, metricsFlushBatch)
	ticker := time.NewTicker(metricsFlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// 用独立 context 写入,避免父 ctx 已取消导致最后一批失败
		writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := model.BatchInsertRequestMetricsLogs(writeCtx, batch); err != nil {
			common.SysError("request metrics writer batch insert failed: " + err.Error())
		} else {
			metricsWrittenCount.Add(int64(len(batch)))
		}
		batch = batch[:0]
	}

	for {
		select {
		case row, ok := <-metricsBuffer:
			if !ok {
				flush()
				return
			}
			batch = append(batch, row)
			if len(batch) >= metricsFlushBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			// drain 剩余项
			for {
				select {
				case row := <-metricsBuffer:
					batch = append(batch, row)
				default:
					flush()
					common.SysLog("request metrics writer stopped")
					return
				}
			}
		}
	}
}

// GetRequestMetricsStats 返回 writer 运行时统计(供管理后台/监控查看)。
func GetRequestMetricsStats() (written int64, dropped int64, bufferUsed int) {
	return metricsWrittenCount.Load(), metricsDroppedCount.Load(), len(metricsBuffer)
}
