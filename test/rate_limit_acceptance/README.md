# 速率限制兜底拆分验收（§21）

对应 `docs/rate-limit-fallback-split.md` §21.1–§21.6。黑盒，只走 HTTP；并发用线程同时发，
上游由脚本自带（请求正文里写 `slow:2500` 就 sleep 2500ms），不依赖外部慢速上游。

## 跑法

```bash
export RL_BASE_URL=http://127.0.0.1:3102
export RL_ADMIN_TOKEN=...      # 管理员 access token（会过期，跑前重新登录）
export RL_RELAY_KEY=sk-...
export RL_USER_ID=1
export RL_MODEL=rl-model
export RL_REDIS_ADDR=127.0.0.1:6380   # RL5 造假降级态要用；不给则 RL5 打 SKIP
export RL_REDIS_DB=0
python3 rate_limit_acceptance.py              # 全跑
python3 rate_limit_acceptance.py --case RL4,RL5
```

脚本自建四个渠道（跑完删除、配置还原）：

| 渠道 | 模型 | 用途 |
|---|---|---|
| `rl-main` | `rl-model` | 主渠道，priority=100，自配 `fallback_channel_id` 指向 `rl-oldfb` |
| `rl-oldfb` | `rl-model` | 渠道自配兜底（拒答降级 / 旧行为走这里） |
| `rl-globalfb` | `rl-model` | 全局速率兜底（`RateLimitFallbackChannelId`） |
| `rl-badfb` | `rl-model-other` | 配成全局兜底但**不支持该模型**，验可用性校验 |
| `rl-distillfb` | `rl-model` | 蒸馏兜底（RL8 用） |

两个兜底渠道 id 不同是有意的：RL4 与 RL1 只差「有没有配分组额度」，靠落点不同区分两条链路。

## 用例

| 编号 | 规格 | 断言 |
|---|---|---|
| RL1 | §21.6-1 | `{limit:2,fallback:true}` 并发 3 → 全 200；恰好 1 条日志渠道=全局兜底；那条日志 `group` 仍是 `default` |
| RL2 | §21.6-2 | `fallback:false` → 恰好 1 条 429、`code=rate_limit_exceeded`、文案含「未启用兜底」；且没有任何一条落全局兜底 |
| RL3 | §21.6-3 | 兜底不可用两种形态（未配 0 / 不支持该模型）→ 429 + `rate_limit_exceeded` + 「兜底渠道不可用」 |
| RL4 | §21.6-4 | 只配总并发 2、分组额度空 → 超限那条走**渠道自配**兜底，不走全局兜底（旧行为） |
| RL5 | §21.6-5 | 拒答降级回归：仍走渠道自配兜底、`group` 不变；渠道没配兜底时 429 文案仍是「风控降级」口径且**不带** `rate_limit_exceeded` |
| RL6 | §21.1 | 两种 429 并排：速率 429 带 code，拒答 429 不带 code —— 谁把两段合并成一条通用逻辑，这里立刻红 |
| RL9 | §21.4 边界（对账） | 重导模式下「这条命中恰好把确认次数凑满、同时并发超限被 429」→ 命中记录的 `fallback_id` 必须是 0（它实际没被重导）。做法：占位请求走**非白名单模型**占住唯一并发槽，命中请求走白名单模型 → 429；按 429 响应里的 request id 回查 hits |
| RL8 | §21.4 边界 | 蒸馏降级态 + 并发超限 → **直接 429，不再跳第二跳**：`limit=1` 并发 3 得 `[200,429,429]`，两条 429 都带 `rate_limit_exceeded`、文案是并发超限口径（不含「兜底渠道不可用」），日志里只有通过的那条（落在蒸馏兜底渠道）；另加控制组「降级 + 未超限 → 200 走蒸馏兜底」 |
| RL7 | §21.3 / §21.6-4 | 分组写了条目但 `limit<=0`（`0+false`、`-1+false`、`0+true` 三种写法）→ 视为没配分组速率：只超总并发必须 **不 429**、走渠道自配兜底、不碰全局速率兜底 |

RL7 是补的洞：RL1–RL6 全 PASS 但没覆盖 `limit<=0` 的分组条目，`decideConcurrencyAction` 当时用
`hasGroupCfg`（map 里有条目）而不是 `groupLimited`（`limit>0`）做判据，导致 `{"limit":0,"fallback":false}`
在只超总并发时被误 429。这条用例在 `5b3d833bb` 上返回 `[200,200,429]`，`9f2f572a4` 起返回 `[200,200,200]`。
教训：新增分支要专门有一条「配置写了但取默认值」的用例。

RL9 有两个必须先处理的坑：①占位请求必须走非白名单模型，否则它会把用户这一轮的判定时间桶
用掉（`judge_interval_seconds` 按用户分桶），命中请求被桶跳过、根本不命中；②request id 只能从
429 响应体里正则取，再拿它去 hits 里查，别按时间窗猜。

RL8 造蒸馏降级态：往 Redis 写 `distill:demote:{user:N}`（EX 900），并把蒸馏检测配成
`enabled=true / observe_only=false / detect_channel_ids=[主渠道] / fallback_channel_id=蒸馏兜底`。
三个兜底渠道 id 互不相同（自配 2 / 全局速率 3 / 蒸馏 5），从落点就能判断走了哪条链路。
在 `5b3d833bb` 上这条是 `[200,200,200]` 三条全被蒸馏兜底接走（降级绕过并发限制），
`bfe7c283d` 起是 `[200,429,429]`。

RL5 造降级态的办法：用户 `refusal_demote_override=1` + 往 Redis 写
`refusal:demote:{user:N}`（EX 600），跑完删键、override 还原 0。内存模式的实例没法从外部伪造，
所以没给 Redis 就打 SKIP。

## 踩过的坑

1. **`PUT /api/channel/` 不接受 `status` 字段**，带了直接 `{"message":"无效的参数"}`。不能把
   `GET /api/channel/` 拿到的整条对象原样回传（里面有 `status`、`channel_info` 等只读字段），
   要自己拼白名单字段。
2. **日志断言要按日志 id 划界**，不能只按秒切窗口：相邻用例在同一秒内跑完，上一条用例
   落在兜底渠道的行会被算进下一条，RL2 会假红。
3. 开了 Redis 的实例建完渠道要 `POST /api/channel/fix` 重建 abilities 缓存，否则一律 503。
4. 并发必须真的重叠：上游延时靠正文里的 `slow:N` 触发，2.5s 足够让三条请求同时在 flight。
5. 管理员 access token 会过期，跑前重新 `POST /api/user/login`。
