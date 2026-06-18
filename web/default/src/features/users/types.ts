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
import { z } from 'zod'

// ============================================================================
// User Schema & Types
// ============================================================================

/** User status: 1 = enabled, 2 = disabled, 3+ = other states */
export const userStatusSchema = z.number()
export type UserStatus = z.infer<typeof userStatusSchema>

/** User role: 1 = common user, 10 = admin, 100 = root */
export const userRoleSchema = z.number()
export type UserRole = z.infer<typeof userRoleSchema>

export const userSchema = z.object({
  id: z.number(),
  username: z.string(),
  display_name: z.string(),
  password: z.string().optional(),
  github_id: z.string().optional(),
  oidc_id: z.string().optional(),
  wechat_id: z.string().optional(),
  telegram_id: z.string().optional(),
  email: z.string().optional(),
  quota: z.number(),
  used_quota: z.number(),
  request_count: z.number(),
  group: z.string(),
  aff_code: z.string().optional(),
  aff_count: z.number().optional(),
  aff_quota: z.number().optional(),
  aff_history_quota: z.number().optional(),
  inviter_id: z.number().optional(),
  inviter_username: z.string().optional(),
  linux_do_id: z.string().optional(),
  status: userStatusSchema,
  role: userRoleSchema,
  created_at: z.number().optional(),
  updated_at: z.number().optional(),
  last_login_at: z.number().optional(),
  DeletedAt: z.any().nullable().optional(),
  remark: z.string().optional(),
  is_vip_customer: z.boolean().optional(),
  business_channel: z.string().optional(),
  /** 仅 GET /api/user/:id 返回：用户所在分组对应的充值比例，调整额度弹窗回显使用 */
  topup_group_ratio: z.number().optional(),
})
export type User = z.infer<typeof userSchema>

export const userListSchema = z.array(userSchema)

// ============================================================================
// API Request/Response Types
// ============================================================================

/** Generic API response */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetUsersParams {
  p?: number
  page_size?: number
}

export interface GetUsersResponse {
  success: boolean
  message?: string
  data?: {
    items: User[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchUsersParams {
  keyword?: string
  group?: string
  role?: string
  status?: string
  /** '' = 不筛选，'true' = 仅重点客户，'false' = 仅非重点客户 */
  is_vip?: string
  /** unix 秒，0/undefined = 不筛选 */
  created_at_start?: number
  /** unix 秒，0/undefined = 不筛选 */
  created_at_end?: number
  p?: number
  page_size?: number
}

export interface UserFormData {
  username: string
  display_name: string
  password?: string
  role?: number // Only used when creating user
  quota?: number // Only used when updating user
  group?: string // Only used when updating user
  remark?: string // Only used when updating user
  /** 邀请人用户名；空串表示清除邀请人。后端反查 → inviter_id */
  inviter_username?: string
}

export type ManageUserAction =
  | 'promote'
  | 'demote'
  | 'enable'
  | 'disable'
  | 'delete'
  | 'add_quota'

export type QuotaAdjustMode = 'add' | 'subtract' | 'override'

/** add 模式下的额度来源类型 */
export type QuotaType = '充值' | '赠送'

export interface ManageUserQuotaPayload {
  id: number
  action: 'add_quota'
  mode: QuotaAdjustMode
  /** 仅 subtract / override 模式使用：直接 quota 单位的数值 */
  value?: number
  /** 仅 add 模式使用 */
  quota_type?: QuotaType
  /** 仅 add 模式使用：充值金额（USD） */
  recharge_amount?: number
  /** 仅 add 模式使用：充值比例 */
  ratio?: number
}

// ============================================================================
// TG Notification Settings
// ============================================================================

export interface TgNotifySettings {
  bot_token: string
  chat_id: string
}

// ============================================================================
// VIP Stats Detail
// ============================================================================

export interface VipStatsSummary {
  user_count: number
  today_consumed: number
  weekly_consumed: number
  current_remaining: number
  today_requests: number
  today_tokens: number
  weekly_requests: number
  weekly_tokens: number
  /** 较昨日同时段（昨天 00:00 ~ now-24h） */
  yesterday_consumed: number
  yesterday_requests: number
  yesterday_tokens: number
  /** 较上周期（[today-14, today-8] 这 7 个整天） */
  prev_week_consumed: number
  prev_week_requests: number
  prev_week_tokens: number
}

export interface VipStatsRow {
  user_id: number
  username: string
  /** 客户简称，来自 users.display_name */
  display_name: string
  remaining: number
  daily: number[]
  daily_requests: number[]
  daily_tokens: number[]
  /** 归属邀请人：当前 VIP 客户邀请人的 username；无邀请人则为空 */
  inviter_username: string
  /** 归属渠道：当前 VIP 客户邀请人的 business_channel；无邀请人/邀请人非商务账号则为空 */
  inviter_business_channel: string
}

export interface VipStatsDetail {
  summary: VipStatsSummary
  dates: string[]
  rows: VipStatsRow[]
  totals: number[]
  total_requests: number[]
  total_tokens: number[]
}

// ============================================================================
// VIP Stats Trend (单用户 两周期对比)
// ============================================================================

export type TrendMode = 'daily' | 'weekly' | 'monthly' | 'custom'
export type TrendGranularity = 'day' | 'hour'

export interface TrendSeries {
  /** x 轴 label：day → "MM-DD"；hour → "MM-DD HH:00" */
  buckets: string[]
  /** 对应 quota 数值（前端转 $ 显示） */
  values: number[]
  total: number
}

export interface VipStatsTrend {
  granularity: TrendGranularity
  current: TrendSeries
  compare: TrendSeries
  current_total: number
  compare_total: number
  diff: number
  /** 已乘 100，单位 %；compare_total=0 时返回 0 */
  change_rate: number
}

/** trend 接口请求参数 */
export interface VipStatsTrendParams {
  user_id: number
  granularity: TrendGranularity
  current_start: string // YYYY-MM-DD
  current_end: string
  compare_start: string
  compare_end: string
  /** 仅 granularity=hour 时使用；day 时省略走默认 0/23 */
  current_start_hour?: number
  current_end_hour?: number
  compare_start_hour?: number
  compare_end_hour?: number
}

// ============================================================================
// Dialog Types
// ============================================================================

export type UsersDialogType = 'create' | 'update' | 'delete' | 'tg-settings'
