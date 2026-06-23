/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/

// 后端 controller/dashboard_user_stats.go 的响应类型对应
// 单位约定：
//   consumed_usd / remaining_usd 都是 USD（quota / QuotaPerUnit）
//   recharge_cny 是人民币 ¥（管理员页面录入即是 ¥）

export type CardSection = {
  total_users: number
  today_active_users: number
  total_recharge_cny: number
  total_consumed_usd: number
  today_recharge_cny: number
  today_recharge_cny_delta: number | null
  today_consumed_usd: number
  today_consumed_usd_delta: number | null
  total_remaining_usd: number
}

export type UserStatsCards = {
  all: CardSection
  official: CardSection
}

export type TopUserRow = {
  user_id: number
  username: string
  consumed_usd: number
}

export type RechargeTrendPoint = {
  date: string // YYYY-MM-DD
  recharge_cny: number
}

export type ChannelPieRow = {
  channel: string
  consumed_usd: number
  percent: number
}

// 筛选区参数（API query params）
//
// channel / sales 走多选 —— 数组前端 join(',') 拼成单个 query value，
// 后端 splitCSV 解析回切片，避免 axios 对数组序列化的版本差异。
export type StatsFilter = {
  start_date?: string
  end_date?: string
  username?: string
  channel?: string[]
  sales?: string[]
  is_vip?: '' | '0' | '1' // 空串表示不过滤
}

export type FilterOptions = {
  channels: string[]
  sales: string[]
}

export type TrendGranularity = 'day' | 'hour'

export type ConsumptionTrendResp = {
  granularity: TrendGranularity
  buckets: string[] // day → "YYYY-MM-DD"；hour → "YYYY-MM-DD#HH"
  values: number[] // 与 buckets 一一对应的 USD 消耗
  has_compare?: boolean
  compare_buckets?: string[]
  compare_values?: number[]
  current_total?: number
  compare_total?: number
  diff?: number
  change_rate?: number // %, compare_total=0 时为 0
}

// 对比时间窗口（前端推算 + 传给后端）
export type CompareWindow = {
  start_date: string // YYYY-MM-DD
  end_date: string
  start_hour?: number // 仅"今天"模式下用 0..23
  end_hour?: number
}

// 明细数据表
export type DetailsFilter = {
  username?: string
  channel?: string[]      // 归属渠道（inviter.business_channel）
  sales?: string[]        // 归属销售（inviter.display_name）
  user_group?: string[]   // users.group
  is_vip?: '' | '0' | '1'
  last_consume_date_from?: string // YYYY-MM-DD
  page?: number
  page_size?: number
  sort_by?: string
  sort_dir?: 'asc' | 'desc'
}

export type DetailsRow = {
  user_id: number
  username: string
  display_name: string
  is_vip_customer: boolean
  is_official: boolean
  business_channel: string
  inviter_display_name: string
  last_consume_at: number // unix 秒；0 = 无
  last_recharge_at: number
  total_requests: number
  total_tokens: number
  total_recharge_cny: number
  total_consumed_usd: number
  remaining_usd: number
}

export type DetailsResp = {
  rows: DetailsRow[]
  total: number
  page: number
  page_size: number
}

// 「按天统计」filter / row / resp
export type DetailsDailyFilter = {
  start_date: string // 必填
  end_date: string // 必填
  username?: string
  channel?: string[]
  sales?: string[]
  user_group?: string[]
  is_vip?: '' | '0' | '1'
  page?: number
  page_size?: number
  sort_by?: string
  sort_dir?: 'asc' | 'desc'
}

export type DetailsDailyRow = {
  date: string // YYYY-MM-DD
  user_id: number
  username: string
  display_name: string
  is_vip_customer: boolean
  is_official: boolean
  business_channel: string
  inviter_display_name: string
  daily_requests: number
  daily_consumed_usd: number
  daily_tokens: number
}

export type DetailsDailyResp = {
  rows: DetailsDailyRow[]
  total: number
  page: number
  page_size: number
}

// 单用户趋势对比
export type UserTrendParams = {
  user_id: number
  granularity: TrendGranularity
  current_start: string
  current_end: string
  compare_start: string
  compare_end: string
  current_start_hour?: number
  current_end_hour?: number
  compare_start_hour?: number
  compare_end_hour?: number
}

// 复用 vip-stats 的 trend 结构（结构相同，因为后端 model 共享）
export type TrendSeries = {
  buckets: string[]
  values: number[]
  total: number
}

export type UserTrendResp = {
  granularity: TrendGranularity
  current: TrendSeries
  compare: TrendSeries
  current_total: number
  compare_total: number
  diff: number
  change_rate: number
}
