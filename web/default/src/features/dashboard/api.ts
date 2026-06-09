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
import { api } from '@/lib/api'
import type {
  QuotaDataItem,
  TokenDailyDetailFilters,
  TokenDailyDetailItem,
  TokenExhaustingItem,
  TokenStatsSummary,
  TokenStatsTopItem,
  UptimeGroupResult,
} from './types'

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

// Get user quota data within a time range
// Admin users get all users' data by default (matching classic frontend behavior)
export async function getUserQuotaDates(
  params: {
    start_timestamp: number
    end_timestamp: number
    default_time?: string
    username?: string
  },
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data' : '/api/data/self'
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    endpoint,
    { params }
  )
  return res.data
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: {
  start_timestamp: number
  end_timestamp: number
}) {
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    '/api/data/users',
    { params }
  )
  return res.data
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    '/api/uptime/status'
  )
  return res.data
}

// ----------------------------------------------------------------------------
// Token Statistics
// ----------------------------------------------------------------------------

export async function getTokenStatsSummary() {
  const res = await api.get<{ success: boolean; data: TokenStatsSummary }>(
    '/api/data/token_stats/summary/self'
  )
  return res.data
}

export async function getTokenStatsTop(limit = 10) {
  const res = await api.get<{ success: boolean; data: TokenStatsTopItem[] }>(
    '/api/data/token_stats/top/self',
    { params: { limit } }
  )
  return res.data
}

export async function getTokenStatsExhausting(
  page = 1,
  pageSize = 10
) {
  const res = await api.get<{
    success: boolean
    data: { items: TokenExhaustingItem[]; total: number }
  }>('/api/data/token_stats/exhausting/self', {
    params: { page, page_size: pageSize },
  })
  return res.data
}

export async function getTokenStatsDaily(filters: TokenDailyDetailFilters) {
  const res = await api.get<{
    success: boolean
    data: {
      items: TokenDailyDetailItem[]
      total: number
      page: number
      page_size: number
    }
  }>('/api/data/token_stats/daily/self', { params: filters })
  return res.data
}
