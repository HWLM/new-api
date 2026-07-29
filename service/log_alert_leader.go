package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// 错误日志告警评估器的分布式 leader 锁。
// 场景：多 pod 部署时若都误认自己是 master（NODE_TYPE 未正确设置），
//   仅靠 common.IsMasterNode 会导致重复扫描/双推。加一道 Redis 抢主锁做兜底。
//
// 语义：
//   - Redis 未启用 → 返回 true，行为退化到既有的 NODE_TYPE 单主门禁；
//   - Redis 启用   → SET NX 抢锁；已持有者是自己则续 TTL；否则跳过本 tick。
// TTL 90s > tick 60s，允许一次 GC/慢查询抖动而不失去任期；
// Redis 故障时返回 false（宁可少发一次也不双推）。

const (
	logAlertLeaderKey = "log_alert_evaluator:leader"
	logAlertLeaderTTL = 90 * time.Second
)

// 每个进程唯一 id：判断当前 key 是否仍由自己持有
var logAlertLeaderId = common.GetUUID()

func tryAcquireLogAlertLeader() bool {
	if !common.RedisEnabled || common.RDB == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ok, err := common.RDB.SetNX(ctx, logAlertLeaderKey, logAlertLeaderId, logAlertLeaderTTL).Result()
	if err != nil {
		common.SysError("log alert leader: SetNX failed: " + err.Error())
		return false
	}
	if ok {
		return true
	}

	current, err := common.RDB.Get(ctx, logAlertLeaderKey).Result()
	if err == redis.Nil {
		// 极小时序窗口：SetNX 判定失败后 key 立刻过期，下一 tick 再抢
		return false
	}
	if err != nil {
		common.SysError("log alert leader: Get failed: " + err.Error())
		return false
	}
	if current != logAlertLeaderId {
		return false
	}
	if err := common.RDB.Expire(ctx, logAlertLeaderKey, logAlertLeaderTTL).Err(); err != nil {
		common.SysError("log alert leader: renew failed: " + err.Error())
		return false
	}
	return true
}
