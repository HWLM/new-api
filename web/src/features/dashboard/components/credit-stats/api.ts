/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'
import type {
  AmountDetailsFilter,
  AmountDetailsResponse,
  CreditBackfillPayload,
  CreditMonthlyFilter,
  CreditMonthlyListResp,
  CreditRecordsFilter,
  CreditRecordsResp,
  CreditRepaymentPayload,
  CreditSummary,
  CreditSummaryFilter,
  CreditSummaryListResp,
  CreditTrendDimension,
  CreditTrendGranularity,
  CreditTrendResp,
} from './types'

type ApiResp<T> = { success: boolean; message?: string; data: T }

// channel / sales 多选 → join(',') 拼成单 value，后端 splitCSV 还原数组。
function appendMultiSelect(
  params: Record<string, string>,
  filter: { username?: string; channel?: string[]; sales?: string[] }
): void {
  if (filter.username) params.username = filter.username
  if (filter.channel && filter.channel.length) {
    params.channel = filter.channel.join(',')
  }
  if (filter.sales && filter.sales.length) params.sales = filter.sales.join(',')
}

function appendPagination(
  params: Record<string, string>,
  filter: { page?: number; page_size?: number }
): void {
  if (filter.page) params.page = String(filter.page)
  if (filter.page_size) params.page_size = String(filter.page_size)
}

export async function fetchCreditSummary(
  startDate: string,
  endDate: string
): Promise<CreditSummary> {
  const res = await api.get<ApiResp<CreditSummary>>(
    '/api/credit_stats/summary',
    { params: { start_date: startDate, end_date: endDate } }
  )
  return res.data.data
}

export async function fetchCreditTrend(
  startDate: string,
  endDate: string,
  granularity: CreditTrendGranularity,
  dimension: CreditTrendDimension
): Promise<CreditTrendResp> {
  const res = await api.get<ApiResp<CreditTrendResp>>(
    '/api/credit_stats/trend',
    {
      params: {
        start_date: startDate,
        end_date: endDate,
        granularity,
        dimension,
      },
    }
  )
  return res.data.data
}

export async function fetchCreditDetailsSummary(
  filter: CreditSummaryFilter
): Promise<CreditSummaryListResp> {
  const params: Record<string, string> = {}
  appendMultiSelect(params, filter)
  appendPagination(params, filter)
  if (filter.sort_by) {
    params.sort_by = filter.sort_by
    params.sort_dir = filter.sort_dir ?? 'desc'
  }
  const res = await api.get<ApiResp<CreditSummaryListResp>>(
    '/api/credit_stats/details_summary',
    { params }
  )
  return res.data.data
}

export async function fetchCreditDetailsMonthly(
  filter: CreditMonthlyFilter
): Promise<CreditMonthlyListResp> {
  const params: Record<string, string> = {
    start_month: filter.start_month,
    end_month: filter.end_month,
  }
  appendMultiSelect(params, filter)
  appendPagination(params, filter)
  const res = await api.get<ApiResp<CreditMonthlyListResp>>(
    '/api/credit_stats/details_monthly',
    { params }
  )
  return res.data.data
}

export async function fetchCreditRecords(
  filter: CreditRecordsFilter
): Promise<CreditRecordsResp> {
  const params: Record<string, string> = {
    user_id: String(filter.user_id),
    type: filter.type,
  }
  if (filter.start_month) params.start_month = filter.start_month
  if (filter.end_month) params.end_month = filter.end_month
  appendPagination(params, filter)
  const res = await api.get<ApiResp<CreditRecordsResp>>(
    '/api/credit_stats/records',
    { params }
  )
  return res.data.data
}

export async function createCreditRepayment(
  payload: CreditRepaymentPayload
): Promise<void> {
  await api.post('/api/credit_stats/repayment', payload)
}

export async function fetchAmountDetails(
  filter: AmountDetailsFilter
): Promise<AmountDetailsResponse> {
  const params: Record<string, string> = {
    dimension: filter.dimension,
    start_date: filter.start_date,
    end_date: filter.end_date,
  }
  appendPagination(params, filter)
  const res = await api.get<ApiResp<AmountDetailsResponse>>(
    '/api/credit_stats/amount_details',
    { params }
  )
  return res.data.data
}

export async function createCreditBackfill(
  payload: CreditBackfillPayload
): Promise<void> {
  await api.post('/api/credit_stats/backfill', payload)
}
