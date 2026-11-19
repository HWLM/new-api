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
  /** 只回「是否已配置」，密钥本身永不回显。 */
  jev_api_key_set: boolean
  jev_timeout_ms: number
  max_state_chars: number
  head_sample_chars: number
  question_corpus_farm: boolean
  question_reward_rl: boolean
  question_judge_eval: boolean
  question_distill_target: boolean
  alert_telegram: boolean
  alert_wecom: boolean
  /** 同一用户告警节流窗口（秒），0 = 不节流。 */
  alert_throttle_seconds: number
  wecom_webhook_url: string
  fallback_channel_id: number
  dedup_seconds: number
}

/**
 * PUT 请求体：全部可选，只覆盖提交了的字段。
 * jev_api_key：省略或空串 = 不修改；'__clear__' = 清空。
 */
export type DistillSettingUpdate = Partial<
  Omit<DistillSetting, 'jev_api_key_set'>
> & {
  jev_api_key?: string
}

export const DISTILL_CLEAR_KEY = '__clear__'

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
  /** 逐题分数的 JSON 文本，可能为空串。 */
  categories: string
  doc_chars: number
  /** RFC3339 */
  sampled_at: string
  /** RFC3339；空串 = 未降级。 */
  demote_until: string
  alert_sent: boolean
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

export type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}
