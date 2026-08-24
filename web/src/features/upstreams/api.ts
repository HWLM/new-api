import { api } from '@/lib/api'

import type {
  ApiResponse,
  AutoSyncConfig,
  BenchmarkProfile,
  BenchmarkRun,
  PageData,
  UpstreamCandidate,
  UpstreamInput,
  UpstreamModels,
} from './types'

export async function getBenchmarkProfile() {
  const response = await api.get<ApiResponse<BenchmarkProfile>>(
    '/api/upstream/benchmark-profile'
  )
  return response.data
}

export async function updateBenchmarkProfile(input: BenchmarkProfile) {
  const response = await api.put<ApiResponse<BenchmarkProfile>>(
    '/api/upstream/benchmark-profile',
    { metrics: input.metrics }
  )
  return response.data
}

export async function getAutoSyncConfig() {
  const response = await api.get<ApiResponse<AutoSyncConfig>>(
    '/api/upstream/auto-sync'
  )
  return response.data
}

export async function updateAutoSyncConfig(input: AutoSyncConfig) {
  const response = await api.put<ApiResponse<AutoSyncConfig>>(
    '/api/upstream/auto-sync',
    input
  )
  return response.data
}

export async function listBenchmarkRuns(id: number) {
  const response = await api.get<ApiResponse<BenchmarkRun[]>>(
    `/api/upstream/${id}/benchmarks`
  )
  return response.data
}

export async function listUpstreamModels(id: number) {
  const response = await api.get<ApiResponse<UpstreamModels>>(
    `/api/upstream/${id}/models`
  )
  return response.data
}

export async function discoverUpstreamModels(input: UpstreamInput) {
  const response = await api.post<ApiResponse<{ models: string[] }>>(
    '/api/business-cooperation/models',
    { ...input, models: undefined }
  )
  return response.data
}

export async function listUpstreams(params: {
  p: number
  page_size: number
  keyword?: string
  source?: string
  benchmark_status?: string
}) {
  const response = await api.get<ApiResponse<PageData<UpstreamCandidate>>>(
    '/api/upstream',
    { params }
  )
  return response.data
}

export async function createUpstream(input: UpstreamInput) {
  const response = await api.post<ApiResponse<UpstreamCandidate>>(
    '/api/upstream',
    input
  )
  return response.data
}

export async function updateUpstream(id: number, input: UpstreamInput) {
  const response = await api.put<ApiResponse<UpstreamCandidate>>(
    `/api/upstream/${id}`,
    input
  )
  return response.data
}

export async function startBenchmark(
  id: number,
  input: { model: string; concurrency: number }
) {
  const response = await api.post<ApiResponse<{ task_id: string }>>(
    `/api/upstream/${id}/benchmark`,
    input
  )
  return response.data
}

export async function cancelBenchmark(id: number) {
  const response = await api.post<ApiResponse<null>>(
    `/api/upstream/${id}/benchmark/cancel`,
    {}
  )
  return response.data
}

export async function rejectUpstream(id: number, reason: string) {
  const response = await api.post<ApiResponse<null>>(
    `/api/upstream/${id}/reject`,
    { reason }
  )
  return response.data
}

export async function syncUpstream(id: number) {
  const response = await api.post<ApiResponse<{ channel_id: number }>>(
    `/api/upstream/${id}/sync`,
    {}
  )
  return response.data
}

export async function syncUpstreams(ids: number[], tag: string) {
  const response = await api.post<ApiResponse<{ channel_ids: number[] }>>(
    '/api/upstream/sync',
    { ids, tag }
  )
  return response.data
}
