package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const tokenExhaustingTickInterval = 5 * time.Minute

var (
	tokenExhaustingOnce    sync.Once
	tokenExhaustingRunning atomic.Bool
)

// StartTokenExhaustingSnapshotTask 启动「即将耗尽密钥」快照定时任务
// 每 5 分钟重算一次 token_exhausting_snapshot 表
func StartTokenExhaustingSnapshotTask() {
	tokenExhaustingOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("token exhausting snapshot task started: tick=%s", tokenExhaustingTickInterval))
			ticker := time.NewTicker(tokenExhaustingTickInterval)
			defer ticker.Stop()

			runTokenExhaustingSnapshotOnce()
			for range ticker.C {
				runTokenExhaustingSnapshotOnce()
			}
		})
	})
}

func runTokenExhaustingSnapshotOnce() {
	if !tokenExhaustingRunning.CompareAndSwap(false, true) {
		return
	}
	defer tokenExhaustingRunning.Store(false)

	ctx := context.Background()
	if err := model.RefreshExhaustingSnapshot(time.Now().Unix()); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("token exhausting snapshot refresh failed: %v", err))
	}
}
