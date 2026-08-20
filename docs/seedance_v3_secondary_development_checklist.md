# Seedance V3 相关二开功能清单

> 用途：将 `docs/seedance_v3_api.md` 所涉及的现有能力补全到原项目。以下以当前仓库实际实现为准，并标出文档与代码不一致之处。

## 一、P0：统一 API 与核心任务链路

### 1. 注册统一对外路由

- [ ] `POST /api/v3/contents/generations/tasks`：创建视频任务。
- [ ] `GET /api/v3/contents/generations/tasks/:task_id`：查询视频任务。
- [ ] `POST /api/v3/open/CreateAsset`：创建素材。
- [ ] `POST /api/v3/open/GetAsset`：查询素材。
- [ ] 保留兼容路由 `POST /v3/open/GetAsset`。
- [ ] 所有路由使用本站 Bearer Token 鉴权，并沿用现有渠道分发、模型限制、分组和限流链路。

涉及模块：`router/video-router.go`、`middleware/distributor.go`、`controller/task.go`、`relay/relay_task.go`。

验收：路由存在；创建请求按 `model` 选择渠道；查询请求按本站任务 ID 找回原任务，不要求客户端持有上游凭证。

### 2. 补齐统一 Seedance DTO

- [ ] 模型常量及识别：
  - `dreamina-seedance-2-0-hc`
  - `doubao-seedance-2-0-filter-off`
  - `doubao-seedance-2-0`
  - `doubao-seedance-2-0-fast`
  - `doubao-seedance-2-0-260128`
  - `doubao-seedance-2-0-fast-260128`
- [ ] 视频请求：`model`、`content`、`duration`、`resolution`、`ratio`、`generate_audio`、`watermark`。
- [ ] `content` 支持 `text`、`image_url`、`video_url`、`audio_url`、`role`。
- [ ] Doubao 扩展字段须完整保留：`tools`、`service_tier`、`draft`、`frames`、`camera_fixed`，以及现有适配器已支持的 `callback_url`、`return_last_frame`、`execution_expires_after`、`safety_identifier`、`priority`、`seed`。
- [ ] 创建素材请求：`model`、`url`、`name`、`AssetType`。
- [ ] 查询素材请求：`model`、大小写敏感的 `Id`。
- [ ] 统一任务响应：`id`、`model`、`status`、`content.video_url`、`duration_seconds`、`outputs`、`usage`、`error`、时间字段、`last_frame_url`。
- [ ] 可选标量使用指针并配合 `omitempty`，确保显式 `false`、`0`、`-1` 不被丢弃。

涉及模块：`dto/seedance_v3.go`、`relay/channel/task/doubao/adaptor.go`、`relay/channel/task/sdrealmax/adaptor.go`。

### 3. 请求归一化与字段无损透传

- [ ] 为统一创建任务路由增加请求转换中间件。
- [ ] 提取 `model`、文本提示词、图片和 `duration` 到通用 `TaskSubmitReq`，供现有分发与计费链路使用。
- [ ] HC 请求额外保存强类型 `SeedanceV3VideoRequest` 到 context，供 Byteplus 适配器读取。
- [ ] 非 HC 请求把完整原始 JSON 保存到 `Metadata`，确保 `video_url`、`audio_url`、`tools` 及未知上游字段不丢失。
- [ ] 多个文本项按顺序合并；构建上游请求时避免产生重复文本项。
- [ ] Doubao 的 `duration=-1` 只保留在 metadata，不作为通用计费乘数。
- [ ] 对超大、非整数 duration 在进入计费前返回 `400`。
- [ ] 素材路由中间件只提取 `model` 用于渠道选择，保留请求主体。

涉及模块：`middleware/seedance_v3_adapter.go`、`middleware/seedance_v3_asset.go`、`constant/context_key.go`。

### 4. Byteplus 渠道与适配器

- [ ] 新增渠道类型 `81`，显示名 `Byteplus`，默认 Base URL 为 `https://model.service-inference.ai`。
- [ ] 注册任务适配器及模型列表。
- [ ] `dreamina-seedance-2-0-hc` 视频默认转发到 `POST /v1/video/generate`。
- [ ] 素材创建转换到 `POST /v1/sd/assets`：客户端小写 `url/name` 转为上游 `URL/Name`，移除 `model`。
- [ ] 素材查询转换到 `GET /v1/sd/assets/:asset_id`，并把成功结果归一化为统一素材结构。
- [ ] 同一 Byteplus 渠道配置了 Doubao 模型时，非 HC 请求委托 Doubao 适配器处理，不能误发到 `/v1/video/generate`。
- [ ] HC 校验：必须含非空文本；duration `4~15`；resolution `480p/720p/1080p`；ratio 使用 HC 支持集合；图片支持 HTTP(S) 或非空 `asset://`；视频只接受公网 HTTP(S)；拒绝 Base64/data URL。
- [ ] HC 默认值：duration `5`、resolution `480p`、ratio `16:9`、`generate_audio=false`、`watermark=false`。

涉及模块：`constant/channel.go`、`relay/relay_adaptor.go`、`relay/channel/task/sdrealmax/*`、`setting/ratio_setting/model_ratio.go`。

### 5. Doubao Seedance 2.0 适配

- [ ] 注册五个 Doubao Seedance 2.0 模型及版本化模型。
- [ ] 默认创建任务上游：`POST /api/v3/contents/generations/tasks`。
- [ ] 默认查询任务上游：`GET /api/v3/contents/generations/tasks/:upstream_task_id`。
- [ ] 保留原始 `content` 顺序和所有媒体项；图片、视频、音频 URL 原样转发。
- [ ] 支持 `duration=-1`、`4K`、`adaptive` 和 Doubao 扩展字段。
- [ ] 解析上游 `content.video_url`、`outputs`、`usage`、实际 `resolution`、实际 `duration`、错误信息。
- [ ] 将 `pending/queued`、`processing/running`、`succeeded`、`failed/expired` 映射到内部任务状态；对外查询时保留 `expired` 语义。

涉及模块：`relay/channel/task/doubao/*`。

### 6. 本站任务 ID 与异步查询

- [ ] 提交前生成公开 ID：`task_<32位随机串>`。
- [ ] 对外创建响应只返回本站任务 ID，不暴露上游 ID。
- [ ] `Task.PrivateData.UpstreamTaskID` 保存上游真实 ID；旧数据缺失时兼容回退到 `TaskID`。
- [ ] 查询时按用户和本站任务 ID读取任务，再通过适配器输出统一 Seedance 响应。
- [ ] 持久化原始模型名、上游模型名、渠道、任务数据、结果 URL、失败原因、计费上下文。
- [ ] 统一状态：`queued`、`running`、`succeeded`、`failed`，Doubao 额外输出 `expired`。
- [ ] 成功时补齐 `outputs`；若上游没有 URL，则使用本站视频代理 URL。

涉及模块：`model/task.go`、`controller/task_video.go`、`relay/relay_task.go`、`relay/channel/adapter.go`。

## 二、P0：素材管理

### 7. 创建素材

- [ ] 按 body 中的 `model` 选择渠道；素材接口不使用静态模型白名单，允许渠道配置的自定义模型。
- [ ] `AssetType` 默认 `Image`，支持 `Image/Video/Audio`。
- [ ] `asset_base_url` 未配置时回退主 Base URL。
- [ ] Doubao 默认透传到 `/v3/open/CreateAsset`。
- [ ] Byteplus 使用 `/v1/sd/assets` 并将上游响应归一化为 `{"id":"..."}`。
- [ ] 上游 HTTP 状态码和错误信息尽量原样保留。
- [ ] 创建素材不扣费，但记录独立素材日志：渠道、模型、素材名、类型、素材 ID、成功状态、响应摘要、耗时。

涉及模块：`controller/seedance_v3_asset.go`、`model/log.go`。

### 8. 查询素材

- [ ] 新旧入口均可用：`/api/v3/open/GetAsset`、`/v3/open/GetAsset`。
- [ ] 查询只执行一次，不在网关内等待状态变化，不参与视频计费。
- [ ] HC 缺少 `Id` 返回 `400`；其他模型默认保留上游校验行为。
- [ ] 支持响应字段：`Id`、`Status`、`AssetType`、`Name`、`URL`、`GroupId`、`CreateTime`、`UpdateTime`、`base_resp`。
- [ ] Byteplus 查询成功但上游未返回状态时，统一补为 `Active`。
- [ ] 素材不存在、鉴权、限流和服务错误保留合理的 `404/401/429/5xx`。

### 9. 明确素材自动上传策略

- [ ] 必须在移植前选择并固定一种契约：
  - **当前代码契约（建议按此补全）**：视频适配器不自动调用 CreateAsset；客户端先调用素材接口，待 `Active` 后传 `asset://<id>`。`contains_face` 即使出现也原样留在 Doubao metadata，当前适配器不会触发上传。
  - **文档旧契约**：`contains_face=true` 时网关自动创建素材、轮询到 `Active`、替换为 `asset://`，且不得把 `contains_face` 发给上游。
- [ ] 若必须实现文档旧契约，需额外补充：仅适用媒体类型、轮询间隔、总超时、429 重试策略、取消传播、失败退款、幂等、模型一致性、日志和回归测试。
- [ ] 同步修订 `docs/seedance_v3_api.md` 与 `docs/openapi/relay.json`，避免继续描述当前不存在的自动上传行为。

## 三、P0：计费、退款与安全

### 10. Seedance 视频计费

- [ ] 为相关模型补默认 `ModelRatio`；HC 当前复用 `doubao-seedance-2-0` 价格表。
- [ ] 按输出分辨率和是否包含视频输入计算 `video_input` 倍率：
  - Doubao 2.0/HC：基础 480p/720p 无视频 `46`；有视频 `28`；1080p 无视频 `51`；有视频 `31`；4K 无视频 `26`；有视频 `16`。
  - fast：无视频 `37`；有视频 `22`。
- [ ] 预扣按 `duration / 5` 增加 `duration_estimate`；缺失或 `-1` 按 5 秒基准。
- [ ] 提交时冻结 `HasVideoInput`、模型、分组倍率、OtherRatios、资金来源和 Token ID。
- [ ] 任务成功后使用上游实际 resolution/duration/tokens 差额结算；移除 `duration_estimate`，避免重复计费。
- [ ] 任务失败或超时只退款一次；使用原子 Claim/Restore 与 CAS 防止多实例/重叠轮询重复退款。
- [ ] 支持钱包、订阅和 Token 额度同步补扣/退款并记录日志。

### 11. tiered_expr 动态计费

- [ ] 任务链路支持冻结表达式快照、原始请求体和变量：`seconds`、`resolution`、`has_video`、`has_image`、`n`、`mode`。
- [ ] Seedance 省略或使用 adaptive resolution 时，预扣按 `720p` 归一化；结算用上游实际 resolution 覆盖。
- [ ] 结算阶段以实际 duration/resolution/tokens 重跑表达式，执行补扣或退款。
- [ ] 管理端提供 Doubao Seedance 2.0 分档价格模板。
- [ ] 所有 quota 转换使用 `common.Quota*Checked`，饱和事件写入管理员日志 `quota_saturation`。

涉及模块：`pkg/billingexpr/*`、`relay/channel/task/taskcommon/expr_vars.go`、`service/task_tiered_settle.go`、`setting/billing_setting/tiered_billing.go`。

### 12. 输入和计费安全

- [ ] 所有可能成为计费乘数的字段在请求边界设置上限，至少包括 `duration`、`frames` 和透传 metadata 中的同名字段。
- [ ] 禁止裸 `int(float64(...))` 或无界 decimal 转换；使用集中 quota helper。
- [ ] `OtherRatios` 只能通过 `PriceData.AddOtherRatio/ReplaceOtherRatios` 写入，拒绝非正数、NaN、Inf。
- [ ] 超大 duration 必须 `400`，不能进入预扣或产生负额度。
- [ ] 失败、超时、重复轮询、数据库写入失败时，计费状态可恢复且有审计日志。

## 四、P1：可配置上游兼容层

### 13. Seedance 路由配置

- [ ] 渠道 `other_settings` 增加：
  - `asset_base_url`
  - `seedance_v3_routes.asset_create`
  - `seedance_v3_routes.asset_get`
  - `seedance_v3_routes.task_create`
  - `seedance_v3_routes.task_get`
- [ ] 每条路由支持 `method`、`target`、`parameters`、`response_mapping`。
- [ ] method 允许 `GET/POST/PUT/PATCH`；target 支持绝对 HTTP(S) URL或以 `/` 开头的相对路径。
- [ ] `task_get` 支持 `{task_id}`；`asset_get` 支持 URL 中 `{asset_id}`，参数映射中可用 `{Id}`。
- [ ] GET 路由将 parameters 写入 query；非 GET 写入 JSON body。
- [ ] parameters 以递归 merge patch 覆盖现有请求；`null` 删除字段。
- [ ] 精确 `{field.path}` 从当前请求或响应复制值并保留类型；`{field?.path}` 允许来源缺失，结果按 `null` 处理。
- [ ] response mapping 在适配器解析前执行。
- [ ] 提交时冻结 task_get 路由到任务私有数据，避免渠道后续改配置导致历史任务无法轮询。
- [ ] `relaykit/` DTO 独立实现并验证：`cd relaykit && GOWORK=off go build ./...`。

涉及模块：`relaykit/dto/channel_settings.go`、`dto/channel_settings.go`、`relay/channel/task/taskcommon/seedance_routes.go`。

### 14. 按模型选择上游请求格式

- [ ] `video_request_format_by_model` 支持 `openai` 与 `seedance_v3`，并支持 `*` fallback。
- [ ] 对 Doubao/Byteplus 混合渠道，可按原始模型决定 `/v1/video/generations` 请求是原样 OpenAI 兼容格式，还是转换为 Seedance V3 原生格式。
- [ ] 校验空模型名、未知格式和重复规则。
- [ ] 不得用 model mapping 代替该功能，两者职责不同。

## 五、P1：视频结果代理、日志与运维

### 15. 视频内容代理

- [ ] 提供 `GET /v1/videos/:task_id/content`，支持 API Token 或后台登录态。
- [ ] 普通用户只能访问自己的任务；管理员可预览其他用户任务。
- [ ] 任务必须成功后才能取视频。
- [ ] 按渠道解析视频 URL；支持 data URL、上游 URL和 OpenAI 原生 content 端点。
- [ ] 使用 SSRF 防护；带渠道代理时仍执行 URL/域名/IP/端口校验。
- [ ] 过滤上游 `Content-Disposition: attachment`，统一改为 `inline`；缺失或 octet-stream 时补 `video/mp4`。
- [ ] 流式转发响应并设置合理缓存。

涉及模块：`controller/video_proxy.go`、`router/video-router.go`。

### 16. 任务轮询与运维恢复

- [ ] 后台轮询按渠道和平台获取异步任务状态。
- [ ] 终态更新使用 CAS，避免并发轮询覆盖或重复结算。
- [ ] 成功后保存结果 URL、usage、实际分辨率和时长；失败后保存失败原因并退款。
- [ ] 上游无法识别的响应进入明确失败或可诊断状态。
- [ ] 支持卡住任务超时扫描和修复脚本；区分历史不退款任务与新任务。
- [ ] 自定义 task_get 路由必须参与后台轮询，而不仅是客户端主动查询。

涉及模块：`service/task_polling.go`、`scripts/fix_stuck_task.go`。

### 17. 日志与审计

- [ ] 任务消费日志记录请求路径、模型价、模型倍率、分组倍率、OtherRatios、模型映射和 tiered_expr 信息。
- [ ] 素材创建日志使用独立 `LogTypeAsset`，不计入消费。
- [ ] 差额补扣、退款、quota saturation 均记录可对账日志。
- [ ] 不得长期保留当前适配器中的 `temporary Seedance upstream request ... body=...` 明文请求日志；上线前删除或改为受控 Debug，并对 URL/Base64/敏感字段脱敏。

## 六、P1：管理端与用户端

### 18. 渠道管理

- [ ] 前端注册 `DoubaoVideo(54)`、`Byteplus(81)`；Byteplus 默认 Base URL 与模型建议列表需包含六个统一模型。
- [ ] 渠道表单支持 `asset_base_url`、四类 Seedance route、parameters、response mapping。
- [ ] 路由配置 UI 提供四个 Tab、method、URL、参数 JSON、响应映射 JSON和占位符说明。
- [ ] type `54/81` 展示 Seedance 路由配置；type `81` 展示 Asset Base URL。
- [ ] 增加按模型请求格式编辑器；type `45/54/81` 可用；支持模型 datalist、`*` fallback、重复规则校验。
- [ ] settings 编辑回填、保存和空值删除行为须有前端测试。

涉及模块：`web/src/features/channels/*`。

### 19. 模型详情和 API 示例

- [ ] Seedance 2.0 模型统一展示 `POST /api/v3/contents/generations/tasks`。
- [ ] 提供 curl、Python、TypeScript、JavaScript 示例，使用 `content/duration/resolution/ratio/generate_audio/watermark`。
- [ ] HC 参数表与 Doubao 参数表分别展示支持范围；Doubao 增加 `-1/4K/adaptive/tools/service_tier/draft/frames/camera_fixed`。
- [ ] 模型识别覆盖版本化模型，不能误匹配 Seedance 1.x。

涉及模块：`web/src/features/pricing/*`。

### 20. 任务日志视频预览

- [ ] 成功的视频任务显示“点击预览视频”。
- [ ] 前端通过已鉴权 API client 获取 `/v1/videos/:task_id/content` Blob，不直接访问 `result_url`。
- [ ] 使用 React Query、Object URL、`<video controls playsInline preload="metadata">`；关闭时 revoke URL。
- [ ] 提供 loading/error UI，并覆盖普通用户、管理员跨用户、URL 编码和失败响应测试。

涉及模块：`web/src/features/usage-logs/*`。

### 21. i18n

- [ ] 新增渠道、路由配置、参数映射、请求格式、模型参数和视频预览文案。
- [ ] 更新 `en/zh/zh-TW/fr/ru/ja/vi` flat JSON。
- [ ] 执行 `cd web && bun run i18n:sync`。

## 七、P1：文档与测试

### 22. OpenAPI 与接入文档

- [ ] 在 `docs/openapi/relay.json` 注册五条路由和所有 schema。
- [ ] 文档模型列表补齐两个版本化 Doubao 模型。
- [ ] OpenAPI 同样取消静态模型枚举，或明确其只是示例；当前运行时允许渠道配置自定义模型。
- [ ] 统一错误体与实际中间件/控制器响应格式保持一致。
- [ ] 明确“Seedance V3”是统一 API 名称，模型实际为 Seedance 2.0，避免误新增 Seedance 3 模型。
- [ ] 修正文档与当前素材自动上传策略的不一致。

### 23. 后端测试

- [ ] 路由注册和旧路径兼容。
- [ ] 中间件：HC 强类型保存、Doubao 原始字段无损保留、动态模型、`duration=-1`、超大 duration。
- [ ] Byteplus：视频构建、素材创建/查询转换、缺失字段、错误透传、非 HC 委托。
- [ ] Doubao：metadata overlay、模型映射、统一创建/查询响应、状态和 usage。
- [ ] 路由映射：GET/非 GET、query/body、占位符、递归 patch、可选字段、响应映射。
- [ ] 计费：分辨率、视频输入、时长预扣、实际参数结算、失败/超时退款、并发幂等、quota 饱和。
- [ ] 素材日志与视频代理权限/SSRF。
- [ ] 新测试使用 `testify/require` 和 `testify/assert`。

### 24. 前端测试与构建

- [ ] 渠道 settings 回填/保存测试。
- [ ] Seedance API sample 与参数表测试。
- [ ] 视频预览权限与组件交互测试。
- [ ] 执行受影响测试、`bun run typecheck`、涉及文件 lint、`bun run build`。
- [ ] 若修改 `relaykit/`：执行 `cd relaykit && GOWORK=off go build ./...`。
- [ ] 后端执行相关 `go test`，并至少执行根模块 `go build ./...`。

## 八、建议移植顺序

1. DTO、模型常量、渠道类型和 `relaykit` settings。
2. 路由、中间件、本站任务 ID/上游任务 ID 分离。
3. Doubao 与 Byteplus 适配器。
4. 素材 Create/Get。
5. 后台轮询、状态归一化、视频代理。
6. 预扣、差额结算、失败退款、tiered_expr。
7. 自定义路由与按模型请求格式。
8. 渠道管理、模型详情、任务视频预览、i18n。
9. OpenAPI、接入文档和完整回归测试。

## 九、最小可交付验收

- [ ] 六个内置模型均可通过统一创建路由选到正确渠道。
- [ ] 创建响应和查询响应始终使用本站 task ID，绝不泄露上游 task ID。
- [ ] 公网图片、`asset://`、视频和音频输入按渠道能力正确处理且字段不丢失。
- [ ] 素材 Create/Get、新旧 GetAsset 路径可用且不计费。
- [ ] 成功任务可查询和代理播放；失败任务只退款一次。
- [ ] 分辨率、视频输入、时长和上游 token 的预扣/结算可对账，无负额度和溢出。
- [ ] 渠道可配置自定义任务/素材路由及请求/响应映射。
- [ ] 管理端可配置、查看 API 示例并预览视频。
- [ ] 文档、OpenAPI、代码和测试对 `contains_face` 策略保持一致。
