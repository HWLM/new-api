# 速率限制兜底验收（§22：走请求所在渠道自配的兜底）

对应 `docs/rate-limit-fallback-split.md` §21/§22。黑盒，只走 HTTP；并发用线程同时发，
上游由脚本自带（请求正文里写 `slow:2500` 就 sleep 2500ms），不依赖外部慢速上游。

§22 相对 §21 的改动：删除全局设置项 `RateLimitFallbackChannelId`，速率超限后改跳
**请求所在渠道自配**的 `FallbackChannelId`（与拒答降级共用同一字段，单跳、不递归）。
本目录的断言按 §22 重写过，不是把旧的全局兜底渠道换个名字。

## 跑法

```bash
export RL_BASE_URL=http://127.0.0.1:3105
export RL_ADMIN_TOKEN=...      # 管理员 access token（会过期，跑前重新登录）
export RL_RELAY_KEY=sk-...
export RL_USER_ID=1
export RL_MODEL=rl-model
export RL_REDIS_ADDR=127.0.0.1:6380   # RL5/RL8/RL9 造降级态要用；不给则这三条打 SKIP
export RL_REDIS_DB=3
python3 rate_limit_acceptance.py              # 全跑
python3 rate_limit_acceptance.py --case RL4,RL5
```

脚本自建渠道（跑完删除、配置还原）：

| 渠道 | 模型 | 用途 |
|---|---|---|
| `rl-main` | `rl-model` | 主渠道，priority=100，每个用例自己改它的 `fallback_channel_id` |
| `rl-oldfb` | `rl-model` | 渠道自配兜底（速率超限 / 拒答降级 / 旧行为都走这里） |
| `rl-decoy` | `rl-model` | 诱饵：任何请求落这里都说明又有人把全局兜底加回来了 |
| `rl-badfb` | `rl-model-other` | 被设成兜底但**不支持该模型**，验模型可用性校验 + 跨厂商 |
| `rl-gemini` | `rl-gemini-pro` | 跨厂商用例的主渠道：只认 gemini 模型，兜底指向只认 openai 的 `rl-badfb` |
| `rl-dead` | `rl-model` | 建出来就是 `status=2`（已禁用），验「兜底渠道被禁用 → 429」 |
| `rl-distillfb` | `rl-model` | 蒸馏兜底（RL8/RL9 用） |

`rl-oldfb`(2) 与 `rl-decoy`(3)、`rl-distillfb`(5) 的 id 互不相同，从日志落点就能判断走了哪条链路。

## 用例

| 编号 | 断言 |
|---|---|
| RL1 | `{limit:2,fallback:true}` + 渠道配兜底，并发 3 → 全 200；恰好 1 条落**渠道自配**兜底；那条 `group` 仍是 `default`（计费不变）；诱饵渠道没被用 |
| RL2 | `fallback:false` → 恰好 1 条 429、`code=rate_limit_exceeded`、文案含「未启用兜底」；**渠道即使配了兜底也不许跳** |
| RL3 | 兜底不可用五种形态：未配 0 / 不支持该模型 / 指向自己（自环）/ 渠道已禁用 / **跨厂商**（gemini 模型配了只认 openai 的兜底）→ 429 + `rate_limit_exceeded` +「该渠道未配置可用的兜底渠道」，且都不许跳过去；自环还要证明不递归、3 条在 30s 内返回 |
| RL4 | 只配总并发 2、分组额度空 → 超限那条走渠道自配兜底（旧行为不变） |
| RL5 | 拒答降级回归：仍走渠道自配兜底、`group` 不变；渠道没配兜底时 429 文案仍是「风控降级」口径且**不带** `rate_limit_exceeded` |
| RL6 | 速率 429 带 code、并发口径，且不混「风控降级」措辞 —— 谁把两条路径合并，这里立刻红 |
| RL7 | 分组写了条目但 `limit<=0`（`0+false`、`-1+false`、`0+true`）→ 视为没配分组速率：只超总并发**不 429**、走渠道自配兜底 |
| RL8 | 蒸馏降级态 + 并发超限 → **直接 429、不再跳第二跳**：`[200,429,429]`，都带 `rate_limit_exceeded`，日志只有通过的那条（落蒸馏兜底）；控制组「降级+未超限 → 200 走蒸馏兜底」 |
| RL9 | 重导模式下「命中把确认次数凑满、同时并发超限被 429」→ 命中记录 `fallback_id` 必须 0（实际没被重导）。占位请求走**非白名单模型**占并发槽，按 429 响应里的 request id 回查 hits |
| RL10 | 旧全局 key 写进去也**不影响路由**（仍 429、诱饵渠道没被用）；渠道自配 `fallback_channel_id` 仍可读写 |
| RL11 | 单跳：兜底渠道自己还配了兜底（2→3）→ 超限那条只落 2，渠道 3 零命中 |
| RL12 | 兜底渠道完全没配分组 + 原分组为空 → 无法计费，必须 429「未配置可用的兜底渠道」，不许重导（渠道建不出空分组时打 SKIP） |

## 踩过的坑

1. **`PUT /api/channel/` 不接受 `status` 字段**，带了直接「无效的参数」；要从 GET 结果里挑白名单字段回传。
2. **日志断言要按日志 id 划界**，不能只按秒切窗口：相邻用例同秒跑完会把上一条的兜底行算进来。
3. 开了 Redis 的实例改完渠道要 `POST /api/channel/fix` 重建 abilities 缓存，否则一律 503。
4. 并发要真的重叠：上游延时靠正文 `slow:N` 触发。
5. 管理员 access token 会过期，跑前重新 `POST /api/user/login`。
6. **跨用例污染**：RL8/RL9 会让服务端写 `distill:demote:{user:N}`（TTL 600~900s），之后跑
   非蒸馏用例会被降级到蒸馏兜底，表现为 429 文案变成「并发请求数已超限，请联系管理员」、
   日志渠道变成蒸馏兜底。所以每个非蒸馏用例开头都先 `clear_distill_state()`（删降级键 +
   `POST /api/distill_detect/clear_cache {"scope":"all"}`）。线上排查“莫名 429”同样先想到这个键。
7. **`PUT /api/option/` 对任何未知 key 都返回 200 success**（`updateOptionMap` 的 switch 没有
   default 分支），并且会把值写进 options 表。这是全局既有行为，不是 §22 引入的；所以 RL10
   不验状态码，只验「旧 key 不影响路由」。副作用：写过之后 `GET /api/option/` 会多出一个
   没有代码路径的死键（本机测试实例无所谓，别在生产上试）。
