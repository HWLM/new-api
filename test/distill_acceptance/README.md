# 蒸馏检测验收用例（黑盒 + 打桩 Jev）

对应 `docs/distill-detection-plan.md` §3/§4/§5/§8/§10。打桩部分全部可自动跑；不需要真 Jev。

## 前置

被测实例（隔离环境）需要：

- 一个管理员令牌：`DISTILL_ADMIN_TOKEN`
- 一条能用的普通用户 Key + 模型名：`DISTILL_RELAY_KEY` / `DISTILL_MODEL`
- 该 Key 所属用户 id：`DISTILL_USER_ID`（T7 要用）
- 本机可监听 `DISTILL_JEV_PORT`（默认 18099，企微 stub 用 18100）

```bash
export DISTILL_BASE_URL=http://127.0.0.1:3100
export DISTILL_ADMIN_TOKEN=...
export DISTILL_RELAY_KEY=sk-...
export DISTILL_MODEL=claude-sonnet-4-5
export DISTILL_USER_ID=123
python3 distill_acceptance.py            # 全跑
python3 distill_acceptance.py --case T2,T5
python3 distill_acceptance.py --jev-error     # 只跑判定失败那组（T8）
```

脚本会在开始时快照 `/setting` 并修改配置做实验，结束时还原（`--keep-setting` 可保留现场）。

## 用例

| 编号 | 断言 | 做法 |
|---|---|---|
| T1 | GET 字段齐全 / PUT 回读一致 / 越界被拒 | 改 `activate_rpm=7` 回读；再提交 `-1` 期望非 200 |
| T2 | 30 秒桶每分钟最多判 2 次；正文截断 ≤4000 字符；桶过期后再判 | 20 秒内发 8 条，假 Jev 计数 ≤2；等 31 秒再发 1 条，计数 +1 |
| T3 | 前 3 次命中只写 hits，第 4 次才降级 | `judge_interval=1` 加速；每次换正文绕开 dedup；逐次读 hits 的 `demote_until` |
| T4 | `observe_only=true` 绝不重导 | 命中后断言 hits 里 `fallback_id=0`、请求仍 200 |
| T5 | 无兜底渠道时 429 + `distill_fallback_unavailable` | `observe_only=false`、`fallback_channel_id=0`、`confirm_hits=1` |
| T6 | 告警按用户 600 秒节流 | 企微 webhook 指向本地 stub；两次降级只应收到 1 条 |
| T7 | 用户三态关闭后不再判定 | 把 `distill_detect_override` 置 -1，发 3 条，假 Jev 调用应为 0 |
| T8 | 判定失败按未命中放行，且不写 dedup 键 | 假 Jev 切 500 / 超时两种模式，各发同一正文两次：请求都要 200、不写 hits、第二次仍要调 Jev（=2 次调用，证明 dedup 没被污染） |

## T6 为什么只能用间接证据

“同一窗口内第二次降级不再告警”**没有做端到端投递验证**，报告里的 PASS 只是节流窗口生效的间接证据，别当成“告警投递验过了”。

原因：后端对 webhook 做域名强校验，只接受 `qyapi.weixin.qq.com`，本地/内网 stub 在配置阶段就被拒绝，请求根本发不出去。所以无法在本机观察“第二次没发第二条”。

采用的替代方案（脚本内 T6 实际做法）：

1. 配一个合法企微 webhook 形状的值，触发降级，读 Redis `distill:alert:{user:<id>}` 键，**TTL=532s** —— 落在 `alert_throttle_seconds=600` 窗口内，证明节流键被写下且带正确过期时间。
2. 等降级过期（62s）再命中一次，键仍在窗口内 → 走 `SETNX` 抢占失败分支，即“不发第二条”的分支被走到。

为什么可接受：告警发送本身是既有功能（拒答降级一直在用同一条企微通道），本次改动只加了“按用户 `SETNX` 节流”这一层，节流判定完全由 Redis 键的抢占结果决定，键的存在与 TTL 已经唯一决定了“会不会发”。剩余未覆盖面仅是“抢占失败时的那行 `return` 之后没有其它发送路径”，属静态可读（`service/distill_detect.go`）。

要补真正的端到端投递，需要三选一：给测试构建开“允许任意 webhook”的开关；DNS/TLS 劫持 `qyapi.weixin.qq.com` 到本地 stub；或抓生产告警的实际出网流量。

## 前端联调核对（隔离实例 31048）

只证明页面与接口接得上，真实点击体验由用户确认。

| 项 | 结果 |
|---|---|
| 前端产物 | `/static/js/index.b095bf8f65.js`（3,371,417 B）含 `蒸馏检测`×42、`distill-hits`×4、`activate_rpm`×5、`alert_throttle_seconds`×5、`distill_fallback_unavailable`×15，为新代码构建 |
| SPA 路由 | `/distill-hits`、`/console/distill-hits` → 200 `text/html` 1166 B，回 index.html（history fallback 正常） |
| 设置卡片 GET | `GET /api/distill_detect/setting` 200，`success:true`，23 个字段 |
| 设置卡片 PUT | `activate_rpm` 1→21 PUT 200，回读 21；再还原 1，回读 1 |
| 命中记录页 | `GET /api/distill_detect/hits?p=1&page_size=3` 200，返回真实命中行（id=24 起） |

## 已知限制

- T3/T6 用 `judge_interval_seconds=1`、`confirm_hits=1` 加速，验的是“计数与降级时机”逻辑；线上默认 30s/4 次的行为由 T2 的桶计数覆盖。
- T7 需要管理员 `PUT /api/user/` 全量更新；若该接口在隔离环境里被限制，脚本会打 SKIP 并提示手工在用户编辑里点「关闭」后重跑。
- 假 Jev 支持三种模式：`ok`（回固定分数）、`error`（HTTP 500）、`timeout`（sleep 6s 越过 `jev_timeout_ms`），T8 用后两种。
