/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Settings as SettingsIcon } from 'lucide-react'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'

// ============================
// 类型定义
// ============================

type Range = '30m' | '1h' | '6h' | '24h' | '48h'

interface OverviewTotal {
  req_total: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  avg_ttft_ms: number
  ttft_p50_ms: number
  ttft_p95_ms: number
  slow_ttft_rate: number
}
interface OverviewPlatform {
  channel_type: number
  platform_name: string
  req_total: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  slow_ttft_rate?: number
}
interface OverviewResult {
  total: OverviewTotal
  platforms: OverviewPlatform[]
}

interface UserRow {
  user_id: number
  username: string
  platforms: string[]
  req_total: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  slow_ttft_rate: number
}

interface ChannelRow {
  channel_id: number
  channel_name: string
  req_total: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  slow_ttft_rate: number
}

interface ModelRow {
  model_name: string
  req_total: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  slow_ttft_rate: number
}

interface ErrorTopRow {
  error_code: string
  err_count: number
  avg_duration_ms: number
  channel_types: number[]
  platform_names: string[]
  sample_message: string
}

interface TrendPoint {
  ts: number
  req_total: number
  err_count: number
  error_rate: number
  avg_duration_ms: number
}
interface TrendResult {
  bucket_seconds: number
  series: TrendPoint[]
}

interface MetricsSettings {
  slow_response_ms: number
  slow_ttft_ms: number
  business_error_keywords: string[]
  log_retention_days: number
  writer_written: number
  writer_dropped: number
  writer_buffer_used: number
}

interface AlertRule {
  id: number
  name: string
  platforms: string
  metric: string
  operator: string
  threshold: number
  sustained_minutes: number
  cooldown_minutes: number
  tg_bot_token: string
  tg_chat_id: string
  enabled: boolean
  created_at: number
  updated_at: number
}

const RANGES: Range[] = ['30m', '1h', '6h', '24h', '48h']

function rangeKey(r: Range) {
  return {
    '30m': 'Last 30 minutes',
    '1h': 'Last 1 hour',
    '6h': 'Last 6 hours',
    '24h': 'Last 24 hours',
    '48h': 'Last 48 hours',
  }[r]
}

function fmtPct(v: number) {
  return ((v ?? 0) * 100).toFixed(2) + '%'
}
function fmtMs(v: number) {
  return (v ?? 0).toLocaleString() + ' ms'
}
function fmtNum(v: number) {
  return (v ?? 0).toLocaleString()
}

// ============================
// 主组件:页面布局对齐原型
//   - 顶部:标题 + 时间筛选 + 设置按钮
//   - 第一行:汇总卡片(总览 + 各平台子卡,横向排列)
//   - 第二行:折线图(每个汇总对应一个)
//   - 第三部分:客户请求-响应明细表(支持平台筛选 + 点击行下钻)
//   - 设置 Dialog:阈值/过滤词/告警规则
// ============================

export function RequestAnalytics() {
  const { t } = useTranslation()
  const [range, setRange] = useState<Range>('1h')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [drilldownUserId, setDrilldownUserId] = useState<number | null>(null)
  const [drilldownUsername, setDrilldownUsername] = useState<string>('')

  return (
    <div className='space-y-4'>
      {/* Header: 仅时间筛选 + 设置按钮(标题由 dashboard 外层渲染,避免重复) */}
      <div className='flex flex-wrap items-center justify-end gap-2'>
        <div className='flex items-center gap-1'>
          {RANGES.map((r) => (
            <Button
              key={r}
              size='sm'
              variant={range === r ? 'default' : 'outline'}
              onClick={() => setRange(r)}
            >
              {t(rangeKey(r))}
            </Button>
          ))}
        </div>
        <Button
          size='sm'
          variant='outline'
          onClick={() => setSettingsOpen(true)}
        >
          <SettingsIcon className='size-4 me-1' />
          {t('Settings')}
        </Button>
      </div>

      {/* 汇总卡片 + 折线图(总览 + 各平台并排) */}
      <SummarySection range={range} />

      {/* 客户请求-响应明细 */}
      <UsersTable
        range={range}
        onDrill={(uid, username) => {
          setDrilldownUserId(uid)
          setDrilldownUsername(username)
        }}
      />

      {/* 设置 Dialog */}
      <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />

      {/* 客户下钻 Dialog(点击客户表行) */}
      <DrilldownDialog
        userId={drilldownUserId}
        username={drilldownUsername}
        range={range}
        onClose={() => {
          setDrilldownUserId(null)
          setDrilldownUsername('')
        }}
      />
    </div>
  )
}

// ============================
// 汇总卡片 + 折线图(对齐原型)
//   - 第一行:3 个大卡片(总览/OpenAI/Anthropic),每卡内含 10 项指标 3 列网格
//   - 第二行:4 张图(top10 失败分布 + 3 张折线)
// ============================

interface SummaryMetrics {
  req_total: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  avg_ttft_ms: number
  ttft_p50_ms: number
  ttft_p95_ms: number
  slow_ttft_rate: number
}

function SummarySection({ range }: { range: Range }) {
  const { t } = useTranslation()
  const [data, setData] = useState<OverviewResult | null>(null)
  const [loading, setLoading] = useState(false)
  // 选中的 channel_type:0 表示总览(不过滤),其它为 channel_type
  const [selectedChannelType, setSelectedChannelType] = useState<number>(0)

  useEffect(() => {
    setLoading(true)
    api
      .get('/api/metrics/overview', { params: { range } })
      .then((res) => setData(res.data?.data ?? null))
      .catch(() => toast.error(t('Failed to load summary data')))
      .finally(() => setLoading(false))
  }, [range, t])

  if (loading && !data) return <div className='text-sm text-muted-foreground'>{t('Loading')}…</div>
  if (!data) return <div className='text-sm text-muted-foreground'>{t('No Data (Stats)')}</div>

  const total = data.total ?? ({} as OverviewTotal)
  const platforms = data.platforms ?? []
  const platformMap = new Map<number, OverviewPlatform>(
    platforms.map((p) => [p.channel_type, p])
  )

  // 固定置顶的平台(无数据时显示 0)
  const PINNED_PLATFORMS: Array<{ type: number; name: string }> = [
    { type: 1, name: 'OpenAI' },     // constant.ChannelTypeOpenAI
    { type: 14, name: 'Anthropic' }, // constant.ChannelTypeAnthropic
  ]
  const pinnedTypes = new Set(PINNED_PLATFORMS.map((p) => p.type))

  const zeroMetrics: SummaryMetrics = {
    req_total: 0,
    avg_duration_ms: 0,
    p50_ms: 0,
    p95_ms: 0,
    error_rate: 0,
    slow_resp_rate: 0,
    avg_ttft_ms: 0,
    ttft_p50_ms: 0,
    ttft_p95_ms: 0,
    slow_ttft_rate: 0,
  }

  const platformToMetrics = (p: OverviewPlatform): SummaryMetrics => ({
    req_total: p.req_total,
    avg_duration_ms: p.avg_duration_ms,
    p50_ms: p.p50_ms,
    p95_ms: p.p95_ms,
    error_rate: p.error_rate,
    slow_resp_rate: p.slow_resp_rate,
    avg_ttft_ms: 0,
    ttft_p50_ms: 0,
    ttft_p95_ms: 0,
    slow_ttft_rate: 0,
  })

  // 卡片 = 总览 + 固定平台(OpenAI/Anthropic,缺数据填 0) + 其他平台(若有数据)
  // channelType: 0 表示总览(不过滤),其它为 channel_type 值
  const cards: Array<{
    key: string
    title: string
    metrics: SummaryMetrics
    channelType: number
  }> = [
    {
      key: 'total',
      title: t('Total Requests'),
      metrics: total,
      channelType: 0,
    },
    ...PINNED_PLATFORMS.map(({ type, name }) => {
      const p = platformMap.get(type)
      return {
        key: `p-${type}`,
        title: name + ' ' + t('Total Requests'),
        metrics: p ? platformToMetrics(p) : zeroMetrics,
        channelType: type,
      }
    }),
    ...platforms
      .filter((p) => !pinnedTypes.has(p.channel_type))
      .map((p) => ({
        key: `p-${p.channel_type}`,
        title: p.platform_name + ' ' + t('Total Requests'),
        metrics: platformToMetrics(p),
        channelType: p.channel_type,
      })),
  ]

  return (
    <div className='space-y-3'>
      {/* 第一行:汇总卡片 */}
      <div
        className='grid gap-3'
        style={{ gridTemplateColumns: `repeat(${Math.max(cards.length, 1)}, minmax(0, 1fr))` }}
      >
        {cards.map((c) => (
          <SummaryCard
            key={c.key}
            title={c.title}
            metrics={c.metrics}
            selected={selectedChannelType === c.channelType}
            onClick={() => setSelectedChannelType(c.channelType)}
          />
        ))}
      </div>

      {/* 第二行:4 张图 — 失败 top10 + 错误折线 + 请求量折线 + 平均响应时长折线 */}
      <div className='grid grid-cols-1 gap-3 lg:grid-cols-4'>
        <ErrorTopList range={range} channelType={selectedChannelType} />
        <SingleTrendChart range={range} channelType={selectedChannelType} metric='err_count' title={t('Request Errors (count)')} color='#ef4444' />
        <SingleTrendChart range={range} channelType={selectedChannelType} metric='req_total' title={t('Request Count (Stats)')} color='#ef4444' />
        <SingleTrendChart range={range} channelType={selectedChannelType} metric='avg_duration_ms' title={t('Average Response Time')} color='#ef4444' />
      </div>
    </div>
  )
}

function SummaryCard({
  title,
  metrics,
  selected,
  onClick,
}: {
  title: string
  metrics: SummaryMetrics
  selected?: boolean
  onClick?: () => void
}) {
  const { t } = useTranslation()
  return (
    <Card
      onClick={onClick}
      className={cn(
        onClick && 'cursor-pointer transition-colors hover:bg-muted/40',
        selected && 'ring-2 ring-primary ring-offset-1',
      )}
    >
      <CardHeader className='pb-1'>
        <CardTitle className='text-sm font-medium text-muted-foreground'>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className='text-3xl font-semibold'>{fmtNum(metrics.req_total)}</div>
        <div className='mt-3 grid grid-cols-3 gap-x-3 gap-y-1.5 text-xs'>
          <SmallStat label={t('Average Duration')} value={fmtMs(metrics.avg_duration_ms)} />
          <SmallStat label={t('Average TTFT (Stats)')} value={fmtMs(metrics.avg_ttft_ms)} />
          <SmallStat label={t('Slow Response Rate')} value={fmtPct(metrics.slow_resp_rate)} />
          <SmallStat label='P50' value={fmtMs(metrics.p50_ms)} />
          <SmallStat label='TTFT-P50' value={fmtMs(metrics.ttft_p50_ms)} />
          <SmallStat label={t('Slow TTFT Rate')} value={fmtPct(metrics.slow_ttft_rate)} />
          <SmallStat label='P95' value={fmtMs(metrics.p95_ms)} />
          <SmallStat label='TTFT-P95' value={fmtMs(metrics.ttft_p95_ms)} />
          <SmallStat label={t('Error Rate')} value={fmtPct(metrics.error_rate)} />
        </div>
      </CardContent>
    </Card>
  )
}

function SmallStat({ label, value }: { label: string; value: string }) {
  return (
    <div className='flex items-baseline gap-1 whitespace-nowrap'>
      <span className='text-muted-foreground'>{label}:</span>
      <span className='font-medium'>{value}</span>
    </div>
  )
}

// ============================
// 失败原因 top10 列表(对齐原型左下)
// ============================

function ErrorTopList({ range, channelType }: { range: Range; channelType: number }) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<ErrorTopRow[]>([])
  useEffect(() => {
    const params: Record<string, string | number> = { range, limit: 10 }
    if (channelType > 0) params.channel_type = channelType
    api
      .get('/api/metrics/errors/top', { params })
      .then((res) => setRows(res.data?.data ?? []))
      .catch(() => {})
  }, [range, channelType])
  const safeRows = rows ?? []
  return (
    <Card>
      <CardHeader className='pb-1'>
        <CardTitle className='text-sm font-medium text-muted-foreground'>
          {t('Request Failure Top 10')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {safeRows.length === 0 ? (
          <div className='flex h-[200px] items-center justify-center text-xs text-muted-foreground'>
            {t('No Data (Stats)')}
          </div>
        ) : (
          <div className='space-y-1.5'>
            {safeRows.map((r) => (
              <div
                key={r.error_code}
                className='flex items-center justify-between text-sm'
              >
                <span className='truncate' title={r.sample_message}>
                  {r.error_code}
                </span>
                <span className='ms-2 shrink-0 text-muted-foreground'>
                  {fmtNum(r.err_count)} {t('times')}
                </span>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// ============================
// 单指标折线图(SVG)
// ============================

function SingleTrendChart({
  range,
  channelType,
  metric,
  title,
  color,
}: {
  range: Range
  channelType: number
  metric: 'req_total' | 'err_count' | 'avg_duration_ms'
  title: string
  color: string
}) {
  const [data, setData] = useState<TrendResult | null>(null)
  useEffect(() => {
    const params: Record<string, string | number> = { range }
    if (channelType > 0) params.channel_type = channelType
    api
      .get('/api/metrics/trend', { params })
      .then((res) => setData(res.data?.data ?? null))
      .catch(() => {})
  }, [range, channelType])
  const points = data?.series ?? []

  const w = 360
  const h = 200
  const padLeft = 38
  const padRight = 12
  const padBottom = 28
  const padTop = 8
  const yBaseline = h - padBottom
  const innerW = w - padLeft - padRight
  const innerH = h - padTop - padBottom

  const values = points.map((p) => p[metric])
  const max = Math.max(...values, 1)

  // 无数据时:合成一个零值的占位线(基于 range 计算时间区间)
  const formatHM = (ts: number) => {
    const d = new Date(ts * 1000)
    return String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0')
  }

  // Y 轴刻度格式化(根据 metric 决定单位)
  const formatY = (v: number) => {
    if (metric === 'avg_duration_ms') {
      if (v >= 1000) return (v / 1000).toFixed(1) + 's'
      return Math.round(v) + 'ms'
    }
    if (v >= 1000) return (v / 1000).toFixed(1) + 'k'
    return String(Math.round(v))
  }

  // 取 0 / max/2 / max 三档刻度
  const yTicks = [0, max / 2, max]

  let path = ''
  let area = ''
  let labels: Array<{ ts: number; x: number }> = []

  if (points.length >= 2) {
    const xStep = innerW / (points.length - 1)
    const yOf = (v: number) => padTop + innerH - (v / max) * innerH
    path = points
      .map((p, i) => `${i === 0 ? 'M' : 'L'} ${padLeft + i * xStep} ${yOf(p[metric])}`)
      .join(' ')
    area =
      `M ${padLeft} ${yBaseline} ` +
      points.map((p, i) => `L ${padLeft + i * xStep} ${yOf(p[metric])}`).join(' ') +
      ` L ${padLeft + (points.length - 1) * xStep} ${yBaseline} Z`
    labels = [
      { ts: points[0].ts, x: padLeft },
      { ts: points[Math.floor(points.length / 2)].ts, x: padLeft + innerW / 2 },
      { ts: points[points.length - 1].ts, x: padLeft + innerW },
    ]
  } else {
    // 占位:画一条贴底的零线 + 当前时间区间的 X 轴标签
    const now = Math.floor(Date.now() / 1000)
    const spans: Record<Range, number> = {
      '30m': 30 * 60,
      '1h': 3600,
      '6h': 6 * 3600,
      '24h': 24 * 3600,
      '48h': 48 * 3600,
    }
    const span = spans[range]
    const start = now - span
    const mid = now - Math.floor(span / 2)
    path = `M ${padLeft} ${yBaseline} L ${padLeft + innerW} ${yBaseline}`
    area = ''
    labels = [
      { ts: start, x: padLeft },
      { ts: mid, x: padLeft + innerW / 2 },
      { ts: now, x: padLeft + innerW },
    ]
  }

  return (
    <Card>
      <CardHeader className='pb-1'>
        <CardTitle className='text-sm font-medium text-muted-foreground'>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <svg viewBox={`0 0 ${w} ${h}`} className='w-full'>
          {/* Y 轴刻度线 + 标签 */}
          {yTicks.map((v, i) => {
            const y = padTop + innerH - (v / max) * innerH
            return (
              <g key={i}>
                <line
                  x1={padLeft}
                  y1={y}
                  x2={padLeft + innerW}
                  y2={y}
                  stroke='currentColor'
                  strokeOpacity={i === 0 ? 0.2 : 0.08}
                  strokeWidth='1'
                  strokeDasharray={i === 0 ? '' : '2 3'}
                />
                <text
                  x={padLeft - 4}
                  y={y + 3}
                  fontSize='9'
                  textAnchor='end'
                  fill='currentColor'
                  opacity='0.55'
                >
                  {formatY(v)}
                </text>
              </g>
            )
          })}
          {area && <path d={area} fill={color} fillOpacity='0.12' />}
          <path
            d={path}
            fill='none'
            stroke={color}
            strokeWidth='1.5'
            strokeOpacity={points.length >= 2 ? 1 : 0.5}
          />
          {labels.map((l, i) => (
            <text
              key={i}
              x={l.x}
              y={h - 6}
              fontSize='10'
              textAnchor={i === 0 ? 'start' : i === labels.length - 1 ? 'end' : 'middle'}
              fill='currentColor'
              opacity='0.6'
            >
              {formatHM(l.ts)}
            </text>
          ))}
        </svg>
      </CardContent>
    </Card>
  )
}

// ============================
// Trend Chart(单个平台,SVG 折线)
// ============================

// ============================
// 客户请求-响应明细表(原型主下半部分)
// ============================

type UserSortKey =
  | 'req_total'
  | 'avg_duration_ms'
  | 'p50_ms'
  | 'p95_ms'
  | 'error_rate'
  | 'slow_resp_rate'
  | 'slow_ttft_rate'

function SortableHead({
  label,
  sortKey,
  current,
  order,
  onClick,
}: {
  label: string
  sortKey: UserSortKey
  current: UserSortKey | null
  order: 'asc' | 'desc' | null
  onClick: (key: UserSortKey) => void
}) {
  const active = current === sortKey && order !== null
  const indicator = active ? (order === 'asc' ? '▲' : '▼') : '↕'
  return (
    <TableHead
      onClick={() => onClick(sortKey)}
      className='cursor-pointer select-none text-right hover:bg-muted/50'
    >
      <span className='inline-flex items-center justify-end gap-1'>
        {label}
        <span className={active ? 'text-foreground' : 'text-muted-foreground opacity-50'}>
          {indicator}
        </span>
      </span>
    </TableHead>
  )
}

function UsersTable({
  range,
  onDrill,
}: {
  range: Range
  onDrill?: (userId: number, username: string) => void
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<UserRow[]>([])
  const [total, setTotal] = useState<number>(0)
  const [page, setPage] = useState<number>(1)
  const pageSize = 10
  const [loading, setLoading] = useState(false)
  // 文本输入(实时)与已提交筛选(失焦/平台改变时刷新)分离
  const [usernameInput, setUsernameInput] = useState<string>('')
  const [appliedUsername, setAppliedUsername] = useState<string>('')
  const [channelTypeFilter, setChannelTypeFilter] = useState<number>(0)
  const [platformsFromApi, setPlatformsFromApi] = useState<OverviewPlatform[]>([])
  const [sortKey, setSortKey] = useState<UserSortKey | null>('req_total')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc' | null>('desc')

  const handleSort = (key: UserSortKey) => {
    if (sortKey === key) {
      // 三态切换:desc → asc → 无
      if (sortOrder === 'desc') setSortOrder('asc')
      else if (sortOrder === 'asc') {
        setSortKey(null)
        setSortOrder(null)
      } else setSortOrder('desc')
    } else {
      setSortKey(key)
      setSortOrder('desc')
    }
  }

  // 切换 range/筛选时回到第一页
  useEffect(() => {
    setPage(1)
  }, [range, appliedUsername, channelTypeFilter])

  useEffect(() => {
    setLoading(true)
    const params: Record<string, string | number> = { range, page, size: pageSize }
    if (appliedUsername) params.username = appliedUsername
    if (channelTypeFilter > 0) params.channel_type = channelTypeFilter
    api
      .get('/api/metrics/users', { params })
      .then((res) => {
        const d = res.data?.data
        // 兼容两种返回:{ rows, total } 或直接 [] (旧版)
        if (Array.isArray(d)) {
          setRows(d)
          setTotal(d.length)
        } else {
          setRows(d?.rows ?? [])
          setTotal(d?.total ?? 0)
        }
      })
      .catch(() => toast.error(t('Failed to load user dimension')))
      .finally(() => setLoading(false))
  }, [range, appliedUsername, channelTypeFilter, page, t])

  // 平台下拉:走后端 /api/metrics/platforms 接口
  useEffect(() => {
    api
      .get('/api/metrics/platforms', { params: { range } })
      .then((res) => setPlatformsFromApi(res.data?.data ?? []))
      .catch(() => {})
  }, [range])

  const safeRows = rows ?? []

  // 排序(前端就近完成,不需要后端二次请求)
  const sorted = useMemo(() => {
    if (!sortKey || !sortOrder) return safeRows
    const arr = [...safeRows]
    arr.sort((a, b) => {
      const av = Number(a[sortKey] ?? 0)
      const bv = Number(b[sortKey] ?? 0)
      return sortOrder === 'asc' ? av - bv : bv - av
    })
    return arr
  }, [safeRows, sortKey, sortOrder])

  // 下拉选项:接口返回的平台 + 固定的 OpenAI/Anthropic(即使无数据也展示)
  type PlatformOption = { value: number; label: string }
  const platformOptions = useMemo<PlatformOption[]>(() => {
    const map = new Map<number, string>()
    ;(platformsFromApi ?? []).forEach((p) => {
      if (p.channel_type > 0 && p.platform_name) {
        map.set(p.channel_type, p.platform_name)
      }
    })
    // 固定补 OpenAI / Anthropic
    if (!map.has(1)) map.set(1, 'OpenAI')
    if (!map.has(14)) map.set(14, 'Anthropic')
    return Array.from(map.entries()).map(([value, label]) => ({ value, label }))
  }, [platformsFromApi])

  return (
    <Card>
      <CardHeader className='pb-2'>
        <CardTitle className='text-base'>{t('User Request Response Details')}</CardTitle>
        <div className='mt-3 flex flex-wrap items-center gap-2'>
          <Input
            className='h-8 w-48 text-sm'
            placeholder={t('Enter username')}
            value={usernameInput}
            onChange={(e) => setUsernameInput(e.target.value)}
            onBlur={() => {
              if (usernameInput !== appliedUsername) {
                setAppliedUsername(usernameInput)
              }
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                ;(e.target as HTMLInputElement).blur()
              }
            }}
          />
          <select
            className='h-8 rounded border bg-background px-2 text-sm'
            value={channelTypeFilter}
            onChange={(e) => setChannelTypeFilter(Number(e.target.value))}
          >
            <option value={0}>{t('All Platforms')}</option>
            {platformOptions.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </div>
      </CardHeader>
      <CardContent>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('User (Stats)')}</TableHead>
                <TableHead>{t('Platform')}</TableHead>
                <SortableHead
                  label={t('Request Count (Stats)')}
                  sortKey='req_total'
                  current={sortKey}
                  order={sortOrder}
                  onClick={handleSort}
                />
                <SortableHead
                  label={t('Average Request Response Duration (ms)')}
                  sortKey='avg_duration_ms'
                  current={sortKey}
                  order={sortOrder}
                  onClick={handleSort}
                />
                <SortableHead
                  label={t('Response P50 (ms)')}
                  sortKey='p50_ms'
                  current={sortKey}
                  order={sortOrder}
                  onClick={handleSort}
                />
                <SortableHead
                  label={t('Response P95 (ms)')}
                  sortKey='p95_ms'
                  current={sortKey}
                  order={sortOrder}
                  onClick={handleSort}
                />
                <SortableHead
                  label={t('Error Rate (Short)')}
                  sortKey='error_rate'
                  current={sortKey}
                  order={sortOrder}
                  onClick={handleSort}
                />
                <SortableHead
                  label={t('Slow Response Rate')}
                  sortKey='slow_resp_rate'
                  current={sortKey}
                  order={sortOrder}
                  onClick={handleSort}
                />
                <SortableHead
                  label={t('Slow TTFT Rate')}
                  sortKey='slow_ttft_rate'
                  current={sortKey}
                  order={sortOrder}
                  onClick={handleSort}
                />
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && (
                <TableRow>
                  <TableCell colSpan={9} className='text-center text-muted-foreground'>
                    {t('Loading')}…
                  </TableCell>
                </TableRow>
              )}
              {!loading && !sorted.length && (
                <TableRow>
                  <TableCell colSpan={9} className='text-center text-muted-foreground'>
                    {t('No Data (Stats)')}
                  </TableCell>
                </TableRow>
              )}
              {sorted.map((r) => (
                <TableRow
                  key={r.user_id}
                  className={onDrill ? 'cursor-pointer hover:bg-muted/40' : ''}
                  onClick={() => onDrill?.(r.user_id, r.username || `#${r.user_id}`)}
                >
                  <TableCell>{r.username || `#${r.user_id}`}</TableCell>
                  <TableCell>
                    <div className='flex flex-wrap gap-1'>
                      {(r.platforms ?? []).map((p) => (
                        <Badge key={p} variant='secondary'>
                          {p}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className='text-right font-medium'>
                    {fmtNum(r.req_total)}
                  </TableCell>
                  <TableCell className='text-right'>{fmtMs(r.avg_duration_ms)}</TableCell>
                  <TableCell className='text-right'>{fmtMs(r.p50_ms)}</TableCell>
                  <TableCell className='text-right'>{fmtMs(r.p95_ms)}</TableCell>
                  <TableCell className='text-right'>{fmtPct(r.error_rate)}</TableCell>
                  <TableCell className='text-right'>{fmtPct(r.slow_resp_rate)}</TableCell>
                  <TableCell className='text-right'>{fmtPct(r.slow_ttft_rate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <Pagination total={total} page={page} pageSize={pageSize} onChange={setPage} />
      </CardContent>
    </Card>
  )
}

// ============================
// 渠道-模型下钻 Dialog
// ============================

function DrilldownDialog({
  userId,
  username,
  range,
  onClose,
}: {
  userId: number | null
  username: string
  range: Range
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [platforms, setPlatforms] = useState<OverviewPlatform[]>([])
  const [overviewTotal, setOverviewTotal] = useState<OverviewTotal | null>(null)
  const [errors, setErrors] = useState<ErrorTopRow[]>([])
  const [allChannels, setAllChannels] = useState<Array<ChannelRow & { channel_type: number }>>([])
  const [expandedChannel, setExpandedChannel] = useState<number | null>(null)
  // 选中的 channel_type:0 表示全部(不过滤),其它为某个平台
  const [selectedChannelType, setSelectedChannelType] = useState<number>(0)

  const open = userId != null

  // 关闭/切换用户时重置选中态,避免下次打开沿用旧状态
  useEffect(() => {
    if (!open) {
      setSelectedChannelType(0)
      setExpandedChannel(null)
    }
  }, [open])
  useEffect(() => {
    setSelectedChannelType(0)
    setExpandedChannel(null)
  }, [userId])

  // 1. 用户级别总览 + 平台子卡
  useEffect(() => {
    if (!open || userId == null) return
    api
      .get('/api/metrics/overview', { params: { range, user_id: userId } })
      .then((res) => {
        setPlatforms(res.data?.data?.platforms ?? [])
        setOverviewTotal(res.data?.data?.total ?? null)
      })
      .catch(() => {})
  }, [open, userId, range])

  // 2. 用户级别错误明细(按选中平台过滤)
  useEffect(() => {
    if (!open || userId == null) return
    const params: Record<string, string | number> = { range, user_id: userId, limit: 10 }
    if (selectedChannelType > 0) params.channel_type = selectedChannelType
    api
      .get('/api/metrics/errors/top', { params })
      .then((res) => setErrors(res.data?.data ?? []))
      .catch(() => {})
  }, [open, userId, range, selectedChannelType])

  // 3. 用户所有渠道(从每个 platform 拉 channels 然后合并)
  useEffect(() => {
    if (!open || userId == null || platforms.length === 0) {
      setAllChannels([])
      return
    }
    Promise.all(
      platforms.map((p) =>
        api
          .get(`/api/metrics/platform/${p.channel_type}/channels`, {
            params: { range, user_id: userId },
          })
          .then((res) => {
            const rows: ChannelRow[] = res.data?.data ?? []
            return rows.map((r) => ({ ...r, channel_type: p.channel_type }))
          })
          .catch(() => [])
      )
    ).then((arrs) => {
      const flat = arrs.flat()
      flat.sort((a, b) => b.req_total - a.req_total)
      setAllChannels(flat)
    })
  }, [open, userId, range, platforms])

  // 渠道耗时明细:按选中平台过滤;切换平台后收起展开行,避免引用到隐藏渠道
  const filteredChannels = useMemo(() => {
    if (selectedChannelType <= 0) return allChannels
    return allChannels.filter((c) => c.channel_type === selectedChannelType)
  }, [allChannels, selectedChannelType])
  useEffect(() => {
    if (expandedChannel != null && !filteredChannels.some((c) => c.channel_id === expandedChannel)) {
      setExpandedChannel(null)
    }
  }, [filteredChannels, expandedChannel])

  // overview 卡里显示的"5300ms(P95)"用平台 P95 突出展示
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='flex max-h-[90vh] w-[95vw] max-w-[1600px] flex-col overflow-hidden sm:max-w-[1600px]'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <span>{username || `#${userId}`}</span>
            <Badge variant='secondary' className='text-xs'>
              {t('User (Stats)')}
            </Badge>
          </DialogTitle>
        </DialogHeader>
        <div className='flex-1 space-y-4 overflow-y-auto overflow-x-hidden pe-1'>
          {/* 平台子卡片(横向) — 严格对齐原型,可点击联动下方明细 */}
          {platforms.length > 0 && (
            <div
              className='grid gap-3'
              style={{ gridTemplateColumns: `repeat(${platforms.length}, minmax(0, 1fr))` }}
            >
              {platforms.map((p) => {
                const selected = selectedChannelType === p.channel_type
                return (
                  <Card
                    key={p.channel_type}
                    onClick={() =>
                      setSelectedChannelType(selected ? 0 : p.channel_type)
                    }
                    className={cn(
                      'cursor-pointer transition-colors hover:bg-muted/40',
                      selected && 'ring-2 ring-primary ring-offset-1',
                    )}
                  >
                    <CardHeader className='pb-1'>
                      <CardTitle className='flex items-center justify-between gap-1 text-sm font-medium'>
                        <span>{p.platform_name}</span>
                        <span className='flex items-center gap-1'>
                          <span className='rounded-full bg-blue-50 px-1.5 py-0.5 text-[10px] font-normal text-blue-600'>
                            {fmtPct(p.slow_ttft_rate ?? 0)} {t('Slow TTFT')}
                          </span>
                          <span className='rounded-full bg-orange-50 px-1.5 py-0.5 text-[10px] font-normal text-orange-600'>
                            {fmtPct(p.slow_resp_rate)} {t('Slow Response')}
                          </span>
                        </span>
                      </CardTitle>
                    </CardHeader>
                    <CardContent>
                      <div className='flex items-baseline gap-1'>
                        <span className='text-3xl font-semibold text-blue-600'>
                          {fmtNum(p.p95_ms)}
                        </span>
                        <span className='text-xs text-muted-foreground'>ms(P95)</span>
                      </div>
                      <div className='mt-2 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground'>
                        <span>{t('Average Duration')}: {fmtMs(p.avg_duration_ms)}</span>
                        <span>| {t('Request Times')}: {fmtNum(p.req_total)}</span>
                        <span>| {t('Error (Stats)')}: {fmtPct(p.error_rate)}</span>
                      </div>
                    </CardContent>
                  </Card>
                )
              })}
            </div>
          )}
          {platforms.length === 0 && overviewTotal && (
            <div className='text-sm text-muted-foreground'>{t('No Data (Stats)')}</div>
          )}

          {/* 错误明细 */}
          <div>
            <div className='mb-2 text-sm font-semibold'>{t('Error Details')}</div>
            {errors.length === 0 ? (
              <div className='rounded border bg-muted/30 px-3 py-2 text-xs text-muted-foreground'>
                {t('No Data (Stats)')}
              </div>
            ) : (
              <div className='space-y-1 rounded border'>
                {errors.map((e) => (
                  <div
                    key={e.error_code}
                    className='flex items-center justify-between border-b px-3 py-2 text-sm last:border-b-0'
                  >
                    <span className='truncate' title={e.sample_message}>
                      {e.error_code}
                    </span>
                    <span className='ms-2 shrink-0 text-muted-foreground'>
                      {fmtNum(e.err_count)} {t('times')}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* 渠道耗时明细 — 单行 | 分隔的字段名严格对齐原型 */}
          <div>
            <div className='mb-2 text-sm font-semibold'>{t('Channel Latency Details')}</div>
            {filteredChannels.length === 0 ? (
              <div className='rounded border bg-muted/30 px-3 py-2 text-xs text-muted-foreground'>
                {t('No Data (Stats)')}
              </div>
            ) : (
              <div className='space-y-2'>
                {filteredChannels.map((r) => (
                  <div key={r.channel_id} className='rounded border'>
                    <div className='flex items-center justify-between gap-3 px-3 py-2'>
                      <div className='min-w-0 flex-1'>
                        <div className='font-medium'>{r.channel_name || `#${r.channel_id}`}</div>
                        <div className='mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground'>
                          <span>{t('Average Duration')}: {fmtMs(r.avg_duration_ms)}</span>
                          <span>| {t('Request Duration P95')}: {fmtMs(r.p95_ms)}</span>
                          <span>| {t('Request Times')}: {fmtNum(r.req_total)}</span>
                          <span>| {t('Error (Stats)')}: {fmtPct(r.error_rate)}</span>
                          <span>| {t('Slow TTFT')}: {fmtPct(r.slow_ttft_rate)}</span>
                          <span>| {t('Slow Response')}: {fmtPct(r.slow_resp_rate)}</span>
                        </div>
                      </div>
                      <button
                        type='button'
                        className='shrink-0 text-sm text-blue-600 hover:underline'
                        onClick={() =>
                          setExpandedChannel(expandedChannel === r.channel_id ? null : r.channel_id)
                        }
                      >
                        {expandedChannel === r.channel_id ? t('Collapse') : t('Expand')}
                      </button>
                    </div>
                    {expandedChannel === r.channel_id && (
                      <div className='border-t bg-muted/20 p-3'>
                        <UserChannelModels
                          channelId={r.channel_id}
                          userId={userId ?? 0}
                          range={range}
                        />
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button onClick={onClose}>{t('Close')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// 渠道展开的模型表格(带搜索 + 内存分页)
function UserChannelModels({
  channelId,
  userId,
  range,
}: {
  channelId: number
  userId: number
  range: Range
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<ModelRow[]>([])
  const [search, setSearch] = useState<string>('')
  const [page, setPage] = useState<number>(1)
  const pageSize = 10

  useEffect(() => {
    api
      .get(`/api/metrics/channel/${channelId}/models`, {
        params: { range, user_id: userId || undefined },
      })
      .then((res) => setRows(res.data?.data ?? []))
      .catch(() => {})
    setPage(1)
  }, [channelId, userId, range])

  const filtered = useMemo(() => {
    const safe = rows ?? []
    if (!search) return safe
    const q = search.toLowerCase()
    return safe.filter((r) => (r.model_name ?? '').toLowerCase().includes(q))
  }, [rows, search])

  const pageRows = filtered.slice((page - 1) * pageSize, page * pageSize)

  return (
    <div className='space-y-2'>
      <Input
        className='h-8 w-48 text-sm'
        placeholder={t('Enter model name')}
        value={search}
        onChange={(e) => {
          setSearch(e.target.value)
          setPage(1)
        }}
      />
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Model')}</TableHead>
            <TableHead className='text-right'>{t('Request Count (Stats)')}</TableHead>
            <TableHead className='text-right'>{t('Average Request Response Duration (ms)')}</TableHead>
            <TableHead className='text-right'>{t('P50 Short (ms)')}</TableHead>
            <TableHead className='text-right'>{t('P95 Short (ms)')}</TableHead>
            <TableHead className='text-right'>{t('Error Rate (Short)')}</TableHead>
            <TableHead className='text-right'>{t('Slow Response Rate (Short)')}</TableHead>
            <TableHead className='text-right'>{t('Slow TTFT Rate (Short)')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {pageRows.length === 0 && (
            <TableRow>
              <TableCell colSpan={8} className='text-center text-muted-foreground'>
                {t('No Data (Stats)')}
              </TableCell>
            </TableRow>
          )}
          {pageRows.map((r) => (
            <TableRow key={r.model_name}>
              <TableCell>{r.model_name}</TableCell>
              <TableCell className='text-right'>{fmtNum(r.req_total)}</TableCell>
              <TableCell className='text-right'>{fmtMs(r.avg_duration_ms)}</TableCell>
              <TableCell className='text-right'>{fmtMs(r.p50_ms)}</TableCell>
              <TableCell className='text-right'>{fmtMs(r.p95_ms)}</TableCell>
              <TableCell className='text-right'>{fmtPct(r.error_rate)}</TableCell>
              <TableCell className='text-right'>{fmtPct(r.slow_resp_rate)}</TableCell>
              <TableCell className='text-right'>{fmtPct(r.slow_ttft_rate)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {filtered.length > pageSize && (
        <Pagination
          total={filtered.length}
          page={page}
          pageSize={pageSize}
          onChange={setPage}
        />
      )}
    </div>
  )
}

// ============================
// 通用分页组件 — 样式对齐原型("总共 N 条" + 页码按钮)
// ============================

function Pagination({
  total,
  page,
  pageSize,
  onChange,
}: {
  total: number
  page: number
  pageSize: number
  onChange: (p: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  if (total <= pageSize) return null

  // 取要展示的页码(最多 5 个 — 围绕当前页)
  const pages: number[] = []
  const maxButtons = 5
  let start = Math.max(1, page - 2)
  let end = Math.min(totalPages, start + maxButtons - 1)
  if (end - start + 1 < maxButtons) {
    start = Math.max(1, end - maxButtons + 1)
  }
  for (let i = start; i <= end; i++) pages.push(i)

  const btnBase =
    'flex h-7 min-w-7 items-center justify-center rounded border bg-background px-2 text-xs hover:bg-muted disabled:opacity-40 disabled:hover:bg-background'

  return (
    <div className='flex items-center justify-end gap-3 pt-2 text-xs text-muted-foreground'>
      <span>
        {t('Total')} {total} {t('rows')}
      </span>
      <div className='flex items-center gap-1'>
        <button
          type='button'
          className={btnBase}
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
        >
          ‹
        </button>
        {pages.map((p) => (
          <button
            key={p}
            type='button'
            className={
              p === page
                ? 'flex h-7 min-w-7 items-center justify-center rounded border border-blue-500 bg-blue-500 px-2 text-xs text-white'
                : btnBase
            }
            onClick={() => onChange(p)}
          >
            {p}
          </button>
        ))}
        <button
          type='button'
          className={btnBase}
          disabled={page >= totalPages}
          onClick={() => onChange(page + 1)}
        >
          ›
        </button>
      </div>
    </div>
  )
}

// ============================
// 设置 Dialog(对齐原型左上)
// 布局:
//   响应慢请求耗时定义(ms)
//   首字慢请求耗时定义(ms)
//   过滤错误原因
//   告警设置
//     - 告警机器人 TOKEN
//     - 告警群 ID
//     - 告警条件: + 新增告警
//       告警规则列表(已有)
// ============================

interface MetricsSettingsExt extends MetricsSettings {
  alert_tg_bot_token?: string
  alert_tg_chat_id?: string
}

function SettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { t } = useTranslation()
  const [data, setData] = useState<MetricsSettingsExt | null>(null)
  const [slowResp, setSlowResp] = useState('')
  const [slowTtft, setSlowTtft] = useState('')
  const [keywords, setKeywords] = useState('')
  const [tgToken, setTgToken] = useState('')
  const [tgChatId, setTgChatId] = useState('')
  const [rules, setRules] = useState<AlertRule[]>([])
  const [editingRule, setEditingRule] = useState<AlertRule | null>(null)
  const [ruleDialogOpen, setRuleDialogOpen] = useState(false)

  const loadAll = () => {
    api
      .get('/api/metrics/settings')
      .then((res) => {
        const d: MetricsSettingsExt | undefined = res.data?.data
        if (!d) return
        setData(d)
        setSlowResp(String(d.slow_response_ms ?? 1500))
        setSlowTtft(String(d.slow_ttft_ms ?? 1500))
        setKeywords((d.business_error_keywords ?? []).join('\n'))
        setTgToken(d.alert_tg_bot_token ?? '')
        setTgChatId(d.alert_tg_chat_id ?? '')
      })
      .catch(() => toast.error(t('Failed to load settings')))
    api
      .get('/api/metrics/alert-rules')
      .then((res) => setRules(res.data?.data ?? []))
      .catch(() => {})
  }

  useEffect(() => {
    if (open) loadAll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const save = () => {
    api
      .put('/api/metrics/settings', {
        slow_response_ms: parseInt(slowResp, 10),
        slow_ttft_ms: parseInt(slowTtft, 10),
        business_error_keywords: keywords.split('\n').map((s) => s.trim()).filter(Boolean),
        alert_tg_bot_token: tgToken,
        alert_tg_chat_id: tgChatId,
      })
      .then(() => {
        toast.success(t('Saved successfully'))
        onOpenChange(false)
      })
      .catch(() => toast.error(t('Save failed')))
  }

  const deleteRule = (id: number) => {
    if (!window.confirm(t('Confirm delete this rule?'))) return
    api
      .delete(`/api/metrics/alert-rules/${id}`)
      .then(() => {
        toast.success(t('Deleted (Stats)'))
        loadAll()
      })
      .catch(() => toast.error(t('Delete failed')))
  }

  // 格式化规则简述,对齐原型:"OpenAI平均响应时长大于1000ms,且持续 5 分钟时触发告警"
  const describeRule = (r: AlertRule) => {
    const metricLabel: Record<string, string> = {
      avg_duration_ms: t('Average Response Time'),
      slow_resp_rate: t('Slow Request Rate'),
      error_rate: t('Error Rate'),
    }
    const opLabel: Record<string, string> = {
      gt: t('Greater than'),
      eq: t('Equal'),
      gte: t('Greater or equal'),
      ge: t('Greater or equal'),
    }
    let platformsText = t('All')
    try {
      const arr = JSON.parse(r.platforms || '[]')
      if (Array.isArray(arr) && arr.length > 0) {
        platformsText = arr.map((id: number) => CHANNEL_TYPE_NAME[id] ?? `#${id}`).join('/')
      }
    } catch {
      // ignore
    }
    const unit =
      r.metric === 'avg_duration_ms' ? 'ms' : r.metric === 'error_rate' || r.metric === 'slow_resp_rate' ? '%' : ''
    return `${platformsText}${metricLabel[r.metric] ?? r.metric}${opLabel[r.operator] ?? r.operator}${r.threshold}${unit},${t('lasting')} ${r.sustained_minutes} ${t('minutes triggers alert')}`
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className='flex max-h-[85vh] w-[95vw] max-w-lg flex-col overflow-hidden sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('Settings')}</DialogTitle>
          </DialogHeader>
          {!data && <div className='text-sm text-muted-foreground'>{t('Loading')}…</div>}
          {data && (
            <div className='flex-1 space-y-4 overflow-y-auto overflow-x-hidden pe-1'>
              <FieldGroup label={'| ' + t('Slow Response Threshold (ms)') + ':'}>
                <Input
                  type='number'
                  value={slowResp}
                  onChange={(e) => setSlowResp(e.target.value)}
                />
              </FieldGroup>

              <FieldGroup label={'| ' + t('Slow TTFT Threshold (ms)') + ':'}>
                <Input
                  type='number'
                  value={slowTtft}
                  onChange={(e) => setSlowTtft(e.target.value)}
                />
              </FieldGroup>

              <FieldGroup label={'| ' + t('Filter Error Reasons')}>
                <textarea
                  className='w-full rounded border bg-background p-2 text-sm'
                  rows={4}
                  value={keywords}
                  onChange={(e) => setKeywords(e.target.value)}
                />
              </FieldGroup>

              {/* 告警设置分组 */}
              <div className='space-y-3 border-t pt-3'>
                <div className='text-sm font-semibold'>{t('Alert Settings')}</div>

                <FieldGroup label={t('Alert Bot TOKEN')}>
                  <Input value={tgToken} onChange={(e) => setTgToken(e.target.value)} />
                </FieldGroup>

                <FieldGroup label={t('Alert Group ID')}>
                  <Input value={tgChatId} onChange={(e) => setTgChatId(e.target.value)} />
                </FieldGroup>

                {/* 告警条件 + 列表 */}
                <div className='space-y-2'>
                  <div className='flex items-center gap-3 text-sm'>
                    <span>{t('Alert Conditions')}</span>
                    <button
                      type='button'
                      className='text-blue-600 hover:underline'
                      onClick={() => {
                        setEditingRule(null)
                        setRuleDialogOpen(true)
                      }}
                    >
                      +{t('New Alert')}
                    </button>
                  </div>
                  <div className='space-y-1'>
                    {(rules ?? []).map((r) => (
                      <div
                        key={r.id}
                        className='flex items-center justify-between gap-2 rounded border bg-muted/30 px-3 py-2 text-sm'
                      >
                        <div className='min-w-0 flex-1 truncate' title={describeRule(r)}>
                          {describeRule(r)}
                        </div>
                        <div className='ms-2 flex shrink-0 gap-2 text-blue-600'>
                          <button
                            type='button'
                            className='hover:underline'
                            onClick={() => {
                              setEditingRule(r)
                              setRuleDialogOpen(true)
                            }}
                          >
                            {t('Edit')}
                          </button>
                          <button
                            type='button'
                            className='hover:underline'
                            onClick={() => deleteRule(r.id)}
                          >
                            {t('Delete')}
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => onOpenChange(false)}>
              {t('Cancel')}
            </Button>
            <Button onClick={save} disabled={!data}>
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertRuleDialog
        rule={editingRule}
        open={ruleDialogOpen}
        onOpenChange={setRuleDialogOpen}
        onSaved={() => {
          setRuleDialogOpen(false)
          loadAll()
        }}
      />
    </>
  )
}

function FieldGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className='block text-sm font-medium'>{label}</label>
      <div className='mt-1'>{children}</div>
    </div>
  )
}

// 常用 channel type → 名称(用于规则简述展示;完整映射在后端)
const CHANNEL_TYPE_NAME: Record<number, string> = {
  1: 'OpenAI',
  3: 'Azure',
  14: 'Anthropic',
  24: 'Gemini',
  33: 'AWS',
  43: 'DeepSeek',
  48: 'xAI',
}

function AlertRuleDialog({
  rule,
  open,
  onOpenChange,
  onSaved,
}: {
  rule: AlertRule | null
  open: boolean
  onOpenChange: (o: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const defaults = {
    name: '',
    selectedPlatform: null as number | null,
    metric: 'avg_duration_ms',
    operator: 'gt',
    threshold: 1000,
    sustained_minutes: 5,
  }
  const [form, setForm] = useState(defaults)
  const [platformsFromApi, setPlatformsFromApi] = useState<OverviewPlatform[]>([])

  // 平台候选 — 后端 /api/metrics/platforms 实际有数据的平台 + 固定 OpenAI/Anthropic 兜底
  const PLATFORM_CANDIDATES = useMemo<Array<{ id: number; name: string }>>(() => {
    const map = new Map<number, string>()
    ;(platformsFromApi ?? []).forEach((p) => {
      if (p.channel_type > 0 && p.platform_name) {
        map.set(p.channel_type, p.platform_name)
      }
    })
    if (!map.has(1)) map.set(1, 'OpenAI')
    if (!map.has(14)) map.set(14, 'Anthropic')
    return Array.from(map.entries()).map(([id, name]) => ({ id, name }))
  }, [platformsFromApi])

  // Dialog 打开时拉一次平台列表(用 48h 窗口尽量全)
  useEffect(() => {
    if (!open) return
    api
      .get('/api/metrics/platforms', { params: { range: '48h' } })
      .then((res) => setPlatformsFromApi(res.data?.data ?? []))
      .catch(() => {})
  }, [open])

  useEffect(() => {
    if (rule) {
      let first: number | null = null
      try {
        const arr = JSON.parse(rule.platforms || '[]')
        if (Array.isArray(arr) && arr.length > 0) first = arr[0]
      } catch {
        // ignore
      }
      setForm({
        name: rule.name,
        selectedPlatform: first,
        metric: rule.metric,
        operator: rule.operator,
        threshold: rule.threshold,
        sustained_minutes: rule.sustained_minutes,
      })
    } else {
      setForm(defaults)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rule, open])

  // 指标单位:平均响应时长用 ms,慢率/错误率用 %
  const unit =
    form.metric === 'avg_duration_ms' ? 'ms' : form.metric === 'slow_resp_rate' || form.metric === 'error_rate' ? '%' : ''

  const save = () => {
    // 平台必填校验
    if (form.selectedPlatform == null) {
      toast.error(t('Please select a platform'))
      return
    }
    const platformName =
      PLATFORM_CANDIDATES.find((p) => p.id === form.selectedPlatform)?.name ?? `#${form.selectedPlatform}`
    const payload = {
      name: form.name || `[${platformName}] ${form.metric}`,
      platforms: JSON.stringify([form.selectedPlatform]),
      metric: form.metric,
      operator: form.operator,
      threshold: form.threshold,
      sustained_minutes: form.sustained_minutes,
      cooldown_minutes: rule?.cooldown_minutes ?? 30, // 沿用旧值或默认 30(原型未暴露)
      tg_bot_token: '', // 全局配置,不再每规则单独设
      tg_chat_id: '',
      enabled: rule?.enabled ?? true, // 默认启用,编辑时沿用原值
    }
    const url = rule ? `/api/metrics/alert-rules/${rule.id}` : '/api/metrics/alert-rules'
    const promise = rule ? api.put(url, payload) : api.post(url, payload)
    promise
      .then(() => {
        toast.success(t('Saved successfully'))
        onSaved()
      })
      .catch(() => toast.error(t('Save failed')))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='flex max-h-[85vh] w-[95vw] max-w-lg flex-col overflow-hidden sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{rule ? t('Edit Alert') : t('New Alert')}</DialogTitle>
        </DialogHeader>
        <div className='flex-1 space-y-3 overflow-y-auto overflow-x-hidden pe-1'>
          {/* 平台多选下拉 */}
          <FieldGroup label={t('Platform') + ':'}>
            <select
              className='h-9 w-full rounded border bg-background px-2 text-sm'
              value={form.selectedPlatform ?? ''}
              onChange={(e) =>
                setForm({
                  ...form,
                  selectedPlatform: e.target.value === '' ? null : Number(e.target.value),
                })
              }
            >
              <option value=''>{t('Please select')}</option>
              {PLATFORM_CANDIDATES.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </FieldGroup>

          {/* 告警条件:指标 + 比较 + 阈值 + 单位 */}
          <FieldGroup label={t('Alert Conditions') + ':'}>
            <div className='flex gap-2'>
              <select
                className='h-9 flex-1 rounded border bg-background px-2 text-sm'
                value={form.metric}
                onChange={(e) => setForm({ ...form, metric: e.target.value })}
              >
                <option value=''>{t('Please select')}</option>
                <option value='avg_duration_ms'>{t('Average Response Time')}</option>
                <option value='slow_resp_rate'>{t('Slow Request Rate')}</option>
                <option value='error_rate'>{t('Error Rate')}</option>
              </select>
              <select
                className='h-9 w-24 rounded border bg-background px-2 text-sm'
                value={form.operator}
                onChange={(e) => setForm({ ...form, operator: e.target.value })}
              >
                <option value='gt'>{t('Greater than')}</option>
                <option value='eq'>{t('Equal')}</option>
                <option value='gte'>{t('Greater or equal')}</option>
              </select>
              <div className='flex w-32 items-center rounded border bg-background pe-2'>
                <Input
                  type='number'
                  className='border-0 focus-visible:ring-0'
                  value={form.threshold}
                  onChange={(e) => setForm({ ...form, threshold: Number(e.target.value) })}
                />
                <span className='ms-1 shrink-0 text-xs text-muted-foreground'>{unit}</span>
              </div>
            </div>
          </FieldGroup>

          {/* 持续时长 */}
          <FieldGroup label={t('Duration (minutes)') + ':'}>
            <div className='flex items-center gap-2'>
              <Input
                type='number'
                className='w-32'
                value={form.sustained_minutes}
                onChange={(e) =>
                  setForm({ ...form, sustained_minutes: Number(e.target.value) })
                }
              />
              <span className='text-sm text-muted-foreground'>{t('minutes')}</span>
            </div>
          </FieldGroup>

        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={save}>{t('Save')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// 用 ErrorTopRow 占位避免类型未使用警告(预留给后续功能)
export type _ErrorTopRow = ErrorTopRow
