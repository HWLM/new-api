/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { fetchUserStatsDetails, fetchUserStatsDetailsDaily } from './api'
import { DetailsFilterBar } from './details-filter-bar'
import type {
  DetailsDailyRow,
  DetailsFilter,
  DetailsRow,
  FilterOptions,
} from './types'

type StatsMode = 'summary' | 'daily'

type SortDir = 'asc' | 'desc'

const PAGE_SIZE = 20

// Unix 秒 → "YYYY-MM-DD HH:MM:SS"；0 → "-"
function fmtTime(ts: number): string {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${y}-${m}-${day} ${hh}:${mm}:${ss}`
}

function fmtTimeDate(ts: number): string {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// 默认 daily 日期：近 7 天（含今天）
function defaultDailyRange(): { start: string; end: string } {
  const today = dayjs()
  return {
    start: today.subtract(6, 'day').format('YYYY-MM-DD'),
    end: today.format('YYYY-MM-DD'),
  }
}

export function DetailsTable({
  options,
  userGroupOptions,
  onOpenTrend,
}: {
  options: FilterOptions | undefined
  userGroupOptions: string[]
  /** 点击「查看趋势」时回调，主组件负责打开 trend dialog */
  onOpenTrend: (row: DetailsRow) => void
}) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<StatsMode>('summary')
  const [filter, setFilter] = useState<DetailsFilter>({
    page: 1,
    page_size: PAGE_SIZE,
    sort_by: '',
    sort_dir: 'desc',
  })
  // daily 模式的日期范围（独立于 summary 的 last_consume_date_from）
  const [dailyRange, setDailyRange] = useState<{ start: string; end: string }>(
    defaultDailyRange
  )

  // 汇总
  const summaryKey = useMemo(() => JSON.stringify(filter), [filter])
  const summaryQuery = useQuery({
    queryKey: ['user-stats', 'details', summaryKey],
    queryFn: () => fetchUserStatsDetails(filter),
    placeholderData: (prev) => prev,
    enabled: mode === 'summary',
  })

  // 按天
  const dailyKey = useMemo(
    () => JSON.stringify({ filter, dailyRange }),
    [filter, dailyRange]
  )
  const dailyQuery = useQuery({
    queryKey: ['user-stats', 'details_daily', dailyKey],
    queryFn: () =>
      fetchUserStatsDetailsDaily({
        start_date: dailyRange.start,
        end_date: dailyRange.end,
        username: filter.username,
        channel: filter.channel,
        sales: filter.sales,
        user_group: filter.user_group,
        is_vip: filter.is_vip,
        page: filter.page,
        page_size: filter.page_size,
        sort_by: filter.sort_by,
        sort_dir: filter.sort_dir,
      }),
    placeholderData: (prev) => prev,
    enabled: mode === 'daily',
  })

  const total =
    mode === 'summary' ? (summaryQuery.data?.total ?? 0) : (dailyQuery.data?.total ?? 0)
  const isLoading =
    mode === 'summary' ? summaryQuery.isLoading : dailyQuery.isLoading
  const pageSize = filter.page_size ?? PAGE_SIZE
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const page = filter.page ?? 1

  const goPage = (p: number) => {
    const np = Math.max(1, Math.min(totalPages, p))
    setFilter({ ...filter, page: np })
  }

  const handleSort = (key: string) => {
    let nextDir: SortDir = 'desc'
    if (filter.sort_by === key) {
      nextDir = filter.sort_dir === 'desc' ? 'asc' : 'desc'
    }
    setFilter({ ...filter, sort_by: key, sort_dir: nextDir, page: 1 })
  }

  const sortIndicator = (key: string) => {
    if (filter.sort_by !== key) return ''
    return filter.sort_dir === 'desc' ? ' ↓' : ' ↑'
  }

  // 标签 chip 渲染（两种模式共用）
  const renderTags = (row: DetailsRow | DetailsDailyRow) => (
    <div className='flex flex-wrap gap-1'>
      {row.is_vip_customer && (
        <Badge
          variant='outline'
          className='border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900 dark:bg-rose-950 dark:text-rose-300'
        >
          {t('VIP Customer')}
        </Badge>
      )}
      {row.is_official && (
        <Badge
          variant='outline'
          className='border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300'
        >
          {t('Official Customer')}
        </Badge>
      )}
    </div>
  )

  const summaryRows = summaryQuery.data?.rows ?? []
  const dailyRows = dailyQuery.data?.rows ?? []

  // 切 mode 时把页码重置
  const handleModeChange = (m: string) => {
    const next = m as StatsMode
    setMode(next)
    setFilter({ ...filter, page: 1, sort_by: '', sort_dir: 'desc' })
  }

  return (
    <Card>
      <CardHeader className='flex flex-row items-center justify-between space-y-0 pb-2'>
        <CardTitle className='text-base'>{t('Details Data')}</CardTitle>
        <Tabs value={mode} onValueChange={handleModeChange}>
          <TabsList>
            <TabsTrigger value='summary' className='px-3 text-xs'>
              {t('Summary Statistics')}
            </TabsTrigger>
            <TabsTrigger value='daily' className='px-3 text-xs'>
              {t('Daily Statistics')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </CardHeader>
      <CardContent className='space-y-3'>
        <DetailsFilterBar
          mode={mode}
          value={filter}
          onChange={(next) => setFilter({ ...filter, ...next })}
          options={options}
          userGroupOptions={userGroupOptions}
          dailyStartDate={dailyRange.start}
          dailyEndDate={dailyRange.end}
          onDailyDateChange={(s, e) => {
            setDailyRange({ start: s, end: e })
            setFilter({ ...filter, page: 1 })
          }}
        />

        <div className='overflow-x-auto'>
          {mode === 'summary' ? (
            // ====== 汇总统计表 ======
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead
                    className='cursor-pointer select-none'
                    onClick={() => handleSort('username')}
                  >
                    {t('Customer')}
                    {sortIndicator('username')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none'
                    onClick={() => handleSort('display_name')}
                  >
                    {t('Display Name')}
                    {sortIndicator('display_name')}
                  </TableHead>
                  <TableHead>{t('Tags')}</TableHead>
                  <TableHead>{t('Owning Channel')}</TableHead>
                  <TableHead>{t('Owning Sales')}</TableHead>
                  <TableHead
                    className='cursor-pointer select-none'
                    onClick={() => handleSort('last_consume')}
                  >
                    {t('Last Consume Date')}
                    {sortIndicator('last_consume')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none'
                    onClick={() => handleSort('last_recharge')}
                  >
                    {t('Last Recharge Date')}
                    {sortIndicator('last_recharge')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('requests')}
                  >
                    {t('Total Requests')}
                    {sortIndicator('requests')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('tokens')}
                  >
                    {t('Total Tokens')}
                    {sortIndicator('tokens')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('recharge')}
                  >
                    {t('Total Recharge (¥)')}
                    {sortIndicator('recharge')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('consumed')}
                  >
                    {t('Total Consumed ($)')}
                    {sortIndicator('consumed')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('remaining')}
                  >
                    {t('Remaining ($)')}
                    {sortIndicator('remaining')}
                  </TableHead>
                  <TableHead>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  Array.from({ length: 5 }).map((_, i) => (
                    <TableRow key={`sk-${i}`}>
                      <TableCell colSpan={13}>
                        <Skeleton className='h-5 w-full' />
                      </TableCell>
                    </TableRow>
                  ))
                ) : summaryRows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={13}
                      className='text-muted-foreground text-center'
                    >
                      {t('No data')}
                    </TableCell>
                  </TableRow>
                ) : (
                  summaryRows.map((row) => (
                    <TableRow key={row.user_id}>
                      <TableCell className='font-medium'>
                        {row.username}
                      </TableCell>
                      <TableCell>{row.display_name || '-'}</TableCell>
                      <TableCell>{renderTags(row)}</TableCell>
                      <TableCell>{row.business_channel || '-'}</TableCell>
                      <TableCell>{row.inviter_display_name || '-'}</TableCell>
                      <TableCell className='whitespace-nowrap'>
                        {fmtTimeDate(row.last_consume_at)}
                      </TableCell>
                      <TableCell className='whitespace-nowrap'>
                        {fmtTime(row.last_recharge_at)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.total_requests.toLocaleString()}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.total_tokens.toLocaleString()}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.total_recharge_cny.toFixed(2)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.total_consumed_usd.toFixed(2)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.remaining_usd.toFixed(2)}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant='outline'
                          size='sm'
                          onClick={() => onOpenTrend(row)}
                        >
                          {t('View Trend')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          ) : (
            // ====== 按天统计表 ======
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead
                    className='cursor-pointer select-none'
                    onClick={() => handleSort('date')}
                  >
                    {t('Date')}
                    {sortIndicator('date')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none'
                    onClick={() => handleSort('username')}
                  >
                    {t('Customer')}
                    {sortIndicator('username')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none'
                    onClick={() => handleSort('display_name')}
                  >
                    {t('Display Name')}
                    {sortIndicator('display_name')}
                  </TableHead>
                  <TableHead>{t('Tags')}</TableHead>
                  <TableHead>{t('Owning Channel')}</TableHead>
                  <TableHead>{t('Owning Sales')}</TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('requests')}
                  >
                    {t('Daily Requests')}
                    {sortIndicator('requests')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('consumed')}
                  >
                    {t('Daily Consumed ($)')}
                    {sortIndicator('consumed')}
                  </TableHead>
                  <TableHead
                    className='cursor-pointer select-none text-right'
                    onClick={() => handleSort('tokens')}
                  >
                    {t('Daily Tokens')}
                    {sortIndicator('tokens')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  Array.from({ length: 5 }).map((_, i) => (
                    <TableRow key={`sk-${i}`}>
                      <TableCell colSpan={9}>
                        <Skeleton className='h-5 w-full' />
                      </TableCell>
                    </TableRow>
                  ))
                ) : dailyRows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={9}
                      className='text-muted-foreground text-center'
                    >
                      {t('No data')}
                    </TableCell>
                  </TableRow>
                ) : (
                  dailyRows.map((row) => (
                    <TableRow key={`${row.user_id}-${row.date}`}>
                      <TableCell className='whitespace-nowrap font-medium'>
                        {row.date}
                      </TableCell>
                      <TableCell>{row.username}</TableCell>
                      <TableCell>{row.display_name || '-'}</TableCell>
                      <TableCell>{renderTags(row)}</TableCell>
                      <TableCell>{row.business_channel || '-'}</TableCell>
                      <TableCell>{row.inviter_display_name || '-'}</TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.daily_requests.toLocaleString()}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.daily_consumed_usd.toFixed(2)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.daily_tokens.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          )}
        </div>

        {/* 分页 */}
        <div className='flex items-center justify-between'>
          <div className='text-muted-foreground text-xs'>
            {t('Total {{count}} rows', { count: total })}
          </div>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={page <= 1}
              onClick={() => goPage(page - 1)}
            >
              <ChevronLeft className='h-4 w-4' />
            </Button>
            <span className='text-xs tabular-nums'>
              {page} / {totalPages}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={page >= totalPages}
              onClick={() => goPage(page + 1)}
            >
              <ChevronRight className='h-4 w-4' />
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
