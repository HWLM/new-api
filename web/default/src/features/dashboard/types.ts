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
import type { TimeGranularity } from '@/lib/time'

// ============================================================================
// Quota & Usage Data Types
// ============================================================================

export interface QuotaDataItem {
  id?: number
  user_id?: number
  username?: string
  model_name?: string
  created_at: number
  token_used?: number
  count?: number
  quota?: number
}

// ============================================================================
// Inviter Statistics Types
// ============================================================================

export interface InviterStatCards {
  invited_count: number
  total_consumed: number // quota 单位
  today_active_users: number
  today_consumed: number // quota 单位
}

export interface InviterChartUserSpend {
  username: string
  quota: number
}

export interface InviterChartDayPoint {
  date: string // YYYY-MM-DD
  quota: number
  requests: number
}

export interface InviterCharts {
  top_users: InviterChartUserSpend[]
  daily: InviterChartDayPoint[]
}

export interface InviterSummaryRow {
  user_id: number
  username: string
  created_at: number
  last_consumed_at: number // 0 = never
  total_requests: number
  total_consumed: number
  total_tokens: number
  current_remaining: number
}

export interface InviterDailyRow {
  date: string // YYYY-MM-DD
  username: string
  total_requests: number
  total_consumed: number
  total_tokens: number
}

// ============================================================================
// Promotion Statistics Types
// ============================================================================

export interface ChannelPromotionRow {
  channel: string
  invited_count: number
  total_consumed: number // quota 内部单位，前端转 USD
}

export interface SalesPromotionRow {
  username: string
  channel: string
  invited_count: number
  total_consumed: number
}

export interface PromotionStats {
  channels: ChannelPromotionRow[]
  sales: SalesPromotionRow[]
}

// ============================================================================
// Uptime Monitoring Types
// ============================================================================

export interface UptimeMonitor {
  name: string
  uptime: number
  status: number
  group?: string
}

export interface UptimeGroupResult {
  categoryName: string
  monitors: UptimeMonitor[]
}

// ============================================================================
// Dashboard Filter Types
// ============================================================================

export interface DashboardFilters {
  start_timestamp?: Date
  end_timestamp?: Date
  time_granularity?: TimeGranularity
  username?: string
}

export type ConsumptionDistributionChartType = 'bar' | 'area'

export type ModelAnalyticsChartTab = 'trend' | 'proportion' | 'top'

export interface DashboardChartPreferences {
  consumptionDistributionChart: ConsumptionDistributionChartType
  modelAnalyticsChart: ModelAnalyticsChartTab
  defaultTimeRangeDays: number
  defaultTimeGranularity: TimeGranularity
}

// ============================================================================
// API Info Types
// ============================================================================

export interface ApiInfoItem {
  url: string
  route: string
  description: string
  color: string
}

export interface PingStatus {
  latency: number | null
  testing: boolean
  error: boolean
}

export type PingStatusMap = Record<string, PingStatus>

// ============================================================================
// Chart Types
// ============================================================================

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type VChartSpec = Record<string, any>

export interface ProcessedChartData {
  spec_pie: VChartSpec
  spec_line: VChartSpec
  spec_area: VChartSpec
  spec_model_line: VChartSpec
  spec_rank_bar: VChartSpec
  totalQuotaDisplay: string
  totalCountDisplay: string
}

export interface ProcessedUserChartData {
  spec_user_rank: VChartSpec
  spec_user_trend: VChartSpec
}

// ============================================================================
// Token Statistics Types
// ============================================================================

export interface TokenStatsSummary {
  enabled_count: number
  remain_total: number
  today_quota: number
  yesterday_quota: number
  last30_quota: number
}

export interface TokenStatsTopItem {
  token_id: number
  token_name: string
  quota: number
}

export interface TokenExhaustingItem {
  id: number
  user_id: number
  snapshot_at: number
  token_id: number
  token_name: string
  token_key: string
  group_name: string
  used_quota: number
  remain_quota: number
  remain_ratio: number
}

export interface TokenDailyDetailItem {
  date: number
  token_id: number
  token_name: string
  token_key: string
  group_name: string
  daily_quota: number
  cumulative_quota: number
}

export interface TokenDailyDetailFilters {
  start_date?: number
  end_date?: number
  group?: string
  status?: number
  token_name?: string
  page?: number
  page_size?: number
  sort_by?: 'date' | 'daily_quota' | 'cumulative_quota'
  sort_order?: 'asc' | 'desc'
}

// ============================================================================
// Announcement Types
// ============================================================================

export interface AnnouncementItem {
  id?: number
  content: string
  publishDate?: string
  type?: 'default' | 'ongoing' | 'success' | 'warning' | 'error'
  extra?: string
}

// ============================================================================
// FAQ Types
// ============================================================================

export interface FAQItem {
  id?: number
  question: string
  answer: string
}
