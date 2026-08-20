# 视频任务资金链与计费链路

本文描述视频异步任务从请求进入、价格计算、预扣、上游提交、任务入库、轮询结算/退款到日志记账的完整链路，供将这套能力移植到其他项目时对照实现。

## 1. 先看结论

视频任务采用“提交时预扣，终态时差额结算”的资金模型：

```text
客户端请求
  -> 参数校验/规范化
  -> 计算基础价格 + 视频维度倍率
  -> 强制预扣（钱包或订阅，同时预扣 Token 配额）
  -> 调用视频上游
       | 失败：释放本次预扣（幂等退款）
       | 成功：立即结算提交阶段的最终金额，写入 Task.Quota
                   |
                   v
              异步轮询
       | SUCCESS：按上游实际参数/token 重算，补扣或退差额
       | FAILURE/超时：退回 Task.Quota
```

这里有两个容易混淆的金额：

- `RelayInfo.PriceData.Quota`：本次提交阶段计算出的额度；预扣前会叠加 `OtherRatios`。
- `Task.Quota`：任务当前仍被占用的预扣额度。成功后的差额结算以它为基准，退款成功后清零。

额度单位是内部 quota，基础换算由 `common.QuotaPerUnit` 控制（当前为 `500 * 1000`）。钱包 quota、Token quota、订阅 `amount_used` 都使用同一额度语义，但订阅还保留自己的预扣记录。

## 2. 入口和任务生命周期

视频路由集中到 `controller.RelayTask`：

- `POST /v1/videos`、`POST /v1/video/generations` 以及 Kling、Seedance 等兼容路径最终都进入这里。
- 中间件完成 Token/用户认证、渠道分发和请求格式转换。
- `relay.RelayTaskSubmit` 只负责一次渠道尝试；控制器可按 `common.RetryTimes` 重试。

任务状态：`NOT_START -> SUBMITTED/QUEUED -> IN_PROGRESS -> SUCCESS|FAILURE`。终态更新使用 `Task.UpdateWithStatus` 的 CAS，只有赢得状态迁移的轮询进程才允许结算或退款，避免多节点重复扣款。

关键代码：

| 责任 | 文件 |
| --- | --- |
| HTTP 路由 | `router/video-router.go` |
| 提交控制器、重试、提交后入库 | `controller/relay.go` (`RelayTask`) |
| 单次提交计费和上游调用 | `relay/relay_task.go` (`RelayTaskSubmit`) |
| 任务数据与计费快照 | `model/task.go` |
| 后台轮询、终态分支 | `service/task_polling.go` |

## 3. 请求校验和计费输入

`relay/common/relay_utils.go` 的 `ValidateBasicTaskRequest` 统一解析 JSON/multipart，并把标准化后的 `TaskSubmitReq` 放入 Gin context。至少要校验：

- `prompt` 非空；
- `duration/seconds` 不得为负且不得超过 `MaxTaskDurationSeconds`（当前 3600 秒）；
- 图片/视频输入、`size`、`mode`、`metadata` 等字段保留给渠道适配器。

计费表达式还会从原始 body 和 metadata 规范化提取变量（`taskcommon.ExtractTaskExprVars`）：

`seconds`、`resolution`、`size`、`has_video`、`has_image`、`n`、`mode`。

原始请求体在 tiered_expr 场景保存到 `TaskBillingContext.RequestBody`，这样后台轮询时仍能读取提交时的 `param()` 输入。

## 4. 提交阶段的价格计算

### 4.1 基础价格

`helper.ModelPriceHelperPerCall` 根据模型配置选择：

1. **固定单价（`usePrice=true`）**：
   `baseQuota = modelPrice * QuotaPerUnit * groupRatio`
2. **倍率计费（无固定单价）**：
   `baseQuota = modelRatio / 2 * QuotaPerUnit * groupRatio`
3. **tiered_expr**：执行表达式并把结果转换为 quota，保存 `BillingSnapshot`，不再走旧的倍率乘法。

若模型/分组被判定为免费且关闭免费模型预扣，则 `FreeModel=true`、预扣额度为 0。

### 4.2 视频维度倍率（OtherRatios）

非 tiered_expr 模型由适配器 `EstimateBilling` 返回附加倍率，常见键：

- `duration_estimate`：按请求时长相对默认时长（例如 5 秒）估算，只用于提交阶段保证预扣足够；
- `video_input`：输入视频、输出分辨率等导致的价格档位倍率；
- 其他渠道自定义倍率（分辨率、质量、批量数等）。

提交时执行：

`quota = QuotaFromFloatChecked(baseQuota * product(OtherRatios))`

所有倍率必须通过 `PriceData.AddOtherRatio` 进入，拒绝 `<=0`、NaN、Inf；转换使用 `common.QuotaFromFloatChecked`，发生 int32 饱和时记录 `RelayInfo.QuotaClamp` 并阻止继续预扣。

### 4.3 tiered_expr（表达式计费）

`modelPriceHelperTieredPerCall` 会冻结：表达式文本/hash、版本、分组倍率、命中 tier、预估变量和预扣 quota。预扣值由 `billingexpr.ComputeTieredQuotaFromCost` 产生。后台结算再次使用同一快照，只替换上游实际 `seconds/resolution/tokens`，不能读取最新配置覆盖历史任务。

## 5. 预扣：钱包、订阅和 Token quota

视频任务在 `RelayTaskSubmit` 中设置 `ForcePreConsume=true`，因此不能使用普通同步请求的“信任额度旁路”，必须在首次渠道尝试前完整预扣。`info.Billing != nil` 时重试不会重复预扣。

入口：`service.PreConsumeBilling -> NewBillingSession -> BillingSession.preConsume`。

资金来源按用户 `BillingPreference` 选择：`wallet_only`、`subscription_only`、`wallet_first`、`subscription_first`。允许的回退关系由 `NewBillingSession` 决定；订阅预扣失败是否回退钱包还要经过订阅的 overflow 配置。

预扣顺序：

1. **Token quota**：`PreConsumeTokenQuota` 先锁定 API Token 的额度；失败直接拒绝请求。
2. **资金来源**：
   - 钱包：`DecreaseUserQuota(userID, amount)`；同时更新 `RelayInfo.UserQuotaBefore/After`。
   - 订阅：`PreConsumeUserSubscription(requestId, ..., amount)` 在事务中锁定可用订阅，写入 `SubscriptionPreConsumeRecord`（按 `requestId` 幂等），并增加 `amount_used`。
3. 任一步失败都回滚已成功的 Token quota 或钱包/订阅预扣。

订阅有一个实现细节：即使 `preConsumedQuota=0`，`NewBillingSession` 也会把订阅预扣量提升为最小值 1，以便创建可幂等追踪的订阅预扣记录；移植时要明确是否保留这个“最小消费单位”规则。

实际预扣数写入 `RelayInfo.FinalPreConsumedQuota`；订阅额外写入订阅 ID、计划、预扣量和预扣后的剩余额度字段，供日志展示。

## 6. 上游提交、重试和提交后结算

`RelayTaskSubmit` 的顺序是：

1. 解析渠道/模型并校验请求；
2. 计算 `PriceData` 和 `OtherRatios`；
3. 首次尝试强制预扣；
4. 构建渠道请求、调用上游、解析上游 task ID；
5. 适配器可执行 `AdjustBillingOnSubmit`，用上游返回的提交信息修正倍率并重算 `finalQuota`；
6. 返回 `TaskSubmitResult{UpstreamTaskID, TaskData, Quota: finalQuota}`。

控制器行为：

- **所有尝试失败**：defer 调用 `relayInfo.Billing.Refund(c)`，退回本次完整预扣。
- **提交成功**：先调用 `SettleBilling(c, relayInfo, result.Quota)`，对提交阶段金额做一次差额补扣/退款；然后创建 `model.Task`。

当前实现中 `BillingSession.Refund` 通过 goroutine 异步执行；`SettleBilling` 出错时控制器记录 `SysError` 但仍继续写入任务。因此迁移时应配套可靠的退款重试/对账任务，不能把 HTTP 请求返回视为资金操作已经完成。

入库时保存：

- `Task.Quota = result.Quota`；
- `PrivateData.BillingSource/SubscriptionId/TokenId/NodeName`；
- `PrivateData.BillingContext`：模型价格、冻结分组/模型倍率、`OtherRatios`、`PerCallBilling`、视频输入标记，以及 tiered_expr 快照/变量/原始请求体；
- 上游 task ID、渠道、模型映射、原始响应（脱敏后）。

`PerCallBilling=true` 的任务（`TASK_PRICE_PATCH` 或按次价格模式）在终态跳过二次差额结算，提交成功时的金额即最终金额。

## 7. 轮询终态和资金变化

系统任务 `async_task_poll` 调用 `RunTaskPollingOnce`，按平台/渠道批量查询未完成任务。每个任务解析成 `TaskInfo`，其中 `TotalTokens`、`Resolution`、`DurationSeconds` 是结算输入。

### 7.1 成功：差额结算

成功状态 CAS 更新后调用 `settleTaskBillingOnComplete`，优先级如下：

1. **tiered_expr**：`settleTaskTieredExpr` 用快照表达式重算最终 quota；上游实际分辨率/时长覆盖冻结变量，`TotalTokens` 可作为 `p/c/len`；然后进入统一差额结算。
2. **按次计费**：`PerCallBilling` 直接跳过。
3. **适配器明确金额**：`AdjustBillingOnComplete` 返回正数，直接作为最终 quota。
4. **按 token 重算**：当 `TotalTokens>0` 时：

   `actualQuota = QuotaFromFloatChecked(totalTokens * frozenModelRatio * frozenGroupRatio * product(frozenOtherRatios))`

5. 没有任何可靠的实际用量时，保留预扣金额，不做猜测性退款。

`RecalculateTaskQuota` 计算 `delta = actualQuota - Task.Quota`：

- `delta > 0`：从钱包/订阅补扣，并补扣 Token quota；更新用户/渠道已用 quota 和请求数；
- `delta < 0`：退回差额，并减少用户已用 quota；
- 更新 `Task.Quota = actualQuota`，记录 consume/refund 差额日志。

适配器若实现 `TaskPollingRatiosAdjuster`，可在 token 重算前用上游实际分辨率替换 `BillingContext.OtherRatios`。例如 Doubao/Seedance 会删除仅用于预扣的 `duration_estimate`，再用上游真实分辨率重查 `video_input`。

### 7.2 失败：全额退款

失败状态 CAS 更新后调用 `RefundTaskQuota`：

1. `ClaimRefundQuota` 使用 `WHERE id=? AND quota=?` 原子把 `Task.Quota` 置 0，保证多节点/重复轮询只有一个退款执行者；
2. 钱包调用 `IncreaseUserQuota`，订阅调用 `RefundSubscriptionPreConsume(requestId)`（事务 + 状态幂等）；
3. 退回 Token quota，减少用户已用 quota；
4. 写入 refund 日志。资金退款失败时恢复 `Task.Quota`，保留任务以便重试或人工对账。

### 7.3 超时

每轮轮询前 `sweepTimedOutTasks` 将超过 `TASK_TIMEOUT_MINUTES` 的未完成任务 CAS 标记为失败，并对新任务执行全额退款。`TaskRefundLegacyCutoff` 之前的历史任务只标失败、不自动退款，避免旧数据重复退款。

### 7.4 当前代码中的异常缺口

以下轮询异常目前是“标记失败但不经过 `RefundTaskQuota`”的批量路径，移植时应明确这是兼容行为还是需要修正：

- 任务缺少上游 task ID 时，`RunTaskPollingOnce` 用 `TaskBulkUpdateByID` 直接标记失败；
- 轮询时取不到渠道信息时，`updateVideoTasks` 也用批量更新直接标记失败；
- 单任务查询/解析暂时失败时通常只记日志，任务保留未完成状态，等待下一轮或最终超时退款。

如果这些任务已经发生过预扣，前两种批量失败路径会留下未退款的 `Task.Quota`，这是迁移项目需要补充的对账/退款策略。

## 8. 账务日志和审计字段

提交成功会写一次普通 consume 日志；异步阶段的补扣/退款由 `RecordTaskBillingLog` 写独立日志。日志通常包含：

- `quota`、`before_quota`、`after_quota`；
- `task_id`、模型、渠道、Token ID、分组；
- `pre_consumed_quota`、`actual_quota`、上游 token 用量；
- `billing_mode`、表达式 hash/version、命中 tier、匹配参数；
- `admin_info.quota_saturation`（发生饱和/NaN 时，仅管理员可见）。

用户/渠道累计用量只在真实 consume 发生时增加；退款用负变更抵销。订阅日志另带预扣量、计划和预扣后余额。

## 9. 移植时必须保留的契约

1. **金额状态分离**：提交会话的 `preConsumedQuota` 与持久化任务的 `Task.Quota` 都要有；后者是后续轮询的唯一退款/差额基准。
2. **首次预扣 + 重试复用**：重试只能换渠道，不能再次预扣；最终失败必须统一退款。
3. **冻结计费上下文**：模型倍率、分组倍率、附加倍率和表达式快照必须随任务保存，不能在轮询时读取当前配置。
4. **钱包/订阅双资金源**：预扣、补扣、退款都要实现相同的正负 delta 语义；订阅预扣必须有 request ID 幂等记录。
5. **Token quota 同步**：资金余额和 Token quota 必须一起预扣、一起补扣/退款；失败回滚顺序要可恢复。
6. **终态 CAS**：只有状态迁移获胜者可以触发结算/退款；批量无 CAS 更新不能用于账务终态。
7. **安全边界**：时长、批次数、分辨率倍率等用户输入先限幅；quota 转换使用饱和 helper，禁止裸 `int(float)`；饱和时拒绝预扣并留下审计信息。
8. **结算优先级**：tiered_expr > 按次跳过 > 适配器最终金额 > token 重算 > 保留预扣，顺序改变会造成重复计费。
9. **失败可重试**：退款失败时不能清空任务 quota；订阅退款使用事务和幂等状态，钱包退款要避免重复执行。
10. **结果 URL 与敏感数据**：任务私有字段保存上游 key/结果 URL，返回用户前需走代理或脱敏；这不改变金额链路，但影响任务迁移后的数据模型。

## 10. 建议的迁移实现顺序

1. 先实现 `Task`、`TaskPrivateData`、`TaskBillingContext` 和状态 CAS；
2. 实现抽象 `FundingSource`（wallet/subscription）及 Token quota 预扣；
3. 接入基础价格、分组倍率、`OtherRatios` 和 quota 饱和转换；
4. 实现提交会话的预扣/提交后结算/失败退款；
5. 实现轮询、成功差额结算、失败/超时退款；
6. 最后接入 tiered_expr、适配器实际分辨率修正和账务审计日志。

迁移完成后至少验证这些场景：提交失败退款、渠道重试只预扣一次、钱包余额不足、订阅切换/幂等退款、成功补扣、成功退差额、重复终态轮询、超时退款、表达式计算失败保留预扣、超大时长被拒绝以及 quota 饱和被拒绝。
