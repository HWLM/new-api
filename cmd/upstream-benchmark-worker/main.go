package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

func main() {
	if os.Getenv("NODE_TYPE") == "" {
		_ = os.Setenv("NODE_TYPE", "slave")
	}
	common.InitEnv()
	logger.SetupLogger()
	if err := model.InitDB(); err != nil {
		common.FatalLog("failed to initialize database: " + err.Error())
	}
	if err := model.InitLogDB(); err != nil {
		common.FatalLog("failed to initialize log database: " + err.Error())
	}
	service.InitHttpClient()
	defer model.CloseDB()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	runnerID := fmt.Sprintf("upstream-benchmark-worker-%s", common.NodeName)
	pollInterval := common.GetEnvOrDefault("UPSTREAM_BENCHMARK_WORKER_POLL_SECONDS", 2)
	lockSeconds := common.GetEnvOrDefault("UPSTREAM_BENCHMARK_WORKER_LOCK_SECONDS", 300)
	healthPort := common.GetEnvOrDefault("UPSTREAM_BENCHMARK_WORKER_HEALTH_PORT", 8081)
	healthServer := &http.Server{Addr: fmt.Sprintf(":%d", healthPort), Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" && request.URL.Path != "/readyz" {
			http.NotFound(response, request)
			return
		}
		if request.URL.Path == "/readyz" {
			db, err := model.DB.DB()
			if err != nil || db.PingContext(request.Context()) != nil {
				http.Error(response, "database unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		response.WriteHeader(http.StatusOK)
	}), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.SysError("worker health server failed: " + err.Error())
		}
	}()
	defer healthServer.Shutdown(context.Background())
	if err := service.RunUpstreamBenchmarkWorker(ctx, runnerID, pollInterval, lockSeconds); err != nil {
		common.FatalLog(err.Error())
	}
}
