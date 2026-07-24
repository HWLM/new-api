/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { TrendDialog } from '@/features/users/vip-stats/trend-dialog'
import type {
  VipStatsTrend,
  VipStatsTrendParams,
} from '@/features/users/types'
import {
  fetchChannelPie,
  fetchRechargeTrend,
  fetchTopUsers,
  fetchUserStatsCards,
  fetchUserStatsFilterOptions,
} from './api'
import { ChartChannelPie } from './chart-channel-pie'
import { ChartConsumptionTrend } from './chart-consumption-trend'
import { ChartRechargeLine } from './chart-recharge-line'
import { ChartTopUsers } from './chart-top-users'
import { CompareBar } from './compare-bar'
import { computeCompareWindow, isTodayMode } from './compare-util'
import { DetailsTable } from './details-table'
import { FilterBar } from './filter-bar'
import { PromotionTablesCompare } from './promotion-tables-compare'
import { SummaryCards } from './summary-cards'
import type { DetailsRow, StatsFilter } from './types'

// 默认时间：今天（与 FilterBar 默认 quick range 一致）
function buildDefaultFilter(): StatsFilter {
  const now = new Date()
  const fmt = (d: Date) => {
    const y = d.getFullYear()
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    return `${y}-${m}-${day}`
  }
  const today = fmt(now)
  return {
    start_date: today,
    end_date: today,
    username: '',
    channel: [],
    sales: [],
    is_vip: '',
  }
}

// filter.start_date / end_date (YYYY-MM-DD) 转 PromotionTables 期望的 unix 时间戳（本地时区当天 00:00 ~ 23:59:59）
function filterToTimeRange(filter: StatsFilter): {
  start_timestamp: number
  end_timestamp: number
} {
  const s = filter.start_date ?? ''
  const e = filter.end_date ?? ''
  const startMs = s ? new Date(`${s}T00:00:00`).getTime() : 0
  const endMs = e ? new Date(`${e}T23:59:59`).getTime() : 0
  return {
    start_timestamp: Math.floor(startMs / 1000),
    end_timestamp: Math.floor(endMs / 1000),
  }
}

export function NewUserStats() {
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<StatsFilter>(() => buildDefaultFilter())
  const [trendTarget, setTrendTarget] = useState<DetailsRow | null>(null)

  // 对比相关 state
  // 小时段默认：0 ~ 上一个已过完的整点。比如 16:10 打开页面 → 0~15，
  // 避免把「今天当前未走完的小时」跟昨天完整整点做对比，导致曲线尾巴异常下探。
  const [compareEnabled, setCompareEnabled] = useState(false)
  const [compareStartHour, setCompareStartHour] = useState(0)
  const [compareEndHour, setCompareEndHour] = useState(
    () => Math.max(0, new Date().getHours() - 1)
  )
  // 用户手动指定的对比日期；null 表示走自动推算
  const [compareOverride, setCompareOverride] = useState<{
    start_date: string
    end_date: string
  } | null>(null)

  // 顶部筛选变化时自动关闭对比 + 清空手动日期覆盖（避免基线对不上）
  useEffect(() => {
    setCompareEnabled(false)
    setCompareOverride(null)
  }, [
    filter.start_date,
    filter.end_date,
    filter.username,
    filter.channel,
    filter.sales,
    filter.is_vip,
  ])

  const todayMode = isTodayMode(filter)
  const autoCompareWindow = useMemo(
    () => computeCompareWindow(filter),
    [filter]
  )
  // 展示 + 传给子组件用的对比窗：用户覆盖优先，否则自动推算
  const compareWindow = compareOverride ?? autoCompareWindow
  // 实际生效的对比窗（仅当 enabled 时传给子组件）
  const effectiveCompareWindow = compareEnabled ? compareWindow : null

  // 小时段实际生效条件：今天模式 + 对比开启 + 小时段非默认全天
  const hourActive =
    compareEnabled &&
    todayMode &&
    (compareStartHour !== 0 || compareEndHour !== 24)
  const effectiveStartHour = hourActive ? compareStartHour : undefined
  const effectiveEndHour = hourActive ? compareEndHour : undefined

  // 卡片不受筛选影响 —— 永远是当前最新值
  const cardsQuery = useQuery({
    queryKey: ['user-stats', 'cards'],
    queryFn: fetchUserStatsCards,
    staleTime: 60_000,
  })

  // 渠道 + 销售下拉数据
  const filterOptionsQuery = useQuery({
    queryKey: ['user-stats', 'filter-options'],
    queryFn: fetchUserStatsFilterOptions,
    staleTime: 5 * 60_000,
  })

  // 用户分组（用于明细子筛选里的 user_group 下拉）
  const userGroupsQuery = useQuery({
    queryKey: ['user-stats', 'groups'],
    queryFn: async () => {
      const res = await api.get<{ success: boolean; data: string[] }>(
        '/api/group/'
      )
      return res.data.data ?? []
    },
    staleTime: 5 * 60_000,
  })

  const filterKey = useMemo(() => JSON.stringify(filter), [filter])

  // 刷新按钮：把所有 ['user-stats', ...] 前缀的 query 一次性置为 stale 并重新拉取。
  // 页面内 12 个 query 的 key 都以 'user-stats' 开头（cards/filter-options/groups/top-users/
  // recharge-trend/channel-pie/consumption-trend/details*/promotion-*）。
  const handleRefresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['user-stats'] })
  }, [queryClient])

  const topUsersQuery = useQuery({
    queryKey: ['user-stats', 'top-users', filterKey],
    queryFn: () => fetchTopUsers(filter),
  })

  const trendQuery = useQuery({
    queryKey: ['user-stats', 'recharge-trend', filterKey],
    queryFn: () => fetchRechargeTrend(filter),
  })

  const channelPieQuery = useQuery({
    queryKey: ['user-stats', 'channel-pie', filterKey],
    queryFn: () => fetchChannelPie(filter),
  })

  const promotionTimeRange = useMemo(() => filterToTimeRange(filter), [filter])

  // admin 版 trend 请求：包给 TrendDialog 的 fetchTrend prop。
  // 复用 vip-stats 的 dialog 但走 /api/user_stats/user_trend（无密码门，admin auth）。
  const adminTrendFetcher = useCallback(
    async (
      params: VipStatsTrendParams
    ): Promise<{
      success: boolean
      message?: string
      data?: VipStatsTrend
    }> => {
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
      const res = await api.get<{
        success: boolean
        message?: string
        data?: VipStatsTrend
      }>('/api/user_stats/user_trend', { params: qs })
      return res.data
    },
    []
  )

  return (
    <div className='space-y-4'>
      <SummaryCards
        data={cardsQuery.data}
        isLoading={cardsQuery.isLoading}
      />

      <FilterBar
        value={filter}
        onChange={setFilter}
        options={filterOptionsQuery.data}
        onRefresh={handleRefresh}
      />

      <CompareBar
        compareWindow={compareWindow}
        onCompareDateChange={(start_date, end_date) =>
          setCompareOverride({ start_date, end_date })
        }
        isTodayMode={todayMode}
        startHour={compareStartHour}
        endHour={compareEndHour}
        onHourChange={(s, e) => {
          setCompareStartHour(s)
          setCompareEndHour(e)
        }}
        enabled={compareEnabled}
        onToggle={() => setCompareEnabled((v) => !v)}
      />

      <div className='grid grid-cols-1 gap-4 lg:grid-cols-3'>
        <ChartTopUsers
          data={topUsersQuery.data}
          isLoading={topUsersQuery.isLoading}
        />
        <ChartRechargeLine
          data={trendQuery.data}
          isLoading={trendQuery.isLoading}
        />
        <ChartChannelPie
          data={channelPieQuery.data}
          isLoading={channelPieQuery.isLoading}
        />
      </div>

      <ChartConsumptionTrend
        filter={filter}
        compareWindow={effectiveCompareWindow}
        startHour={effectiveStartHour}
        endHour={effectiveEndHour}
      />

      <PromotionTablesCompare
        timeRange={promotionTimeRange}
        compareWindow={effectiveCompareWindow}
        topN={10}
      />

      <DetailsTable
        options={filterOptionsQuery.data}
        userGroupOptions={userGroupsQuery.data ?? []}
        onOpenTrend={(row) => setTrendTarget(row)}
      />

      <TrendDialog
        open={trendTarget !== null}
        onOpenChange={(open) => {
          if (!open) setTrendTarget(null)
        }}
        password=''
        userId={trendTarget?.user_id ?? null}
        username={trendTarget?.username ?? trendTarget?.display_name}
        fetchTrend={adminTrendFetcher}
      />
    </div>
  )
}
