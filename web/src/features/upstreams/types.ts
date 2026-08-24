export type UpstreamSource = 'admin' | 'self'
export type BenchmarkStatus = 'pending' | 'running' | 'completed'

export type UpstreamCandidate = {
  id: number
  source: UpstreamSource
  owner_user_id?: number
  application_id?: number
  application_status?: string
  type: number
  name: string
  base_url: string
  models: string
  contact: string
  remark: string
  benchmark_status: BenchmarkStatus
  latest_run_id?: number
  channel_id?: number
  synced_time?: number
  created_time: number
  updated_time: number
  has_api_key: boolean
  latest_benchmark?: BenchmarkRun
}

export type UpstreamInput = {
  type: number
  name: string
  base_url: string
  api_key: string
  models: string
  contact: string
  remark: string
}

export type UpstreamModels = {
  models: string[]
}

export type BenchmarkMetric = {
  admission: number
  full: number
  weight: number
  enabled: boolean
  direction: 'higher' | 'lower'
}

export type BenchmarkProfile = {
  name: string
  version: number
  metrics: Record<string, BenchmarkMetric>
  updated_time: number
}

export type BenchmarkRun = {
  id: number
  status: string
  concurrency?: number
  model?: string
  status_code?: number
  latency_ms?: number
  overall_score?: number
  metrics?: Record<string, number>
  scores?: Record<string, number>
  unmet_count?: number
  started_time?: number
  finished_time?: number
  duration_ms?: number
  error?: string
  profile_name?: string
  profile_version?: number
  baseline?: Record<string, BenchmarkMetric>
}

export function countBenchmarkUnmetMetrics(run: BenchmarkRun) {
  return Object.entries(run.baseline ?? {}).reduce((count, [name, metric]) => {
    if (!metric.enabled) return count
    const actual = run.metrics?.[name]
    if (actual == null || !Number.isFinite(actual)) return count
    const met =
      metric.direction === 'higher'
        ? actual >= metric.admission
        : actual <= metric.admission
    return met ? count : count + 1
  }, 0)
}

export type AutoSyncConfig = {
  enabled: boolean
  channel_tag: string
  min_score: number
}

export type PageData<T> = {
  page: number
  page_size: number
  total: number
  items: T[]
}

export type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}
