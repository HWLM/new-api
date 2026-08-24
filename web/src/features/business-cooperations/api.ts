import { api } from '@/lib/api'

import type { ApiResponse, PageData, UpstreamCandidate, UpstreamInput } from '../upstreams/types'

export type BusinessCooperation = {
  id: number
  user_id: number
  upstream_id: number
  status: string
  reject_reason?: string
  revision: number
  created_time: number
  updated_time: number
  submitted_time: number
  reviewed_time?: number
  upstream: UpstreamCandidate
}

export async function updateBusinessCooperation(id: number, upstream: UpstreamInput, resubmit: boolean) {
  const path = resubmit ? `/api/business-cooperation/${id}/resubmit` : `/api/business-cooperation/${id}`
  const response = await api.request<ApiResponse<BusinessCooperation>>({ method: resubmit ? 'POST' : 'PUT', url: path, data: { upstream } })
  return response.data
}

export async function deleteBusinessCooperation(id: number) {
  const response = await api.delete<ApiResponse<null>>(`/api/business-cooperation/${id}`)
  return response.data
}

export async function listBusinessCooperations() {
  const response = await api.get<ApiResponse<PageData<BusinessCooperation>>>(
    '/api/business-cooperation'
  )
  return response.data
}

export async function createBusinessCooperation(upstream: UpstreamInput) {
  const response = await api.post<ApiResponse<BusinessCooperation>>(
    '/api/business-cooperation',
    { upstream }
  )
  return response.data
}

export async function discoverBusinessCooperationModels(input: UpstreamInput) {
  const response = await api.post<ApiResponse<{ models: string[] }>>(
    '/api/business-cooperation/models',
    { ...input, models: undefined }
  )
  return response.data
}
