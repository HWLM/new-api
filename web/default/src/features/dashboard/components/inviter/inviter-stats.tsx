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
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { quotaUnitsToDollars } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { DatePicker } from '@/components/date-picker'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  getInviterCharts,
  getInviterDaily,
  getInviterStatCards,
  getInviterSummary,
} from '../../api'
import type {
  InviterCharts,
  InviterDailyRow,
  InviterSummaryRow,
} from '../../types'

const quotaToUsd = (q: number) => quotaUnitsToDollars(q)

// ---------------- 时间范围 preset ----------------
const TIME_PRESETS = [
  { key: 'all', labelKey: 'All', days: 0 },
  { key: 'today', labelKey: 'Today', days: 0 },
  { key: '3d', labelKey: 'Last 3 Days', days: 3 },
  { key: '7d', labelKey: 'Last 7 Days', days: 7 },
  { key: '1m', labelKey: 'Last 1 Month', days: 30 },
] as const

type TimePreset = (typeof TIME_PRESETS)[number]['key']

function getRangeOfPreset(p: TimePreset): {
  startTs: number
  endTs: number
} {
  const now = dayjs()
  const endTs = now.unix()
  if (p === 'all') return { startTs: 0, endTs: 0 }
  if (p === 'today') {
    return { startTs: now.startOf('day').unix(), endTs }
  }
  const map: Record<string, number> = { '3d': 3, '7d': 7, '1m': 30 }
  const days = map[p] ?? 7
  return {
    startTs: now
      .subtract(days - 1, 'day')
      .startOf('day')
      .unix(),
    endTs,
  }
}

// ============================================================================
// 主组件
// ============================================================================

export function InviterStats() {
  const businessChannel = useAuthStore(
    (s) => s.auth.user?.business_channel || ''
  )
  const isBusiness = businessChannel !== ''

  return (
    <div className='flex flex-col gap-4'>
      <SummaryCards />
      {isBusiness ? (
        <>
          <ChartsSection />
          <DetailSection />
        </>
      ) : (
        <></>
      )}
    </div>
  )
}

// ============================================================================
// 4 张卡片
// ============================================================================

function SummaryCards() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['inviter-stats', 'cards'],
    queryFn: getInviterStatCards,
    select: (res) => (res.success ? res.data : undefined),
    staleTime: 30_000,
  })
  const fmt = (n: number | undefined) => (n == null ? '-' : n.toLocaleString())
  const fmtUsd = (q: number | undefined) =>
    q == null ? '-' : formatCurrencyFromUSD(quotaToUsd(q))

  const cards = [
    { label: t('Invited Count'), value: fmt(data?.invited_count) },
    { label: t('Total Consumed ($)'), value: fmtUsd(data?.total_consumed) },
    { label: t('Today Active Users'), value: fmt(data?.today_active_users) },
    { label: t('Today Consumed ($)'), value: fmtUsd(data?.today_consumed) },
  ]
  return (
    <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4'>
      {cards.map((c) => (
        <Card key={c.label}>
          <CardHeader className='pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-normal'>
              {c.label}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-semibold tabular-nums'>
              {isLoading ? '…' : c.value}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

// ============================================================================
// 3 张图表（受顶部时间筛选影响）
// ============================================================================

function ChartsSection() {
  const { t } = useTranslation()
  const [preset, setPreset] = useState<TimePreset>('today')
  const [appliedPreset, setAppliedPreset] = useState<TimePreset>('today')

  const timeRange = useMemo(() => {
    const { startTs, endTs } = getRangeOfPreset(appliedPreset)
    return { start_timestamp: startTs, end_timestamp: endTs }
  }, [appliedPreset])

  const { data, isLoading } = useQuery({
    queryKey: ['inviter-stats', 'charts', timeRange],
    queryFn: () => getInviterCharts(timeRange),
    select: (res) => (res.success ? res.data : undefined),
    staleTime: 30_000,
  })

  return (
    <Card>
      <CardContent className='flex flex-col gap-4 pt-6'>
        <div className='flex flex-wrap items-center gap-2'>
          <Tabs
            value={preset}
            onValueChange={(v) => setPreset(v as TimePreset)}
          >
            <TabsList>
              {TIME_PRESETS.map((p) => (
                <TabsTrigger
                  key={p.key}
                  value={p.key}
                  className='px-2.5 text-xs'
                >
                  {t(p.labelKey)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <Button size='sm' onClick={() => setAppliedPreset(preset)}>
            {t('Query')}
          </Button>
          <Button
            size='sm'
            variant='outline'
            onClick={() => {
              setPreset('today')
              setAppliedPreset('today')
            }}
          >
            {t('Reset')}
          </Button>
        </div>

        <div className='grid grid-cols-1 gap-3 md:grid-cols-3'>
          <ChartCard
            title={t('User Consumption Ranking (TOP10)')}
            isLoading={isLoading}
            data={data}
            type='top'
          />
          <ChartCard
            title={t('Consumption Trend')}
            isLoading={isLoading}
            data={data}
            type='trend'
          />
          <ChartCard
            title={t('Request Count')}
            isLoading={isLoading}
            data={data}
            type='requests'
          />
        </div>
      </CardContent>
    </Card>
  )
}

function ChartCard(props: {
  title: string
  isLoading: boolean
  data: InviterCharts | undefined
  type: 'top' | 'trend' | 'requests'
}) {
  const spec = useMemo(() => {
    if (!props.data) return undefined
    if (props.type === 'top') {
      return {
        type: 'bar',
        direction: 'horizontal',
        data: [
          {
            id: 'top',
            values: props.data.top_users.map((u) => ({
              user: u.username,
              quota: Number(quotaToUsd(u.quota).toFixed(2)),
            })),
          },
        ],
        xField: 'quota',
        yField: 'user',
        background: 'transparent',
      }
    }
    if (props.type === 'trend') {
      return {
        type: 'area',
        data: [
          {
            id: 'trend',
            values: props.data.daily.map((d) => ({
              date: d.date,
              quota: Number(quotaToUsd(d.quota).toFixed(2)),
            })),
          },
        ],
        xField: 'date',
        yField: 'quota',
        background: 'transparent',
      }
    }
    return {
      type: 'area',
      data: [
        {
          id: 'req',
          values: props.data.daily.map((d) => ({
            date: d.date,
            requests: d.requests,
          })),
        },
      ],
      xField: 'date',
      yField: 'requests',
      background: 'transparent',
    }
  }, [props.data, props.type])

  return (
    <div className='rounded-lg border'>
      <div className='border-b px-3 py-2 text-sm font-semibold'>
        {props.title}
      </div>
      <div className='h-72 p-2'>
        {props.isLoading ? (
          <Skeleton className='h-full w-full' />
        ) : spec ? (
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          <VChart spec={spec as any} />
        ) : null}
      </div>
    </div>
  )
}

// ============================================================================
// 明细表格（汇总 / 按天）
// ============================================================================

type DetailMode = 'summary' | 'daily'

function DetailSection() {
  const { t } = useTranslation()
  const [mode, setMode] = useState<DetailMode>('summary')
  // 共享：用户名（在两个视图间保留）
  const [username, setUsername] = useState('')

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>{t('Detail Data')}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-3'>
        {mode === 'summary' ? (
          <SummaryView
            username={username}
            onUsernameChange={setUsername}
            onSwitchDaily={(uname) => {
              setUsername(uname)
              setMode('daily')
            }}
            mode={mode}
            onModeChange={setMode}
          />
        ) : (
          <DailyView
            username={username}
            onUsernameChange={setUsername}
            mode={mode}
            onModeChange={setMode}
          />
        )}
      </CardContent>
    </Card>
  )
}

function ModeSwitch(props: {
  mode: DetailMode
  onChange: (m: DetailMode) => void
}) {
  const { t } = useTranslation()
  return (
    <Tabs
      value={props.mode}
      onValueChange={(v) => props.onChange(v as DetailMode)}
    >
      <TabsList>
        <TabsTrigger value='summary' className='px-3 text-xs'>
          {t('Summary')}
        </TabsTrigger>
        <TabsTrigger value='daily' className='px-3 text-xs'>
          {t('By Day')}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  )
}

// ----------- 汇总视图 -----------
type SummarySortKey =
  | 'username'
  | 'created_at'
  | 'last_consumed_at'
  | 'total_requests'
  | 'total_consumed'
  | 'total_recharge_cny'
  | 'total_tokens'
  | 'current_remaining'
type SortOrder = 'asc' | 'desc'

function SummaryView(props: {
  username: string
  onUsernameChange: (v: string) => void
  onSwitchDaily: (username: string) => void
  mode: DetailMode
  onModeChange: (m: DetailMode) => void
}) {
  const { t } = useTranslation()
  const [lastConsumedStart, setLastConsumedStart] = useState<Date | undefined>()
  const [lastConsumedEnd, setLastConsumedEnd] = useState<Date | undefined>()
  const [remainingOp, setRemainingOp] = useState<string>('>=')
  const [remainingValue, setRemainingValue] = useState<string>('')
  // 表头排序：默认按 total_consumed 降序，与后端默认一致
  const [sortBy, setSortBy] = useState<SummarySortKey>('total_consumed')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  // 受 "查询" 按钮提交才生效的实际筛选条件
  const [appliedQuery, setAppliedQuery] = useState<{
    last_consumed_start: number
    last_consumed_end: number
    remaining_op: string
    remaining_value: number
    username: string
  }>({
    last_consumed_start: 0,
    last_consumed_end: 0,
    remaining_op: '',
    remaining_value: 0,
    username: '',
  })

  const { data, isLoading } = useQuery({
    queryKey: [
      'inviter-stats',
      'summary',
      appliedQuery,
      sortBy,
      sortOrder,
    ],
    queryFn: () =>
      getInviterSummary({
        ...appliedQuery,
        sort_by: sortBy,
        sort_order: sortOrder,
      }),
    select: (res) => (res.success ? res.data : []),
    staleTime: 30_000,
  })

  const handleQuery = () => {
    const remVal = Number(remainingValue)
    const usdToQuota = (usd: number) => Math.round(usd / quotaUnitsToDollars(1))
    setAppliedQuery({
      last_consumed_start: lastConsumedStart
        ? dayjs(lastConsumedStart).startOf('day').unix()
        : 0,
      last_consumed_end: lastConsumedEnd
        ? dayjs(lastConsumedEnd).endOf('day').unix()
        : 0,
      remaining_op: remainingValue !== '' && remainingOp ? remainingOp : '',
      remaining_value:
        remainingValue !== '' && remainingOp ? usdToQuota(remVal) : 0,
      username: props.username.trim(),
    })
  }
  const handleReset = () => {
    setLastConsumedStart(undefined)
    setLastConsumedEnd(undefined)
    setRemainingOp('>=')
    setRemainingValue('')
    props.onUsernameChange('')
    setAppliedQuery({
      last_consumed_start: 0,
      last_consumed_end: 0,
      remaining_op: '',
      remaining_value: 0,
      username: '',
    })
  }

  const handleSort = (key: SummarySortKey) => {
    if (sortBy === key) {
      setSortOrder((o) => (o === 'desc' ? 'asc' : 'desc'))
      return
    }
    setSortBy(key)
    setSortOrder('desc')
  }
  const SortIcon = ({ col }: { col: SummarySortKey }) => {
    if (sortBy !== col)
      return (
        <ArrowUpDown className='text-muted-foreground/40 ml-1 inline size-3.5' />
      )
    return sortOrder === 'desc' ? (
      <ArrowDown className='ml-1 inline size-3.5' />
    ) : (
      <ArrowUp className='ml-1 inline size-3.5' />
    )
  }

  return (
    <>
      <div className='flex flex-wrap items-end gap-3'>
        <FilterField label={t('Last Consumed Date')}>
          <div className='flex items-center gap-1'>
            <DatePicker
              selected={lastConsumedStart}
              onSelect={setLastConsumedStart}
              placeholder={t('Start date')}
            />
            <span className='text-muted-foreground text-sm'>~</span>
            <DatePicker
              selected={lastConsumedEnd}
              onSelect={setLastConsumedEnd}
              placeholder={t('End date')}
            />
          </div>
        </FilterField>
        <FilterField label={t('Remaining Balance')}>
          <div className='flex items-center gap-1'>
            <Select
              items={[
                { value: '>=', label: '>=' },
                { value: '<=', label: '<=' },
                { value: '=', label: '=' },
              ]}
              value={remainingOp}
              onValueChange={(v) => v !== null && setRemainingOp(v)}
            >
              <SelectTrigger className='w-[80px]'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='>='>&gt;=</SelectItem>
                <SelectItem value='<='>&lt;=</SelectItem>
                <SelectItem value='='>=</SelectItem>
              </SelectContent>
            </Select>
            <Input
              type='number'
              placeholder={t('Please enter')}
              value={remainingValue}
              onChange={(e) => setRemainingValue(e.target.value)}
              className='w-[140px]'
            />
          </div>
        </FilterField>
        <FilterField label={t('Username')}>
          <Input
            placeholder={t('Please enter')}
            value={props.username}
            onChange={(e) => props.onUsernameChange(e.target.value)}
            className='w-[180px]'
          />
        </FilterField>
        <Button size='sm' onClick={handleQuery}>
          {t('Query')}
        </Button>
        <Button size='sm' variant='outline' onClick={handleReset}>
          {t('Reset')}
        </Button>
        <div className='ms-auto'>
          <ModeSwitch mode={props.mode} onChange={props.onModeChange} />
        </div>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead
              className='cursor-pointer select-none'
              onClick={() => handleSort('username')}
            >
              {t('Invited User')}
              <SortIcon col='username' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none'
              onClick={() => handleSort('created_at')}
            >
              {t('Created At')}
              <SortIcon col='created_at' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none'
              onClick={() => handleSort('last_consumed_at')}
            >
              {t('Last Consumed Date')}
              <SortIcon col='last_consumed_at' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_requests')}
            >
              {t('Total Requests')}
              <SortIcon col='total_requests' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_consumed')}
            >
              {t('Total Consumed ($)')}
              <SortIcon col='total_consumed' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_recharge_cny')}
            >
              {t('Total Recharge (¥)')}
              <SortIcon col='total_recharge_cny' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_tokens')}
            >
              {t('Total Tokens')}
              <SortIcon col='total_tokens' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('current_remaining')}
            >
              {t('Current Balance ($)')}
              <SortIcon col='current_remaining' />
            </TableHead>
            <TableHead className='text-center'>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(data ?? []).map((r: InviterSummaryRow) => (
            <TableRow key={r.user_id}>
              <TableCell className='font-medium'>{r.username}</TableCell>
              <TableCell className='text-sm'>
                {r.created_at
                  ? dayjs.unix(r.created_at).format('YYYY-MM-DD HH:mm')
                  : '-'}
              </TableCell>
              <TableCell className='text-sm'>
                {r.last_consumed_at
                  ? dayjs.unix(r.last_consumed_at).format('YYYY-MM-DD HH:mm')
                  : '-'}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                {r.total_requests.toLocaleString()}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                {formatCurrencyFromUSD(quotaToUsd(r.total_consumed))}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                ¥{(r.total_recharge_cny ?? 0).toFixed(2)}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                {r.total_tokens.toLocaleString()}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                {formatCurrencyFromUSD(quotaToUsd(r.current_remaining))}
              </TableCell>
              <TableCell className='text-center'>
                <Button
                  size='sm'
                  variant='ghost'
                  onClick={() => props.onSwitchDaily(r.username)}
                >
                  {t('View Detail')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
          {!isLoading && (data ?? []).length === 0 && (
            <TableRow>
              <TableCell
                colSpan={9}
                className='text-muted-foreground text-center'
              >
                {t('No data')}
              </TableCell>
            </TableRow>
          )}
          {isLoading && (
            <TableRow>
              <TableCell
                colSpan={9}
                className='text-muted-foreground text-center'
              >
                {t('Loading...')}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </>
  )
}

// ----------- 按天视图 -----------
type DailySortKey =
  | 'date'
  | 'username'
  | 'total_requests'
  | 'total_consumed'
  | 'total_recharge_cny'
  | 'total_tokens'

function DailyView(props: {
  username: string
  onUsernameChange: (v: string) => void
  mode: DetailMode
  onModeChange: (m: DetailMode) => void
}) {
  const { t } = useTranslation()
  // 默认"今天"（Q3=C）
  const initToday = dayjs().startOf('day').toDate()
  const [dateStart, setDateStart] = useState<Date | undefined>(initToday)
  const [dateEnd, setDateEnd] = useState<Date | undefined>(new Date())
  // 表头排序：默认按 date 降序，与后端默认一致
  const [sortBy, setSortBy] = useState<DailySortKey>('date')
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc')

  const [appliedQuery, setAppliedQuery] = useState<{
    start_timestamp: number
    end_timestamp: number
    username: string
  }>({
    start_timestamp: dayjs(initToday).unix(),
    end_timestamp: dayjs().unix(),
    username: props.username,
  })

  const { data, isLoading } = useQuery({
    queryKey: [
      'inviter-stats',
      'daily',
      appliedQuery,
      sortBy,
      sortOrder,
    ],
    queryFn: () =>
      getInviterDaily({
        ...appliedQuery,
        sort_by: sortBy,
        sort_order: sortOrder,
      }),
    select: (res) => (res.success ? res.data : []),
    staleTime: 30_000,
  })

  const handleQuery = () => {
    setAppliedQuery({
      start_timestamp: dateStart ? dayjs(dateStart).startOf('day').unix() : 0,
      end_timestamp: dateEnd ? dayjs(dateEnd).endOf('day').unix() : 0,
      username: props.username.trim(),
    })
  }
  const handleReset = () => {
    setDateStart(initToday)
    setDateEnd(new Date())
    props.onUsernameChange('')
    setAppliedQuery({
      start_timestamp: dayjs(initToday).unix(),
      end_timestamp: dayjs().unix(),
      username: '',
    })
  }

  const handleSort = (key: DailySortKey) => {
    if (sortBy === key) {
      setSortOrder((o) => (o === 'desc' ? 'asc' : 'desc'))
      return
    }
    setSortBy(key)
    setSortOrder('desc')
  }
  const SortIcon = ({ col }: { col: DailySortKey }) => {
    if (sortBy !== col)
      return (
        <ArrowUpDown className='text-muted-foreground/40 ml-1 inline size-3.5' />
      )
    return sortOrder === 'desc' ? (
      <ArrowDown className='ml-1 inline size-3.5' />
    ) : (
      <ArrowUp className='ml-1 inline size-3.5' />
    )
  }

  return (
    <>
      <div className='flex flex-wrap items-end gap-3'>
        <FilterField label={t('Date')}>
          <div className='flex items-center gap-1'>
            <DatePicker
              selected={dateStart}
              onSelect={setDateStart}
              placeholder={t('Start date')}
            />
            <span className='text-muted-foreground text-sm'>~</span>
            <DatePicker
              selected={dateEnd}
              onSelect={setDateEnd}
              placeholder={t('End date')}
            />
          </div>
        </FilterField>
        <FilterField label={t('Username')}>
          <Input
            placeholder={t('Please enter')}
            value={props.username}
            onChange={(e) => props.onUsernameChange(e.target.value)}
            className='w-[180px]'
          />
        </FilterField>
        <Button size='sm' onClick={handleQuery}>
          {t('Query')}
        </Button>
        <Button size='sm' variant='outline' onClick={handleReset}>
          {t('Reset')}
        </Button>
        <div className='ms-auto'>
          <ModeSwitch mode={props.mode} onChange={props.onModeChange} />
        </div>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead
              className='cursor-pointer select-none'
              onClick={() => handleSort('date')}
            >
              {t('Date')}
              <SortIcon col='date' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none'
              onClick={() => handleSort('username')}
            >
              {t('Invited User')}
              <SortIcon col='username' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_requests')}
            >
              {t('Total Requests')}
              <SortIcon col='total_requests' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_consumed')}
            >
              {t('Total Consumed ($)')}
              <SortIcon col='total_consumed' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_recharge_cny')}
            >
              {t('Total Recharge (¥)')}
              <SortIcon col='total_recharge_cny' />
            </TableHead>
            <TableHead
              className='cursor-pointer select-none text-center'
              onClick={() => handleSort('total_tokens')}
            >
              {t('Total Tokens')}
              <SortIcon col='total_tokens' />
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(data ?? []).map((r: InviterDailyRow, i: number) => (
            <TableRow key={`${r.date}-${r.username}-${i}`}>
              <TableCell>{r.date}</TableCell>
              <TableCell className='font-medium'>{r.username}</TableCell>
              <TableCell className='text-center tabular-nums'>
                {r.total_requests.toLocaleString()}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                {formatCurrencyFromUSD(quotaToUsd(r.total_consumed))}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                ¥{(r.total_recharge_cny ?? 0).toFixed(2)}
              </TableCell>
              <TableCell className='text-center tabular-nums'>
                {r.total_tokens.toLocaleString()}
              </TableCell>
            </TableRow>
          ))}
          {!isLoading && (data ?? []).length === 0 && (
            <TableRow>
              <TableCell
                colSpan={6}
                className='text-muted-foreground text-center'
              >
                {t('No data')}
              </TableCell>
            </TableRow>
          )}
          {isLoading && (
            <TableRow>
              <TableCell
                colSpan={6}
                className='text-muted-foreground text-center'
              >
                {t('Loading...')}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </>
  )
}

function FilterField(props: { label: string; children: React.ReactNode }) {
  return (
    <div className='flex flex-col gap-1'>
      <Label className='text-muted-foreground text-xs'>{props.label}</Label>
      {props.children}
    </div>
  )
}
