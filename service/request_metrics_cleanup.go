package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

const (
	cleanupInterval     = 6 * time.Hour
	cleanupInitialDelay = time.Minute
)

func StartRequestMetricsCleanup(ctx context.Context) {
	go runMetricsCleanup(ctx)
}

func runMetricsCleanup(ctx context.Context) {
	common.SysLog("request metrics cleanup task started")
	initial := time.NewTimer(cleanupInitialDelay)
	defer initial.Stop()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			common.SysLog("request metrics cleanup task stopped")
			return
		case <-initial.C:
			doMetricsCleanup()
		case <-ticker.C:
			doMetricsCleanup()
		}
	}
}

func doMetricsCleanup() {
	days := setting.GetMetricsRetentionDays()
	before := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rows, err := model.CleanupRequestMetricsLogsBefore(cleanupCtx, before)
	if err != nil {
		common.SysError("request metrics cleanup failed: " + err.Error())
		return
	}
	common.SysLog(fmt.Sprintf("request metrics cleanup: deleted %d rows older than %d days", rows, days))
}
