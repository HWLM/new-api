/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// 字段与 controller/distill_detect.go 的 DistillSettingView / DistillHitView 一一对应。
// 题库与独立 TG 通道见 docs/distill-detection-plan.md §14.3。

/** 题库中的一道题。id 是命中记录 categories 的键，已保存的题不改 id。 */
export type DistillQuestion = {
  /** [a-z0-9_]{1,32}，唯一 */
  id: string
  /** 展示用短名，1..64 */
  name: string
  /** 题面正文，1..8000 字符 */
  prompt: string
  enabled: boolean
}

export type DistillSetting = {
  enabled: boolean
  activate_rpm: number
  judge_interval_seconds: number
  confirm_hits: number
  confirm_window_seconds: number
  threshold: number
  demote_seconds: number
  observe_only: boolean
  jev_base_url: string
  /** 判定服务要求的 model 字段；空 = 后端按 jev-latest */
  jev_model: string
  /** 只回「是否已配置」，密钥本身永不回显。 */
  jev_api_key_set: boolean
  jev_timeout_ms: number
  max_state_chars: number
  head_sample_chars: number
  /** 题库，按判定顺序排列。 */
  questions: DistillQuestion[]
  alert_telegram: boolean
  alert_wecom: boolean
  /** 同一用户告警节流窗口（秒），0 = 不节流。 */
  alert_throttle_seconds: number
  /** 独立 TG bot token 只回「是否已配置」。 */
  tg_bot_token_set: boolean
  tg_chat_id: string
  wecom_webhook_url: string
  fallback_channel_id: number
  /** §16 检测渠道白名单；空 = 不检测。与 fallback_channel_id 互斥。 */
  detect_channel_ids: number[]
  dedup_seconds: number
}

/**
 * PUT 请求体：全部可选，只覆盖提交了的字段。
 * jev_api_key / tg_bot_token：省略或空串 = 不修改；'__clear__' = 清空。
 * questions：全量替换。
 */
export type DistillSettingUpdate = Partial<
  Omit<DistillSetting, 'jev_api_key_set' | 'tg_bot_token_set'>
> & {
  jev_api_key?: string
  tg_bot_token?: string
}

export const DISTILL_CLEAR_KEY = '__clear__'

export const DISTILL_QUESTION_LIMITS = {
  maxCount: 20,
  idPattern: /^[a-z0-9_]{1,32}$/,
  nameMax: 64,
  promptMax: 8000,
} as const

export type DistillQuestionResult = {
  id: string
  /** 写入时的题名快照 */
  name: string
  score: number
  /** 写入时是否 ≥ 当时的阈值 */
  hit: boolean
}

export type DistillHit = {
  id: number
  user_id: number
  username: string
  request_id: string
  model: string
  channel_id: number
  /** 实际重导到的渠道，0 = 未重导（观察模式恒为 0）。 */
  fallback_id: number
  score: number
  /** 逐题分数的 JSON 文本 {题目id: 分数}，可能为空串。 */
  categories: string
  /**
   * §18 写入时的逐题快照（题名、是否命中按当时阈值算好）。
   * 老记录可能为 null，后端会用 categories + 当前题库名兜底。
   */
  question_results?: DistillQuestionResult[] | null
  doc_chars: number
  /** RFC3339 */
  sampled_at: string
  /** RFC3339；空串 = 未降级。 */
  demote_until: string
  alert_sent: boolean
  /**
   * §19：去重窗口内同一正文、上次判定为命中时，不再调判定服务，
   * 复用上次分数并计一次确认命中。老后端没有此字段时视为 false。
   */
  dedup_reused?: boolean
}

export type DistillHitsPage = {
  items: DistillHit[]
  total: number
  page: number
}

export type DistillHitsQuery = {
  p: number
  page_size: number
  user_id?: string
  request_id?: string
  /** RFC3339 */
  from?: string
  /** RFC3339 */
  to?: string
}

export type DistillTestResult = {
  hit: boolean
  score: number
  categories: string
  doc_chars: number
  /** 非空 = 没有真的调用判定服务，值是原因。 */
  skipped: string
  observe_only: boolean
}

/**
 * 单个告警通道的测试结果，对应 service.DistillAlertChannelResult。
 * status：sent / disabled（开关没开）/ not_configured（开了但没填）/ failed（带 error）。
 */
export type DistillAlertChannelResult = {
  status: 'sent' | 'disabled' | 'not_configured' | 'failed' | (string & {})
  error?: string
}

export type DistillAlertTestResult = {
  telegram?: DistillAlertChannelResult
  wecom?: DistillAlertChannelResult
}

export type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

/** §20 POST /api/distill_detect/clear_cache 请求体。 */
export type DistillClearCacheRequest =
  | { scope: 'all' }
  | { scope: 'user'; user_id: number }

/** §20 清空结果：只清运行态，不删命中记录。 */
export type DistillClearCacheResult = {
  /** 实际删除 / 清理的键数 */
  cleared: number
  /** 本次清理所用的后端 */
  backend: 'redis' | 'memory' | (string & {})
  /** 被解除降级的用户数（字段名按 §20.2 契约） */
  upgraded_users: number
}

/**
 * Jev 判定服务调用记录（管理员专用，GET /api/distill_detect/jev_calls）。
 * 字段对应 controller/distill_jev_calls.go 的 DistillJevCallView。
 */
export type DistillJevOutcome =
  | 'success'
  | 'http_error'
  | 'timeout'
  | 'network_error'
  | 'parse_error'

export const DISTILL_JEV_OUTCOMES: DistillJevOutcome[] = [
  'success',
  'http_error',
  'timeout',
  'network_error',
  'parse_error',
]

export type DistillJevCall = {
  id: number
  /** 被检测的用户 */
  user_id: number
  username?: string
  request_id: string
  model: string
  channel_id: number
  /** 调用时该用户当前分钟的请求数 */
  rpm: number
  outcome: DistillJevOutcome | (string & {})
  /** 没拿到响应（超时 / 网络错误）时为 0 */
  http_status: number
  /** 只有 success 才有值 */
  score: number | null
  /** 当时的命中阈值 */
  threshold: number
  duration_ms: number
  /** 失败时的错误摘要 */
  error: string
  /** RFC3339 */
  created_at: string
}

export type DistillJevCallsSummary = {
  total: number
  success: number
  failed: number
  avg_score: number | null
  avg_duration_ms: number | null
}

export type DistillJevCallsPage = {
  items: DistillJevCall[]
  total: number
  page: number
  summary: DistillJevCallsSummary
}

export type DistillJevCallsQuery = {
  p: number
  page_size: number
  user_id?: string
  outcome?: string
  /** RFC3339 */
  from?: string
  /** RFC3339 */
  to?: string
}
