# 二次开发 PR 拆分与功能清单

本文记录 `release` 相对官方上游的二次开发能力，作为后续上游合并、回归测试、PR 拆分和发布验收的共同依据。它描述的是业务契约和验证边界，不以当前文件位置或历史提交顺序作为长期契约。

## 1. 基线信息

| 项目 | 值 |
| --- | --- |
| 生成日期 | 2026-08-01 |
| 二开分支 | `release` |
| 二开 HEAD | `97ed8b4216fbb3f1c5caa8d4b6c73a83260c515a` |
| 官方分支 | `github官方仓库/main` |
| 官方 HEAD | `cfaba1dd6754d4238e1360247c198a64a313e96c` |
| 共同基线 | `8bc4bf1d6b1fd7d117100edadcf4257d3a4eb479` |
| 分叉提交数 | 二开侧 156，官方侧 119 |

统计口径：

- 功能识别以共同基线后的二开提交、当前路由、模型、服务和测试为主要证据。
- 不能把 `git diff 官方HEAD..release` 全部当作二开功能。官方分支在共同基线后也持续演进，其中包含认证、RelayKit 和前端目录结构变化。
- `release` 当前已将默认前端移动到 `web/`，同时删除 classic 前端。后续合并应使用当前上游目录契约，不恢复已经淘汰的旧目录。

每次同步前更新本节：

```powershell
git fetch "github官方仓库" main
git rev-parse release
git rev-parse "github官方仓库/main"
git merge-base release "github官方仓库/main"
git rev-list --left-right --count release..."github官方仓库/main"
git log --first-parent --oneline (git merge-base release "github官方仓库/main")..release
```

## 2. 合并原则

1. 非冲突改动使用 Git 自动合并结果。
2. 冲突段先保留二开分支的业务行为，不因上游重构直接删除现有能力。
3. 在二开行为之上补齐上游必须的新函数参数、认证链路、DTO 字段、数据库字段和返回契约。
4. 认证与数据契约不能只为通过编译而空实现。必须沿调用链验证 Router -> Middleware -> Controller -> Service -> Model。
5. 计费冲突必须同时验证请求边界、预扣费、结算/退款、日志审计和并发幂等。
6. 前端冲突以当前上游目录和状态管理契约为骨架，再迁移二开组件；不恢复已删除的 classic 前端。
7. 每个拆分 PR 只承载一个可独立回归的业务域，禁止夹带 `.env`、部署凭据、构建产物或无关格式化。

## 3. 总 PR 草案

### 变更描述 / Description

将当前二次开发能力从长期混合分支拆成可独立审查、测试和回滚的业务 PR。拆分后仍保持现有密钥额度、商务结算、运营统计、告警、VIP、视频代理、动态计费、内部对账接口和站点配置能力，并适配官方最新认证、RelayKit、前端目录和数据契约。

AI 辅助披露：本功能盘点和初始 PR 草案由 AI 辅助整理；提交者必须逐项核对代码行为、人工修订 PR 描述，并附真实测试证据。

### 变更类型 / Type of change

- [ ] Bug 修复 (Bug fix)
- [x] 新功能 (New feature)
- [x] 性能优化 / 重构 (Refactor)
- [x] 文档更新 (Documentation)

### 关联任务 / Related Issue

- 二开上游同步、回归基线和 PR 拆分。

### 提交前检查项 / Checklist

- [ ] 已按本文 PR 顺序拆分，单个 PR 不跨业务域。
- [ ] 已补齐官方最新认证、参数和数据契约。
- [ ] 已通过 Go 单测、前端测试、类型检查和生产构建。
- [ ] 已验证 SQLite、MySQL 和 PostgreSQL 迁移兼容性。
- [ ] 已验证计费无负数、溢出、重复退款和绕过路径。
- [ ] 已确认代码、日志、截图和提交中无敏感凭据。
- [ ] 已附接口响应、页面截图或关键测试日志作为运行证明。

### 运行证明 / Proof of Work

每个子 PR 单独附证据，不在总 PR 中使用“全量测试通过”替代业务回归。最低证据见第 6 节。

## 4. 建议 PR 拆分

| ID | 建议标题 | 依赖 | 风险 |
| --- | --- | --- | --- |
| PR-00 | `chore: establish custom baseline and remove deployment secrets` | 无 | 高 |
| PR-01 | `feat(auth): add business scope and settlement identity contracts` | PR-00 | 高 |
| PR-02 | `feat(keys): add multi-group keys and periodic quota limits` | PR-01 | 高 |
| PR-03 | `feat(billing): add tiered expression v2 task settlement` | PR-01 | 极高 |
| PR-04 | `feat(video): add Seedance, Byteplus and SD Real Max relay support` | PR-03 | 极高 |
| PR-05 | `fix(relay): preserve billing on disconnect and normalize media results` | PR-03 | 高 |
| PR-06 | `feat(operations): add business settlement and controlled online topup` | PR-01 | 高 |
| PR-07 | `feat(analytics): add user, inviter, token and ROI dashboards` | PR-01, PR-06 | 中高 |
| PR-08 | `feat(vip): add key-account statistics and notifications` | PR-07 | 中高 |
| PR-09 | `feat(metrics): add request performance analytics and alerts` | PR-01 | 中高 |
| PR-10 | `feat(alerting): add multi-platform error-log alert rules` | PR-01 | 中高 |
| PR-11 | `feat(integration): add internal reconciliation and channel OpenAPI` | PR-01, PR-07 | 高 |
| PR-12 | `feat(site): add custom menu pages, header navigation and SEO settings` | PR-00 | 中 |
| PR-13 | `chore(release): align frontend layout, images and deployment config` | PR-02 至 PR-12 | 高 |

### PR-00 基线与凭据治理

范围：

- 固定官方基线、二开 HEAD、差异统计和回归命令。
- 将环境配置改为 `.env.example`/部署系统注入，禁止真实 `.env` 进入提交。
- 记录数据库迁移、定时任务、Redis、对象存储和反向代理前置条件。

发布阻断：`97ed8b42` 将 `.env` 纳入版本历史。该文件不得进入任何上游或二开功能 PR；如果其中存在真实凭据，应立即轮换，而不是只从后续提交删除。

### PR-01 商务权限与结算身份契约

业务能力：

- 用户支持 `business_channel`、`settlement_currency`、`is_vip_customer`、`allow_online_topup`。
- 商务账号只能查看其直接邀请用户范围内的使用日志和统计。
- 用户分组权限贯穿渠道选择和可用分组展示。
- 结算币种支持 CNY/USD，并在配额、充值金额和密钥限额之间保持统一换算。

关键范围：`model/user.go`、`middleware/auth.go`、`controller/user.go`、`controller/settlement.go`、`service/group.go`、`model/log.go`。

必须保留的上游契约：最新会话认证、Token/User 双认证、管理员权限、用户缓存失效、请求上下文用户字段。

最低回归：普通用户、商务账号、管理员三种角色分别验证日志列表、日志统计、任务访问、分组可见性和越权拒绝。

### PR-02 API 密钥多分组与周期额度

业务能力：

- 单个 API 密钥支持多个分组。
- 密钥支持日额度、周额度以及对应使用量查询。
- 密钥列表默认脱敏，只有所有权验证后的专用接口返回完整密钥。
- 密钥统计支持汇总、Top、即将耗尽和每日趋势，并统一时区口径。
- 密钥页面展示余额和 API 端点信息。

关键范围：`model/token.go`、`model/token_quota_data.go`、`controller/token.go`、`controller/token_stats.go`、`middleware/distributor.go`、`web/src/features/keys/`、`web/src/features/dashboard/components/tokens/`。

数据契约：`daily_quota`、`weekly_quota`、多分组字段、周期使用量和密钥脱敏响应必须在创建、更新、查询和鉴权路径一致。

最低回归：`controller/token_test.go` 中的迁移、脱敏、所有权、日/周额度持久化与使用量测试；补充跨周边界、时区和多分组路由测试。

### PR-03 动态计费表达式 v2

业务能力：

- 保留 v1 按百万 token 计费，增加 v2 按次/按秒任务计费。
- 支持 `seconds`、`resolution`、`size`、`has_video`、`has_image`、`n`、`mode` 等任务变量。
- 预扣费冻结表达式、分组倍率、请求参数和估算档位；结算用上游实际结果重算差额。
- 视觉价格矩阵未匹配时失败闭合，保留预扣费，不允许零价退款。
- 所有额度转换使用集中式安全转换和饱和审计，不允许裸 `float -> int`。

关键范围：`pkg/billingexpr/`、`relay/helper/price.go`、`relay/channel/task/taskcommon/expr_vars.go`、`service/task_billing.go`、`service/task_tiered_settle.go`、`service/log_info_generate.go`、`web/src/features/system-settings/models/tiered-pricing-editor.tsx`。

不可拆开的安全链：参数上限 -> 表达式估算 -> 预扣费 -> 异步结算/退款 -> 日志 `quota_saturation` 审计。

最低回归：表达式边界、显式零值、分组倍率冻结、预扣大于/小于实扣、并发只结算一次、未匹配失败闭合、int32 饱和、订阅与钱包两种资金来源。

### PR-04 视频渠道与 Seedance V3

业务能力：

- 新增 Byteplus/SD Real Max 渠道和 Seedance 2.0/3.0 请求适配。
- 支持 `/api/v3/contents/generations/tasks` 任务创建/查询契约。
- 支持 `/api/v3/open/CreateAsset` 和 `/v3/open/GetAsset` 素材接口。
- 渠道可配置 `asset_base_url`、视频请求格式和三类上游路由覆盖。
- 视频结果代理同时支持 Dashboard 会话和 API Token，并按角色校验任务归属。
- Doubao 支持请求格式切换、自动时长、实际 token/时长/分辨率结算和退款。

关键范围：`dto/seedance_v3.go`、`dto/channel_settings.go`、`middleware/seedance_v3_*`、`controller/seedance_v3_asset.go`、`relay/channel/task/doubao/`、`relay/channel/task/sdrealmax/`、`router/video-router.go`、`web/src/features/channels/`。

最低回归：动态模型、素材主/备用 Base URL、路由覆盖、超长 duration 拒绝、自动 duration 保留、任务归属、视频内容代理、提交失败退款、轮询完成只结算一次。

### PR-05 Relay 中断与媒体结果

业务能力：

- 客户端主动断开时区分“上游尚未产生成本”和“上游已产生有效内容”，避免错误扣费或免费使用。
- 流式扫描识别内容帧和上游错误，保持不同协议的结束状态一致。
- 图像生成请求格式转换；结果可选择上传 S3 兼容对象存储并回写稳定 URL。
- 图片/视频结果 URL 在上游不可用或对象存储失败时保留原响应。
- 透传 `X-Client-Request-ID`，用于跨系统追踪。

关键范围：`relay/helper/client_abort.go`、`content_frame_detector.go`、`stream_scanner.go`、`upstream_stream_error.go`、`service/image_result_url.go`、`pkg/objectstore/`、OpenAI/Gemini/Claude relay 文件。

最低回归：断开前无内容、断开后已有内容、SSE 错误帧、base64 图片上传、远程 URL 跳过、对象存储不可用降级、显式零参数保留。

### PR-06 商务结算与在线充值控制

业务能力：

- 用户结算币种和全局汇率联动。
- 管理员可批量调整用户额度、设置商务渠道和开启在线充值。
- 未授权用户无法发起在线充值，但后台充值和既有账务不受影响。
- 钱包快捷金额按用户分组比例联动。
- 充值、消费、退款展示使用统一币种换算。

关键范围：`common/currency.go`、`controller/settlement.go`、`controller/topup*.go`、`model/user.go`、`setting/operation_setting/`、`web/src/features/users/`、`web/src/features/wallet/`。

最低回归：CNY/USD、零/异常汇率、禁止在线充值、管理员批量操作、各支付通道金额一致性、退款不重复换算。

### PR-07 运营、邀请、密钥与 ROI 统计

业务能力：

- 数据看板新增用户统计、充值趋势、消耗趋势、渠道分布、重点客户、邀请人和销售推广分析。
- 支持单日明细、时间对比、排序、筛选和导出。
- ROI 统计区分消费与退款，日志支持 `account_id` 回填和按账号/渠道聚合。
- 邀请用户统计按商务渠道和直接邀请关系归属。
- 密钥消耗快照支持即将耗尽分析。

关键范围：`controller/dashboard_user_stats*.go`、`controller/inviter_stats.go`、`model/inviter_stats.go`、`model/promotion_stats.go`、`model/token_quota_data.go`、`web/src/features/dashboard/`。

最低回归：时区边界、消费减退款净口径、空时间桶补零、商务范围、导出与列表一致、分页排序稳定、SQLite/MySQL/PostgreSQL 聚合结果一致。

### PR-08 重点客户/VIP 统计与通知

业务能力：

- 管理员批量标记重点客户。
- 重点客户页展示日/周消耗、请求数、token、充值和趋势。
- 定时生成小时/每日统计，支持手动 backfill。
- Telegram 定时报表和低余额通知，支持手动触发。
- 对外重点客户页面使用独立密码校验。

关键范围：`model/vip_*.go`、`controller/vip_stats.go`、`controller/tg_notify.go`、`service/vip_*`、`service/tg_*`、`web/src/features/users/vip-stats/`。

最低回归：定时任务幂等、跨实例只执行一次、历史回填、取消 VIP 后查询范围、低余额去重、密码失败限流、时区日切。

### PR-09 请求性能统计与告警

业务能力：

- 记录请求性能指标，提供总览、趋势、用户、平台、渠道、模型和失败原因分析。
- 普通用户只能查看自身数据，管理员可查看全局和分维度数据。
- 支持采样/清理配置、时间桶补齐和 TTFT 分位数展示。
- 支持请求告警规则、事件和测试通知。

关键范围：`model/request_metrics_log.go`、`controller/request_metrics.go`、`service/request_metrics_*`、`model/request_alert.go`、`service/request_alert_evaluator.go`、`setting/request_metrics_setting.go`、`web/src/features/dashboard/components/request-analytics/`。

最低回归：自身数据隔离、管理员权限、分类器、空桶、分位数、清理保留期、多实例定时任务和告警去重。

### PR-10 多平台错误日志告警

业务能力：

- 管理员维护错误日志告警规则、作用域、阈值、冷却时间和启停状态。
- 支持企业微信群机器人和 Telegram 多平台并行发送；单个平台失败不阻塞其他平台。
- 告警事件支持 ack 链接、冷却延期、用户/密钥查找和历史查询。
- 目标凭据在错误响应和日志中脱敏。

关键范围：`model/log_alert.go`、`controller/log_alert.go`、`service/log_alert_*`、`service/wecom_bot.go`、`web/src/features/usage-logs/components/dialogs/error-log-alert-*`。

最低回归：阈值窗口、冷却去重、leader 选举、平台部分失败、凭据脱敏、ack token、规则作用域和管理员权限。

### PR-11 内部对账接口与渠道 OpenAPI

业务能力：

- `/internal/logs/refund-status` 查询退款状态。
- `/internal/logs/patch-account` 回填日志 `account_id`。
- `/internal/logs/stat-by-accounts` 和 `/stat-by-channel` 为 ROI 对账提供聚合。
- `/openapi/channel` 使用静态 `OpenAPIToken` 创建或更新渠道。

关键范围：`router/internal-router.go`、`controller/internal_*.go`、`router/api-router.go`、`middleware/openapi_auth.go`、`setting/system_setting/system_setting_old.go`。

安全阻断：当前 `/internal/*` 没有应用层认证，且 `patch-account` 是写接口，与“只读”注释不一致。部署必须限制在内网/反向代理白名单，或者在此 PR 中增加内部 JWT、mTLS 或签名认证。该问题未解决前禁止公网暴露。

最低回归：错误 token、空 token、批量上限、重复回填、ClickHouse 不支持路径、时间范围、消费/退款类型、最小响应字段和公网拒绝策略。

### PR-12 自定义页面、导航与 SEO

业务能力：

- 管理员配置自定义菜单页、是否登录、用户/管理员可见性、iframe/新窗口和侧栏/全宽布局。
- 顶部导航和自定义页面使用同一配置源。
- 支持站点 meta description 和分析脚本设置。
- 前端根据系统配置动态更新 SEO 信息。

关键范围：`controller/custom_menu_pages.go`、`web/src/features/custom-menu-page/`、`web/src/features/system-settings/maintenance/`、`web/src/features/system-settings/site/seo-section.tsx`、`web/src/lib/site-seo.ts`、`web/src/routes/__root.tsx`。

最低回归：匿名/登录/管理员三种访问状态、外链协议校验、iframe 沙箱策略、配置解析降级、路由刷新和 SEO DOM 更新。

### PR-13 发布与目录收口

范围：

- 默认前端统一为 `web/`，清理 classic 和重复的 `web/default` 路径引用。
- Dockerfile、Compose、CI 和本地构建使用相同产物目录。
- 保留国内镜像属于部署策略，不与业务 PR 混合。
- 模型导入工具、数据修复脚本和迁移文档单独审查，不随服务启动隐式执行。

最低回归：`go test ./...`、前端测试、类型检查、生产构建、Docker 镜像构建、空数据库启动、已有数据库升级、服务健康检查和静态资源加载。

## 5. 功能清单

状态说明：`已实现` 表示当前 `release` 存在代码路径；不代表已适配最新官方 HEAD 或已完成本轮回归。

| 领域 | 功能 | 当前状态 | 归属 PR |
| --- | --- | --- | --- |
| 用户 | 商务渠道、结算币种、VIP、在线充值权限 | 已实现 | PR-01/06/08 |
| 权限 | 商务账号直接邀请用户日志范围 | 已实现 | PR-01 |
| 密钥 | 多分组 | 已实现 | PR-02 |
| 密钥 | 日/周额度与周期使用量 | 已实现 | PR-02 |
| 密钥 | 脱敏列表和按所有权查看完整密钥 | 已实现 | PR-02 |
| 密钥 | 汇总、Top、耗尽、每日趋势 | 已实现 | PR-02 |
| 计费 | v1 token 表达式 | 已实现 | PR-03 |
| 计费 | v2 视频/图片按次按秒表达式 | 已实现 | PR-03 |
| 计费 | 未匹配失败闭合和饱和审计 | 已实现 | PR-03 |
| 视频 | Byteplus/SD Real Max | 已实现 | PR-04 |
| 视频 | Seedance 任务和素材 API | 已实现 | PR-04 |
| 视频 | 自定义上游路由和请求格式 | 已实现 | PR-04 |
| 视频 | 按角色访问结果代理 | 已实现 | PR-04 |
| Relay | 客户端断开扣费保护 | 已实现 | PR-05 |
| Relay | 流式内容帧和错误检测 | 已实现 | PR-05 |
| 媒体 | 图片结果上传 S3 兼容存储 | 已实现 | PR-05 |
| 充值 | 用户在线充值开关 | 已实现 | PR-06 |
| 充值 | 分组快捷金额和汇率换算 | 已实现 | PR-06 |
| 统计 | 新用户、充值、消耗、渠道、邀请、销售 | 已实现 | PR-07 |
| 统计 | 单日明细、对比、导出 | 已实现 | PR-07 |
| 对账 | 日志 account_id 和 ROI 聚合 | 已实现 | PR-07/11 |
| VIP | 小时/每日统计、趋势、backfill | 已实现 | PR-08 |
| VIP | Telegram 报表和低余额通知 | 已实现 | PR-08 |
| 指标 | 请求性能、TTFT、错误分类 | 已实现 | PR-09 |
| 指标 | 请求告警规则和事件 | 已实现 | PR-09 |
| 告警 | 错误日志企业微信/Telegram 告警 | 已实现 | PR-10 |
| 告警 | 冷却、ack、多平台部分失败 | 已实现 | PR-10 |
| 集成 | 内部退款/账号/ROI 对账接口 | 已实现，需安全收口 | PR-11 |
| 集成 | OpenAPI Token 管理渠道 | 已实现 | PR-11 |
| 站点 | 自定义菜单页和导航 | 已实现 | PR-12 |
| 站点 | SEO 描述和分析脚本 | 已实现 | PR-12 |
| 发布 | 前端迁移到 `web/` | 已实现，需构建回归 | PR-13 |

## 6. 回归矩阵

| 测试层 | 每个 PR 最低要求 | 高风险 PR 附加要求 |
| --- | --- | --- |
| Go 单测 | 运行变更包及其调用方测试 | PR-03/04/05 运行 relay、service、billingexpr 全套测试 |
| 数据库 | SQLite 自动化迁移 | MySQL 5.7.8+、PostgreSQL 9.6+ 迁移和关键查询 |
| 前端 | `bun test`、`bun run typecheck` | 涉及路由/设置页时增加生产构建和页面流程截图 |
| API | 正常、缺参、显式零值、未授权 | 计费接口增加极值、重复请求、断开和并发结算 |
| 权限 | user/admin/root | 商务账号、API Token、会话认证、内部调用方 |
| 账务 | 预扣、实扣、退款、日志一致 | 钱包/订阅、消费/退款净口径、饱和审计 |
| 定时任务 | 单次执行和失败重试 | Redis 不可用、多实例 leader、重复执行幂等 |
| 发布 | 二进制启动和 `/api/status` | Docker/Compose、反向代理、静态资源、已有数据升级 |

推荐全量命令：

```powershell
go test ./...
Set-Location web
bun install --frozen-lockfile
bun test
bun run typecheck
bun run build
```

## 7. 上游合并冲突热点

以下区域发生冲突时不能简单整文件选一侧：

- 认证：`middleware/auth.go`、`controller/user.go`、`model/user.go`、`model/user_cache.go`。
- 数据契约：用户、Token、Task、Log、ChannelSettings DTO 和上下文键。
- 计费：`pkg/billingexpr/`、`relay/helper/price.go`、`service/task_billing.go`、`service/log_info_generate.go`。
- Relay：OpenAI/Claude/Gemini 转换、流式扫描和 RelayKit 迁移接口。
- 视频：`controller/task_video.go`、Doubao/SD Real Max adaptor、视频 Router。
- 前端：认证 store/API、系统设置、渠道表单、密钥、统计、日志和路由生成文件。
- i18n：所有 locale 必须由英文 key 同步生成，不能只保留某一个语言文件。

冲突完成后的最低检查：

```powershell
git diff --name-only --diff-filter=U
rg -n "^(<<<<<<<|=======|>>>>>>>)" .
gofmt -w <本次修改的 Go 文件>
go test ./...
Set-Location web
bun run i18n:sync
bun test
bun run typecheck
bun run build
```

## 8. 发布前阻断项

- [ ] `.env` 已从 PR 和发布制品源代码中排除；历史真实凭据已轮换。
- [ ] `/internal/*` 已通过应用认证或基础设施白名单隔离。
- [ ] 所有新增模型和迁移已验证 SQLite、MySQL、PostgreSQL。
- [ ] 计费字段已验证显式 `0`、缺省值、超大值和并发结算。
- [ ] 商务账号、普通用户、管理员不存在横向越权。
- [ ] Redis 不可用时，告警和定时任务有可接受的降级行为。
- [ ] 前端仅保留一个有效工作区和锁文件，Docker/CI 使用相同路径。
- [ ] OpenAPI、对象存储、Telegram、企业微信配置未出现在日志或前端响应中。
- [ ] 合并后的发布版本重新构建并部署，不能沿用旧二进制。

## 9. 维护方式

新增二开功能时必须同时更新：

1. 第 4 节对应 PR 的业务契约、关键范围和最低回归。
2. 第 5 节功能状态与归属 PR。
3. 第 6 节新增的高风险回归场景。
4. 第 7 节新增冲突热点。
5. 基线 HEAD、共同基线和分叉提交数。

功能被官方吸收后，将其状态改为“上游已覆盖”，记录首次覆盖的官方提交，并从二开 PR 中删除重复实现；不要继续维护两套同义代码。
