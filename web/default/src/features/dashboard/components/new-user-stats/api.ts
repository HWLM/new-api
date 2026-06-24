/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'
import type {
  ChannelPieRow,
  ConsumptionTrendResp,
  DetailsDailyFilter,
  DetailsDailyResp,
  DetailsFilter,
  DetailsResp,
  DetailsSingleDayFilter,
  DetailsSingleDayResp,
  FilterOptions,
  RechargeTrendPoint,
  StatsFilter,
  TopUserRow,
  TrendGranularity,
  UserStatsCards,
  UserTrendParams,
  UserTrendResp,
} from './types'

type ApiResp<T> = { success: boolean; message?: string; data: T }

// 把 filter 转成 query params，去掉空值。
// channel / sales 多选 → join(',') 拼成单 value，后端 splitCSV 还原数组。
function toParams(f: StatsFilter): Record<string, string> {
  const p: Record<string, string> = {}
  if (f.start_date) p.start_date = f.start_date
  if (f.end_date) p.end_date = f.end_date
  if (f.username) p.username = f.username
  if (f.channel && f.channel.length > 0) p.channel = f.channel.join(',')
  if (f.sales && f.sales.length > 0) p.sales = f.sales.join(',')
  if (f.is_vip) p.is_vip = f.is_vip
  return p
}

export async function fetchUserStatsFilterOptions(): Promise<FilterOptions> {
  const res = await api.get<{ success: boolean; data: FilterOptions }>(
    '/api/user_stats/filter_options'
  )
  return res.data.data
}

export async function fetchUserStatsCards(): Promise<UserStatsCards> {
  const res = await api.get<ApiResp<UserStatsCards>>('/api/user_stats/cards')
  return res.data.data
}

export async function fetchTopUsers(filter: StatsFilter): Promise<TopUserRow[]> {
  const res = await api.get<ApiResp<TopUserRow[]>>('/api/user_stats/top_users', {
    params: toParams(filter),
  })
  return res.data.data
}

export async function fetchRechargeTrend(
  filter: StatsFilter
): Promise<RechargeTrendPoint[]> {
  const res = await api.get<ApiResp<RechargeTrendPoint[]>>(
    '/api/user_stats/recharge_trend',
    { params: toParams(filter) }
  )
  return res.data.data
}

export async function fetchChannelPie(
  filter: StatsFilter
): Promise<ChannelPieRow[]> {
  const res = await api.get<ApiResp<ChannelPieRow[]>>(
    '/api/user_stats/channel_pie',
    { params: toParams(filter) }
  )
  return res.data.data
}

export async function fetchConsumptionTrend(
  filter: StatsFilter,
  granularity: TrendGranularity,
  options?: {
    compare?: { start_date: string; end_date: string }
    start_hour?: number
    end_hour?: number
  }
): Promise<ConsumptionTrendResp> {
  const params: Record<string, string> = { ...toParams(filter), granularity }
  if (options?.compare) {
    params.compare_start_date = options.compare.start_date
    params.compare_end_date = options.compare.end_date
  }
  if (typeof options?.start_hour === 'number') {
    params.start_hour = String(options.start_hour)
  }
  if (typeof options?.end_hour === 'number') {
    params.end_hour = String(options.end_hour)
  }
  const res = await api.get<ApiResp<ConsumptionTrendResp>>(
    '/api/user_stats/consumption_trend',
    { params }
  )
  return res.data.data
}

function toDetailsParams(f: DetailsFilter): Record<string, string> {
  const p: Record<string, string> = {}
  if (f.username) p.username = f.username
  if (f.channel && f.channel.length) p.channel = f.channel.join(',')
  if (f.sales && f.sales.length) p.sales = f.sales.join(',')
  if (f.user_group && f.user_group.length) p.user_group = f.user_group.join(',')
  if (f.is_vip) p.is_vip = f.is_vip
  if (f.last_consume_date_from)
    p.last_consume_date_from = f.last_consume_date_from
  if (f.page) p.page = String(f.page)
  if (f.page_size) p.page_size = String(f.page_size)
  if (f.sort_by) p.sort_by = f.sort_by
  if (f.sort_dir) p.sort_dir = f.sort_dir
  return p
}

export async function fetchUserStatsDetails(
  filter: DetailsFilter
): Promise<DetailsResp> {
  const res = await api.get<ApiResp<DetailsResp>>('/api/user_stats/details', {
    params: toDetailsParams(filter),
  })
  return res.data.data
}

function toDetailsDailyParams(f: DetailsDailyFilter): Record<string, string> {
  const p: Record<string, string> = {
    start_date: f.start_date,
    end_date: f.end_date,
  }
  if (f.username) p.username = f.username
  if (f.channel && f.channel.length) p.channel = f.channel.join(',')
  if (f.sales && f.sales.length) p.sales = f.sales.join(',')
  if (f.user_group && f.user_group.length) p.user_group = f.user_group.join(',')
  if (f.is_vip) p.is_vip = f.is_vip
  if (f.page) p.page = String(f.page)
  if (f.page_size) p.page_size = String(f.page_size)
  if (f.sort_by) p.sort_by = f.sort_by
  if (f.sort_dir) p.sort_dir = f.sort_dir
  return p
}

export async function fetchUserStatsDetailsDaily(
  filter: DetailsDailyFilter
): Promise<DetailsDailyResp> {
  const res = await api.get<ApiResp<DetailsDailyResp>>(
    '/api/user_stats/details_daily',
    { params: toDetailsDailyParams(filter) }
  )
  return res.data.data
}

function toDetailsSingleDayParams(
  f: DetailsSingleDayFilter
): Record<string, string> {
  const p: Record<string, string> = { date: f.date }
  if (f.username) p.username = f.username
  if (f.channel && f.channel.length) p.channel = f.channel.join(',')
  if (f.sales && f.sales.length) p.sales = f.sales.join(',')
  if (f.user_group && f.user_group.length) p.user_group = f.user_group.join(',')
  if (f.is_vip) p.is_vip = f.is_vip
  if (f.page) p.page = String(f.page)
  if (f.page_size) p.page_size = String(f.page_size)
  return p
}

export async function fetchUserStatsDetailsSingleDay(
  filter: DetailsSingleDayFilter
): Promise<DetailsSingleDayResp> {
  const res = await api.get<ApiResp<DetailsSingleDayResp>>(
    '/api/user_stats/details_singleday',
    { params: toDetailsSingleDayParams(filter) }
  )
  return res.data.data
}

// 导出当日统计 Excel —— 后端返回二进制流，绕过 axios 默认的 JSON 解析。
// 同时跳过业务错误处理，因为响应不是 ApiResp 格式。
export async function exportUserStatsDetailsSingleDay(
  filter: DetailsSingleDayFilter
): Promise<Blob> {
  const res = await api.get<Blob>(
    '/api/user_stats/details_singleday/export',
    {
      params: toDetailsSingleDayParams(filter),
      responseType: 'blob',
      skipBusinessError: true,
    }
  )
  return res.data
}

export async function fetchUserStatsUserTrend(
  params: UserTrendParams
): Promise<ApiResp<UserTrendResp>> {
  const qs: Record<string, string> = {
    user_id: String(params.user_id),
    granularity: params.granularity,
    current_start: params.current_start,
    current_end: params.current_end,
    compare_start: params.compare_start,
    compare_end: params.compare_end,
  }
  if (params.granularity === 'hour') {
    qs.current_start_hour = String(params.current_start_hour ?? 0)
    qs.current_end_hour = String(params.current_end_hour ?? 23)
    qs.compare_start_hour = String(params.compare_start_hour ?? 0)
    qs.compare_end_hour = String(params.compare_end_hour ?? 23)
  }
  const res = await api.get<ApiResp<UserTrendResp>>(
    '/api/user_stats/user_trend',
    { params: qs }
  )
  return res.data
}
