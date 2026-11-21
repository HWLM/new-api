## 21. 速率限制兜底与拒答降级拆开 + 用户分组速率（2026-11-20 用户定稿）

用户已拍板：兜底走**渠道**（不是分组）；与拒答拆干净；总并发与分组并发都算取最严；429 独立错误码；走兜底后**按原分组计费**。

### 21.1 两套兜底必须分离

现状：并发超限与拒答降级都写 `ContextKeyUserOverloaded`，`Distribute` 里同一分支处理，都跳 `channel.FallbackChannelId`。改为：

| | 拒答降级 | 速率限制 |
|---|---|---|
| 触发标记 | `ContextKeyRefusalDemoted`（现有） | 新增 `ContextKeyRateLimitExceeded` |
| 兜底渠道 | `channel.FallbackChannelId`（各渠道自配，**不动**） | 新增全局 `rate_limit_fallback_channel_id` |
| 计费分组 | 原分组（不动） | 原分组（用户定） |
| 失败形态 | 现有拒答 429 文案（不动） | 新增 429 + code `rate_limit_exceeded` |

`Distribute` 里两个分支独立判断、独立跳转，不许合并成一段通用逻辑；两条路径的测试并排写，谁合并谁红。

### 21.2 配置

**系统设置 → 速率限制**新增：

| 项 | 默认 | 说明 |
|---|---|---|
| 速率超限兜底渠道 | 0（未配） | 全局单选一个渠道 id；与渠道自配的 `FallbackChannelId` 彼此独立 |
| 兜底不可用时 | 固定 429 | code `rate_limit_exceeded`，文案与拒答降级不同 |

兜底渠道可用性校验：存在、`status=enabled`、支持当前请求模型；任一不满足 → 视为不可用 → 429。

### 21.3 用户分组速率

用户表新增 JSON 列 `group_rate_limits`：

```json
{"vip":{"limit":20,"fallback":true},"default":{"limit":5,"fallback":false}}
```

- `limit`：`>0` 自定义并发上限；`-1` 该分组不限；缺省或 `0` = 该分组不单独限制（只受总并发约束）。
- `fallback`：默认 `true`（走兜底渠道）；`false` 时该分组并发到顶直接 429。
- 只有出现在 map 里的分组才单独计数；其它分组只受总并发约束。
- 用户编辑页「最大并发请求数」下方新增该表格：分组、并发上限、是否走兜底；分组下拉用系统已配置的分组名。

### 21.4 计数与判定

- 总并发沿用现有 Redis 键 `concurrency:user:<id>`（in-flight + TTL），行为不变。
- 分组并发新增键 `concurrency:user:<id>:group:<group>`，同一套 INCR/DECR + TTL。
- 两个额度都判、**取最严**：任一超限即视为速率超限。总并发超限时，**只有该分组配了 limit>0** 才按该分组的 fallback 策略决定走兜底还是 429；分组没配额度（limit<=0 或缺省）就维持旧行为。
- 判定顺序：拒答降级先判（保持现状），速率再判；两者都命中时以拒答为准（拒答的兜底与文案优先），并在日志里标出两者都命中。

### 21.5 边界

- 兜底只跳一跳：兜底渠道自身不再跳、不再判断并发兜底，避免循环。
- 兜底渠道 = 当前渠道时视为不可用 → 429。
- 管理员指定渠道（`TokenSpecificChannelId`）的请求跳过整套逻辑（现状保持）。
- 不配置任何分组速率 = 行为与现在完全一致。
- 429 响应体带 `code: rate_limit_exceeded`，并区分两个原因文案：并发超限被拒 / 兜底渠道不可用。

### 21.6 验收

1. 用户配 `vip:{limit:2,fallback:true}`，并发 3 条 → 第 3 条走全局兜底渠道，日志 `group` 仍是原分组。
2. 同配置 `fallback:false` → 第 3 条 429，code `rate_limit_exceeded`。
3. 不配兜底渠道 → 429，文案是「兜底不可用」。
4. 该用户所在分组**没配**分组速率（limit<=0 或缺省）时，只超总并发 → 仍是旧行为（走渠道自配 FallbackChannelId）；配了 limit>0 的分组，其 fallback 策略对总并发超限同样生效。
5. 拒答降级用户 → 与改动前逐字节一致（兜底、文案、计费都不变）。


## 22. 速率兜底改走「请求所在渠道自配的兜底」（2026-11-21 用户定稿）

原 §21 引入的全局配置项 `RateLimitFallbackChannelId` **废弃并移除**（设置页下拉一并去掉；
数据库里残留的 option 行不再读取）。原因：平台同时有 Gemini 与 OpenAI 渠道，全局一个兜底
渠道会把 Gemini 请求送进只认 OpenAI 的渠道，必须按请求实际落到的渠道来选兜底。

### 22.1 规则

| 场景 | 行为 |
|---|---|
| 分组 `fallback=false` 且超限 | 429 `rate_limit_exceeded`，「该分组未启用兜底」（不变） |
| 分组 `fallback=true` 且超限，渠道配了可用兜底 | 跳到该渠道的 `FallbackChannelId`，只跳一跳 |
| 分组 `fallback=true` 且超限，渠道没配/兜底禁用/不支持该模型或路径/自环 | 429 `rate_limit_exceeded`，「该渠道未配置可用的兜底渠道」 |
| 没配分组速率、只超总并发 | 旧行为（同一条 `redirectToFallbackChannel`，不变） |

- 兜底可用性校验与拒答降级完全一致（存在、`enabled`、支持模型、支持路径、非自环），
  复用同一个 `redirectToFallbackChannel`。
- **只跳一跳**：不跟随兜底渠道自身的 `FallbackChannelId`。
- 计费仍按**原分组**，不碰 `UsingGroup/AutoGroup`。
- 拒答降级与蒸馏检测两条链路一行不动。

### 22.2 影响与验收

- 每个要兜底的主力渠道必须配 `fallback_channel_id`，否则 `fallback=true` 也会 429。
- 429 两种原因文案保持可区分：分组策略拒 / 渠道无可用兜底；两者都不与蒸馏兜底的
  `distill_fallback_unavailable` 口径混用。
- 验收新增：同模型 `fallback=true` → 落该渠道兜底且 group 不变；未配 → 429；
  兜底禁用 → 429；兜底只支持别家模型（Gemini 请求 vs OpenAI-only 兜底）→ 429；
  兜底渠道自身再配兜底时只跳一跳、不跟第二跳。
