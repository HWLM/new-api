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
//   - Redis 启用   → 一段 Lua 里完成 "SET NX 抢锁 或 GET 校验后 EXPIRE 续租"，
//                    避免 SET NX / GET / EXPIRE 三步非原子造成"续别人的锁"。
//
// TTL 与心跳：
//   - TTL 90s，心跳 30s 一次（≤ TTL/2 保证一次心跳失败仍有时间恢复）。
//   - runLogAlertLeaderHeartbeat 在 evalOnce 期间持续续租，
//     避免"规则慢查询把 evalOnce 拖过 TTL → 锁过期 → 其他节点抢锁 → 双推"。
//   - evalOnce 本身还有 `logAlertEvalOnceHardDeadline` 硬截止兜底，
//     保证 evalOnce 总执行时间 < TTL - 缓冲，即便心跳挂了也不会跨节点重叠。
//
// Redis 故障时 tryAcquireLogAlertLeader 返回 false（宁可少发一次也不双推）。

const (
	logAlertLeaderKey            = "log_alert_evaluator:leader"
	logAlertLeaderTTL            = 90 * time.Second
	logAlertLeaderHeartbeat      = 30 * time.Second
	logAlertEvalOnceHardDeadline = 75 * time.Second // < TTL；给心跳/清理/网络抖动留 15s 缓冲
)

// 每个进程唯一 id：判断当前 key 是否仍由自己持有
var logAlertLeaderId = common.GetUUID()

// logAlertLeaderScript：
//   1) 若 key 不存在 → SET key = myId EX ttl → 返回 1（首次抢到）
//   2) 若 key 存在且值 == myId → EXPIRE key ttl → 返回 1（续租成功）
//   3) 否则返回 0（不是自己持有）
// 整段在 Redis 单线程里执行，杜绝 SetNX/Get/Expire 三步之间的时序窗口。
var logAlertLeaderScript = redis.NewScript(`
if redis.call("SET", KEYS[1], ARGV[1], "NX", "EX", ARGV[2]) then
    return 1
end
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("EXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

// logAlertLeaderReleaseScript：仅当 key 由自己持有时 DEL。
// evalOnce 结束后立刻释放锁，可以让其他节点无需等 TTL 就能接手（正常路径）；
// 崩溃/宕机路径下 TTL 兜底保证锁会过期。
var logAlertLeaderReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
`)

func tryAcquireLogAlertLeader() bool {
	if !common.RedisEnabled || common.RDB == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := logAlertLeaderScript.Run(
		ctx,
		common.RDB,
		[]string{logAlertLeaderKey},
		logAlertLeaderId,
		int(logAlertLeaderTTL.Seconds()),
	).Int64()
	if err != nil {
		common.SysError("log alert leader: script failed: " + err.Error())
		return false
	}
	return res == 1
}

// renewLogAlertLeader 由心跳 goroutine 调用。跟 tryAcquire 用同一段 Lua，
// 语义是 "如果我还是持有者就续 TTL"；返回值：true = 续到；false = 已失去锁。
func renewLogAlertLeader() bool {
	if !common.RedisEnabled || common.RDB == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := logAlertLeaderScript.Run(
		ctx,
		common.RDB,
		[]string{logAlertLeaderKey},
		logAlertLeaderId,
		int(logAlertLeaderTTL.Seconds()),
	).Int64()
	if err != nil {
		common.SysError("log alert leader: renew failed: " + err.Error())
		return false
	}
	return res == 1
}

// releaseLogAlertLeader 主动释放锁（正常路径）。失败也无所谓，TTL 会兜底。
func releaseLogAlertLeader() {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := logAlertLeaderReleaseScript.Run(
		ctx,
		common.RDB,
		[]string{logAlertLeaderKey},
		logAlertLeaderId,
	).Err(); err != nil && err != redis.Nil {
		common.SysError("log alert leader: release failed: " + err.Error())
	}
}
