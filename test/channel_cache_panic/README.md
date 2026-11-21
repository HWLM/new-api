# 渠道缓存定时同步 panic 验收（fix/channel-cache-sync-error）

对应崩溃：`model.InitChannelCache()` 里 `DB.Find(&abilities)` 的错误被忽略，查询失败返回 0 行后
`groups` 为空，随后按 `channel.Group` 往 `newGroup2model2channels[group][model]` 写就对 nil map 赋值 →
整进程 panic（`channel_cache.go:62`，实测 `panic: assignment to entry in nil map`）。

## 跑法

```bash
export CC_BASE_URL=http://127.0.0.1:3005
export CC_ADMIN_TOKEN=...            # 管理员 access token
export CC_RELAY_KEY=sk-...           # 普通用户 Key
export CC_SQLITE=/path/to/instance.db
export CC_MODEL=stub-model
export CC_SYNC_SECONDS=5             # 该实例的 SYNC_FREQUENCY，脚本据此算等几轮
python3 channel_cache_panic.py
```

## 怎么造“查询失败”

把 `ALTER TABLE abilities RENAME TO abilities_hidden`，等两三轮定时同步，再改回来。
比占锁稳，也不依赖锁的语义。

## 断言

| 编号 | 断言 |
|---|---|
| C1 | 基线：relay 200、`/api/channel` 可读 |
| C2 | `abilities` 查询失败时进程不 panic（对照组：旧构建 5 秒内退出） |
| C3 | 期间 relay 仍 200（旧渠道缓存保留，没被 0 行结果覆盖） |
| C4 | 破坏前后渠道集合一致 |
| C5 | 恢复表名后进程存活、relay 可用 |

## 踩过的坑

1. **定时同步只在 `MEMORY_CACHE_ENABLED=true` 时才启动**（`main.go` 里 `if common.MemoryCacheEnabled`），
   只设 `SYNC_FREQUENCY` 不会跑，用例会假绿。没有 Redis 的实例要显式给 `MEMORY_CACHE_ENABLED=true`。
2. **不要用 SQLite 排他锁复现**：rollback journal 模式下 `BEGIN EXCLUSIVE` 实测只拦写，读照样通过，
   同步仍然 `channels synced from database`，复现不出来（第一次就是这么白跑的）。
3. 启动时那次 `InitChannelCache` 有 `recover` + 重试，**不会**崩；只有定时同步那一次没有 recover，
   所以必须让同步真的跑一轮才能看到 panic。
4. 版本号：自己 `go build` 出来的二进制 `/api/status` 显示 `v0.0.0`，不代表版本不对，
   靠 commit 和构建来源核对即可。
