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
  ApiResponse,
  DistillAlertTestResult,
  DistillClearCacheRequest,
  DistillClearCacheResult,
  DistillHitsPage,
  DistillHitsQuery,
  DistillSetting,
  DistillSettingUpdate,
  DistillTestResult,
} from './types'

export async function getDistillSetting() {
  const res = await api.get<ApiResponse<DistillSetting>>(
    '/api/distill_detect/setting'
  )
  return res.data
}

export async function updateDistillSetting(input: DistillSettingUpdate) {
  const res = await api.put<ApiResponse<DistillSetting>>(
    '/api/distill_detect/setting',
    input
  )
  return res.data
}

export async function listDistillHits(query: DistillHitsQuery) {
  const params: Record<string, string | number> = {
    p: query.p,
    page_size: query.page_size,
  }
  if (query.user_id) params.user_id = query.user_id
  if (query.request_id) params.request_id = query.request_id
  if (query.from) params.from = query.from
  if (query.to) params.to = query.to
  const res = await api.get<ApiResponse<DistillHitsPage>>(
    '/api/distill_detect/hits',
    { params }
  )
  return res.data
}

export async function testDistillAlert() {
  const res = await api.post<ApiResponse<DistillAlertTestResult>>(
    '/api/distill_detect/alert_test',
    {}
  )
  return res.data
}

export async function testDistill(input: { text?: string; body?: string }) {
  const res = await api.post<ApiResponse<DistillTestResult>>(
    '/api/distill_detect/test',
    input
  )
  return res.data
}

export async function clearDistillCache(input: DistillClearCacheRequest) {
  const res = await api.post<ApiResponse<DistillClearCacheResult>>(
    '/api/distill_detect/clear_cache',
    input
  )
  return res.data
}
