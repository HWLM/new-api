/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
import { useEffect, useMemo, useRef, useState } from 'react'
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
import {
  TimeRange,
  TimeRangeBar,
  defaultTimeRange,
  toWindowParams,
  validateTimeRange,
} from './time-range-bar'

// ============================
// 类型定义
// ============================

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
  err_count: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  avg_ttft_ms: number
  ttft_p50_ms: number
  ttft_p95_ms: number
  slow_ttft_rate?: number
}
interface OverviewResult {
  total: OverviewTotal
  platforms: OverviewPlatform[]
  compare_total?: OverviewTotal
  compare_platforms?: OverviewPlatform[]
}

interface UserCompare {
  req_total: number
  avg_duration_ms: number
  p50_ms: number
  p95_ms: number
  error_rate: number
  slow_resp_rate: number
  slow_ttft_rate: number
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
  compare?: UserCompare
}

interface ChannelCompare {
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
  compare?: ChannelCompare
}

interface ModelCompare {
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
  compare?: ModelCompare
}

interface ErrorTopRow {
  error_code: string
  err_count: number
  avg_duration_ms: number
  channel_types: number[]
  platform_names: string[]
  sample_message: string
  compare_count?: number
}

interface TrendPoint {
  ts: number
  req_total: number
  err_count: number
  slow_resp_count: number
  slow_ttft_count: number
  error_rate: number
  avg_duration_ms: number
}
interface TrendResult {
  bucket_seconds: number
  series: TrendPoint[]
  compare_series?: TrendPoint[]
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

function fmtPct(v: number) {
  return ((v ?? 0) * 100).toFixed(2) + '%'
}
function fmtMs(v: number) {
  return (v ?? 0).toLocaleString() + ' ms'
}
function fmtNum(v: number) {
  return (v ?? 0).toLocaleString()
}

// 同比变化百分比：(cur - base) / base * 100。
// 边界处理：
//   base == null（未开启对比）         → null，UI 显示 "—"
//   base === 0 && cur === 0              → 0，无变化
//   base === 0 && cur !== 0              → ±100%（从无到有约定 +100%；反向同理 -100%）
function pctDelta(cur: number, base: number | undefined | null): number | null {
  if (base == null) return null
  if (base === 0) {
    if (cur === 0) return 0
    return cur > 0 ? 100 : -100
  }
  return ((cur - base) / Math.abs(base)) * 100
}

// 颜色规则按用户确认：统一"上涨红 / 下降绿"，与指标好坏无关。
function deltaColorClass(deltaPct: number | null): string {
  if (deltaPct == null || deltaPct === 0) return 'text-muted-foreground'
  return deltaPct > 0 ? 'text-red-600' : 'text-green-600'
}

function fmtDelta(deltaPct: number | null): string {
  if (deltaPct == null) return '—'
  const sign = deltaPct > 0 ? '↑' : deltaPct < 0 ? '↓' : ''
  return `${sign}${Math.abs(deltaPct).toFixed(2)}%`
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
  const [timeRange, setTimeRange] = useState<TimeRange>(defaultTimeRange)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [drilldownUserId, setDrilldownUserId] = useState<number | null>(null)
  const [drilldownUsername, setDrilldownUsername] = useState<string>('')

  // 输入合法性：未通过校验时不下发 query，避免无效请求。
  const valid = useMemo(() => validateTimeRange(timeRange) == null, [timeRange])

  return (
    <div className='space-y-4'>
      {/* Header: 时间筛选 + 设置按钮(标题由 dashboard 外层渲染,避免重复) */}
      <div className='flex flex-wrap items-center justify-end gap-2'>
        <TimeRangeBar value={timeRange} onChange={setTimeRange} />
        <Button
          size='sm'
          variant='outline'
          onClick={() => setSettingsOpen(true)}
        >
          <SettingsIcon className='size-4 me-1' />
          {t('Settings')}
        </Button>
      </div>

      {!valid && (
        <div className='rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700'>
          {t('Invalid time range, please adjust')}
        </div>
      )}

      {/* 汇总卡片 + 折线图(总览 + 各平台并排) */}
      <SummarySection timeRange={timeRange} />

      {/* 客户请求-响应明细 */}
      <UsersTable
        timeRange={timeRange}
        onDrill={(uid, username) => {
          setDrilldownUserId(uid)
          setDrilldownUsername(username)
        }}
      />

      {/* 用户下钻弹窗(保持当前 UI,内部按 timeRange 取数;不展示对比) */}
      <DrilldownDialog
        userId={drilldownUserId}
        username={drilldownUsername}
        timeRange={timeRange}
        onClose={() => {
          setDrilldownUserId(null)
          setDrilldownUsername('')
        }}
      />

      {/* 设置 Dialog */}
      <SettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
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

function SummarySection({ timeRange }: { timeRange: TimeRange }) {
  const { t } = useTranslation()
  const [data, setData] = useState<OverviewResult | null>(null)
  const [loading, setLoading] = useState(false)
  // 选中的 channel_type:0 表示总览(不过滤),其它为 channel_type
  const [selectedChannelType, setSelectedChannelType] = useState<number>(0)
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])

  useEffect(() => {
    if (validateTimeRange(timeRange) != null) return
    setLoading(true)
    api
      .get('/api/metrics/overview', { params: winParams })
      .then((res) => setData(res.data?.data ?? null))
      .catch(() => toast.error(t('Failed to load summary data')))
      .finally(() => setLoading(false))
  }, [winParams, timeRange, t])

  if (loading && !data) return <div className='text-sm text-muted-foreground'>{t('Loading')}…</div>
  if (!data) return <div className='text-sm text-muted-foreground'>{t('No Data (Stats)')}</div>

  const total = data.total ?? ({} as OverviewTotal)
  const platforms = data.platforms ?? []
  const compareTotal = data.compare_total
  const comparePlatforms = data.compare_platforms ?? []
  const platformMap = new Map<number, OverviewPlatform>(
    platforms.map((p) => [p.channel_type, p])
  )
  const comparePlatformMap = new Map<number, OverviewPlatform>(
    comparePlatforms.map((p) => [p.channel_type, p])
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
    slow_ttft_rate: p.slow_ttft_rate ?? 0,
  })

  // 卡片 = 总览 + 固定平台(OpenAI/Anthropic,缺数据填 0) + 其他平台(若有数据)
  // channelType: 0 表示总览(不过滤),其它为 channel_type 值
  const cards: Array<{
    key: string
    title: string
    metrics: SummaryMetrics
    compare?: SummaryMetrics
    channelType: number
  }> = [
    {
      key: 'total',
      title: t('Total Requests'),
      metrics: total,
      compare: compareTotal,
      channelType: 0,
    },
    ...PINNED_PLATFORMS.map(({ type, name }) => {
      const p = platformMap.get(type)
      const cp = comparePlatformMap.get(type)
      return {
        key: `p-${type}`,
        title: name + ' ' + t('Total Requests'),
        metrics: p ? platformToMetrics(p) : zeroMetrics,
        compare: cp ? platformToMetrics(cp) : (timeRange.compareEnabled ? zeroMetrics : undefined),
        channelType: type,
      }
    }),
    ...platforms
      .filter((p) => !pinnedTypes.has(p.channel_type))
      .map((p) => {
        const cp = comparePlatformMap.get(p.channel_type)
        return {
          key: `p-${p.channel_type}`,
          title: p.platform_name + ' ' + t('Total Requests'),
          metrics: platformToMetrics(p),
          compare: cp ? platformToMetrics(cp) : (timeRange.compareEnabled ? zeroMetrics : undefined),
          channelType: p.channel_type,
        }
      }),
  ]

  // 拆分：总览卡片固定在左侧，平台卡片放进可横向滚动的容器
  // cards[0] 总是 total 卡（见上面 cards 数组构造）
  const [totalCard, ...platformCards] = cards
  const CARD_W = 'w-[460px]' // 单卡固定宽度；保留同步参数化便于以后改

  return (
    <div className='space-y-3'>
      {/* 第一行:汇总卡片 — 总览固定 + 平台横向滚动 */}
      <div className='flex gap-3'>
        <div className={cn(CARD_W, 'shrink-0')}>
          <SummaryCard
            key={totalCard.key}
            title={totalCard.title}
            metrics={totalCard.metrics}
            compare={totalCard.compare}
            selected={selectedChannelType === totalCard.channelType}
            onClick={() => setSelectedChannelType(totalCard.channelType)}
          />
        </div>
        <div className='min-w-0 flex-1 overflow-x-auto'>
          <div className='flex gap-3'>
            {platformCards.map((c) => (
              <div key={c.key} className={cn(CARD_W, 'shrink-0')}>
                <SummaryCard
                  title={c.title}
                  metrics={c.metrics}
                  compare={c.compare}
                  selected={selectedChannelType === c.channelType}
                  onClick={() => setSelectedChannelType(c.channelType)}
                />
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* 第二行:折线图(4 指标 Tab 切换 + 对比虚线 + 4 块汇总) */}
      <MetricTrendChart timeRange={timeRange} channelType={selectedChannelType} />

      {/* 第三行:左 Top10 + 右联动小图 */}
      <ErrorTopPanel timeRange={timeRange} channelType={selectedChannelType} />
    </div>
  )
}

function SummaryCard({
  title,
  metrics,
  compare,
  selected,
  onClick,
}: {
  title: string
  metrics: SummaryMetrics
  compare?: SummaryMetrics
  selected?: boolean
  onClick?: () => void
}) {
  const { t } = useTranslation()
  const headDelta = compare ? pctDelta(metrics.req_total, compare.req_total) : null
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
        <div className='flex items-baseline gap-2'>
          <div className='text-3xl font-semibold'>{fmtNum(metrics.req_total)}</div>
          {compare && (
            <span className={cn('text-xs font-medium', deltaColorClass(headDelta))}>
              {fmtDelta(headDelta)}
            </span>
          )}
        </div>
        {/*
         * 2 列 5 行排布：
         *   行 1: 平均请求时长 | 平均 TTFT 时长
         *   行 2: P50          | TTFT-P50
         *   行 3: P95          | TTFT-P95
         *   行 4: 响应慢请求率 | 首字慢请求率
         *   行 5: 请求错误率   | (空)
         * 左列时长系，右列对应 TTFT，最后一行错误率单列。
         */}
        <div className='mt-3 grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs'>
          <SmallStat
            label={t('Average Duration')}
            value={fmtMs(metrics.avg_duration_ms)}
            deltaPct={compare ? pctDelta(metrics.avg_duration_ms, compare.avg_duration_ms) : null}
          />
          <SmallStat
            label={t('Average TTFT (Stats)')}
            value={fmtMs(metrics.avg_ttft_ms)}
            deltaPct={compare ? pctDelta(metrics.avg_ttft_ms, compare.avg_ttft_ms) : null}
          />
          <SmallStat
            label='P50'
            value={fmtMs(metrics.p50_ms)}
            deltaPct={compare ? pctDelta(metrics.p50_ms, compare.p50_ms) : null}
          />
          <SmallStat
            label='TTFT-P50'
            value={fmtMs(metrics.ttft_p50_ms)}
            deltaPct={compare ? pctDelta(metrics.ttft_p50_ms, compare.ttft_p50_ms) : null}
          />
          <SmallStat
            label='P95'
            value={fmtMs(metrics.p95_ms)}
            deltaPct={compare ? pctDelta(metrics.p95_ms, compare.p95_ms) : null}
          />
          <SmallStat
            label='TTFT-P95'
            value={fmtMs(metrics.ttft_p95_ms)}
            deltaPct={compare ? pctDelta(metrics.ttft_p95_ms, compare.ttft_p95_ms) : null}
          />
          <SmallStat
            label={t('Slow Response Rate')}
            value={fmtPct(metrics.slow_resp_rate)}
            deltaPct={compare ? pctDelta(metrics.slow_resp_rate, compare.slow_resp_rate) : null}
          />
          <SmallStat
            label={t('Slow TTFT Rate')}
            value={fmtPct(metrics.slow_ttft_rate)}
            deltaPct={compare ? pctDelta(metrics.slow_ttft_rate, compare.slow_ttft_rate) : null}
          />
          <SmallStat
            label={t('Error Rate')}
            value={fmtPct(metrics.error_rate)}
            deltaPct={compare ? pctDelta(metrics.error_rate, compare.error_rate) : null}
          />
        </div>
      </CardContent>
    </Card>
  )
}

function SmallStat({ label, value, deltaPct }: { label: string; value: string; deltaPct?: number | null }) {
  return (
    // 单行不换行；卡片宽度已保证每个 cell 装得下；min-w-0 让 grid 允许收缩，
    // 真装不下时由父容器 overflow-hidden 截断（而不是溢出覆盖到相邻格）。
    <div className='flex min-w-0 items-baseline gap-1 overflow-hidden whitespace-nowrap'>
      <span className='text-muted-foreground'>{label}:</span>
      <span className='font-medium'>{value}</span>
      {deltaPct != null && (
        <span className={cn('text-[10px]', deltaColorClass(deltaPct))}>({fmtDelta(deltaPct)})</span>
      )}
    </div>
  )
}

// ============================
// 折线图(4 指标 Tab 切换 + 对比虚线 + 4 块汇总)
// ============================

type TrendMetric = 'req_total' | 'slow_resp_count' | 'slow_ttft_count' | 'avg_duration_ms'
// 折线图渲染组件可绘制的全部 metric（含错误码联动折线的 err_count）
type ChartMetric = TrendMetric | 'err_count'

const METRIC_LABEL_KEY: Record<TrendMetric, string> = {
  req_total: 'Request Count (Stats)',
  slow_resp_count: 'Slow Response Requests',
  slow_ttft_count: 'Slow TTFT Requests',
  avg_duration_ms: 'Average Response Time',
}

function MetricTrendChart({
  timeRange,
  channelType,
  userId,
}: {
  timeRange: TimeRange
  channelType: number
  userId?: number
}) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<TrendMetric>('req_total')
  const [data, setData] = useState<TrendResult | null>(null)
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])

  useEffect(() => {
    if (validateTimeRange(timeRange) != null) return
    const params: Record<string, string | number> = { ...winParams }
    if (channelType > 0) params.channel_type = channelType
    if (userId && userId > 0) params.user_id = userId
    api
      .get('/api/metrics/trend', { params })
      .then((res) => setData(res.data?.data ?? null))
      .catch(() => {})
  }, [winParams, timeRange, channelType, userId])

  return (
    <Card>
      <CardHeader className='flex flex-row items-center justify-between gap-2 pb-1'>
        <CardTitle className='text-sm font-medium text-muted-foreground'>
          {t('Trend')}
        </CardTitle>
        <div className='flex items-center gap-1 rounded bg-muted p-0.5'>
          {(Object.keys(METRIC_LABEL_KEY) as TrendMetric[]).map((m) => (
            <button
              key={m}
              type='button'
              className={cn(
                'rounded px-2.5 py-0.5 text-xs transition-colors',
                metric === m ? 'bg-background shadow-sm' : 'text-muted-foreground hover:text-foreground',
              )}
              onClick={() => setMetric(m)}
            >
              {t(METRIC_LABEL_KEY[m])}
            </button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        <TrendChartSVG metric={metric} series={data?.series ?? []} compareSeries={data?.compare_series} />
        <TrendSummaryRow metric={metric} series={data?.series ?? []} compareSeries={data?.compare_series} compareEnabled={timeRange.compareEnabled} />
      </CardContent>
    </Card>
  )
}

// 通用 SVG 折线渲染：series 主线（实线 #2563eb），compareSeries 对比虚线（#dc2626）。
// 鼠标悬浮：垂直辅助线 + 主/对比线高亮点 + 右上角 tooltip（时间 + 当前 + 对比 + 差值）
function TrendChartSVG({
  metric,
  series,
  compareSeries,
  height = 240,
}: {
  metric: ChartMetric
  series: TrendPoint[]
  compareSeries?: TrendPoint[]
  height?: number
}) {
  const { t } = useTranslation()
  const svgRef = useRef<SVGSVGElement>(null)
  const [hover, setHover] = useState<{ idx: number; svgX: number } | null>(null)

  const w = 720
  const h = height
  const padLeft = 40
  const padRight = 12
  const padBottom = 28
  const padTop = 10
  const yBaseline = h - padBottom
  const innerW = w - padLeft - padRight
  const innerH = h - padTop - padBottom

  // 防御：从 TrendPoint 取 metric 字段并强制数值化，非有限值（undefined/null/NaN/Infinity）一律返回 0。
  // 这一层兜底覆盖：后端返回字段缺失、JSON 解析得到 null、metric 名拼错等所有"取数路径"。
  const getMetricNum = (p: TrendPoint | undefined | null): number => {
    if (!p) return 0
    const v = (p as Record<string, unknown>)[metric]
    const n = typeof v === 'number' ? v : Number(v)
    return Number.isFinite(n) ? n : 0
  }

  // 主线和对比线共享 Y 轴最大值
  const valuesMain = series.map(getMetricNum)
  const valuesCmp = compareSeries?.map(getMetricNum) ?? []
  const max = Math.max(...valuesMain, ...valuesCmp, 1)

  const formatHM = (ts: number) => {
    const d = new Date(ts * 1000)
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }
  const formatFull = (ts: number) => {
    const d = new Date(ts * 1000)
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  }
  const formatY = (v: number) => {
    if (!Number.isFinite(v)) return '0'
    if (metric === 'avg_duration_ms') {
      if (v >= 1000) return (v / 1000).toFixed(1) + 's'
      return Math.round(v) + 'ms'
    }
    if (v >= 1000) return (v / 1000).toFixed(1) + 'k'
    return String(Math.round(v))
  }
  const formatVal = (v: number) => {
    if (!Number.isFinite(v)) return '—'
    return metric === 'avg_duration_ms' ? fmtMs(v) : fmtNum(v)
  }
  const yTicks = [0, max / 2, max]

  const pointsLen = Math.max(series.length, compareSeries?.length ?? 0)
  const xStep = pointsLen > 1 ? innerW / (pointsLen - 1) : 0
  const yOf = (v: number) => padTop + innerH - (v / max) * innerH
  const buildPath = (pts: TrendPoint[]) =>
    pts.length >= 2
      ? pts.map((p, i) => `${i === 0 ? 'M' : 'L'} ${padLeft + i * xStep} ${yOf(getMetricNum(p))}`).join(' ')
      : ''

  const mainPath = buildPath(series)
  const cmpPath = compareSeries ? buildPath(compareSeries) : ''

  // X 轴标签（取主序列的首/中/末）
  let labels: Array<{ ts: number; x: number }> = []
  if (series.length >= 2) {
    labels = [
      { ts: series[0].ts, x: padLeft },
      { ts: series[Math.floor(series.length / 2)].ts, x: padLeft + innerW / 2 },
      { ts: series[series.length - 1].ts, x: padLeft + innerW },
    ]
  }

  // 鼠标移动 → 把浏览器像素坐标换算到 viewBox 坐标 → 找到最近的 bucket index。
  // 所有除零 / NaN / 越界 / SVG 尚未挂载等异常路径都引向 setHover(null)，确保不会渲染坏掉的 hover。
  const onMouseMove = (e: React.MouseEvent<SVGSVGElement>) => {
    try {
      if (!svgRef.current || pointsLen < 2 || xStep <= 0) {
        if (hover) setHover(null)
        return
      }
      const rect = svgRef.current.getBoundingClientRect()
      if (!(rect.width > 0)) {
        if (hover) setHover(null)
        return
      }
      const svgX = ((e.clientX - rect.left) / rect.width) * w
      if (!Number.isFinite(svgX)) {
        if (hover) setHover(null)
        return
      }
      const relX = svgX - padLeft
      if (relX < 0 || relX > innerW) {
        if (hover) setHover(null)
        return
      }
      const raw = Math.round(relX / xStep)
      if (!Number.isFinite(raw)) {
        if (hover) setHover(null)
        return
      }
      const idx = Math.max(0, Math.min(pointsLen - 1, raw))
      const nextX = padLeft + idx * xStep
      // 只在 idx 变化时 setState，避免每像素都触发 re-render
      if (!hover || hover.idx !== idx) {
        setHover({ idx, svgX: nextX })
      }
    } catch {
      // 任意异常（事件对象异常 / 浏览器 quirk）都直接清空 hover，绝不让图表卡死
      if (hover) setHover(null)
    }
  }
  const onMouseLeave = () => {
    if (hover) setHover(null)
  }

  // Tooltip 内容
  // 数组下标兜底：series[idx] 在 idx 越界时是 undefined，所有下游用 ?? null 处理
  const hoverPoint = hover && hover.idx >= 0 && hover.idx < series.length ? series[hover.idx] : null
  const hoverCmpPoint =
    hover && compareSeries && hover.idx >= 0 && hover.idx < compareSeries.length
      ? compareSeries[hover.idx]
      : null
  // 只要主线或对比线任一在该 idx 有数据，就显示 tooltip（避免主时段无数据时挡住对比线提示）
  const hasAny = !!(hoverPoint || hoverCmpPoint)
  const curVal = getMetricNum(hoverPoint)
  const cmpVal = getMetricNum(hoverCmpPoint)
  // 时间戳取主线优先，否则用对比线；都没有时为 0（不会显示 tooltip 因为 hasAny=false）
  const tipTs = (hoverPoint ?? hoverCmpPoint)?.ts ?? 0
  // tooltip 水平位置：靠左半图 → 显示右侧；靠右半图 → 显示左侧
  const tooltipPct = hover ? (hover.svgX / w) * 100 : 0
  const tooltipOnLeft = tooltipPct > 60

  return (
    <div className='relative'>
      <svg
        ref={svgRef}
        viewBox={`0 0 ${w} ${h}`}
        className='w-full'
        onMouseMove={onMouseMove}
        onMouseLeave={onMouseLeave}
      >
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
                fontSize='10'
                textAnchor='end'
                fill='currentColor'
                opacity='0.55'
              >
                {formatY(v)}
              </text>
            </g>
          )
        })}
        {cmpPath && (
          <path d={cmpPath} fill='none' stroke='#dc2626' strokeWidth='1.5' strokeDasharray='4 3' />
        )}
        {mainPath ? (
          <path d={mainPath} fill='none' stroke='#2563eb' strokeWidth='1.5' />
        ) : (
          <line x1={padLeft} y1={yBaseline} x2={padLeft + innerW} y2={yBaseline} stroke='#2563eb' strokeWidth='1' strokeOpacity='0.4' />
        )}
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
        {/* 悬浮辅助线 + 高亮点（只要任一线在该 idx 有数据就显示） */}
        {hover && hasAny && (
          <>
            <line
              x1={hover.svgX}
              y1={padTop}
              x2={hover.svgX}
              y2={yBaseline}
              stroke='currentColor'
              strokeOpacity={0.4}
              strokeWidth='1'
            />
            {hoverPoint && Number.isFinite(yOf(curVal)) && (
              <circle
                cx={hover.svgX}
                cy={yOf(curVal)}
                r={4}
                fill='#2563eb'
                stroke='white'
                strokeWidth='2'
              />
            )}
            {hoverCmpPoint && Number.isFinite(yOf(cmpVal)) && (
              <circle
                cx={hover.svgX}
                cy={yOf(cmpVal)}
                r={4}
                fill='#dc2626'
                stroke='white'
                strokeWidth='2'
              />
            )}
          </>
        )}
      </svg>
      {/* Tooltip：HTML 层叠在 SVG 之上，位置按 viewBox 百分比换算 */}
      {hover && hasAny && (
        <div
          className='pointer-events-none absolute z-10 min-w-[160px] rounded border bg-popover/95 px-2 py-1.5 text-xs shadow-md backdrop-blur'
          style={{
            top: 8,
            left: tooltipOnLeft ? undefined : `${tooltipPct}%`,
            right: tooltipOnLeft ? `${100 - tooltipPct}%` : undefined,
            transform: tooltipOnLeft ? 'translateX(-8px)' : 'translateX(8px)',
          }}
        >
          <div className='mb-1 text-[11px] text-muted-foreground'>{formatFull(tipTs)}</div>
          {hoverPoint && (
            <div className='flex items-center justify-between gap-3'>
              <span className='flex items-center gap-1'>
                <span className='inline-block h-[2px] w-3 bg-[#2563eb]' />
                {t('Current')}
              </span>
              <span className='font-medium'>{formatVal(curVal)}</span>
            </div>
          )}
          {hoverCmpPoint && (
            <div className={cn('flex items-center justify-between gap-3', hoverPoint && 'mt-1')}>
              <span className='flex items-center gap-1'>
                <span className='inline-block h-[2px] w-3 border-t border-dashed border-[#dc2626]' />
                {t('Compare')}
              </span>
              <span className='font-medium'>{formatVal(cmpVal)}</span>
            </div>
          )}
          {hoverPoint && hoverCmpPoint && (() => {
            const pct = pctDelta(curVal, cmpVal)
            const diff = curVal - cmpVal
            return (
              <div className='mt-1 flex items-center justify-between gap-3 border-t pt-1'>
                <span className='text-muted-foreground'>{t('Difference')}</span>
                <span className={cn('font-medium', deltaColorClass(pct))}>
                  {diff >= 0 ? '+' : ''}
                  {formatVal(diff)} ({fmtDelta(pct)})
                </span>
              </div>
            )
          })()}
        </div>
      )}
      {/* 图例 */}
      <div className='mt-1 flex items-center justify-center gap-4 text-xs text-muted-foreground'>
        <span className='flex items-center gap-1'>
          <span className='inline-block h-[2px] w-4 bg-[#2563eb]' />
          {t('Current')}
        </span>
        {compareSeries && (
          <span className='flex items-center gap-1'>
            <span className='inline-block h-[2px] w-4 border-t border-dashed border-[#dc2626]' />
            {t('Compare')}
          </span>
        )}
      </div>
    </div>
  )
}

// 折线图下方 4 块汇总：本周期 / 对比周期 / 差值 / 环比变化率
function TrendSummaryRow({
  metric,
  series,
  compareSeries,
  compareEnabled,
}: {
  metric: ChartMetric
  series: TrendPoint[]
  compareSeries?: TrendPoint[]
  compareEnabled: boolean
}) {
  const { t } = useTranslation()
  const reduce = (pts: TrendPoint[]) => {
    if (metric === 'avg_duration_ms') {
      // 加权平均：sum(avg*req_total) / sum(req_total)
      let totalReq = 0
      let weighted = 0
      for (const p of pts) {
        totalReq += p.req_total
        weighted += p.avg_duration_ms * p.req_total
      }
      return totalReq > 0 ? Math.round(weighted / totalReq) : 0
    }
    return pts.reduce((acc, p) => acc + ((p[metric] as number) ?? 0), 0)
  }
  const cur = reduce(series)
  const cmp = compareSeries ? reduce(compareSeries) : 0
  const diff = cur - cmp
  const delta = pctDelta(cur, cmp)
  const fmt = (v: number) => (metric === 'avg_duration_ms' ? fmtMs(v) : fmtNum(v))

  return (
    <div className='mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4'>
      <div className='rounded border bg-muted/30 px-3 py-2'>
        <div className='text-xs text-muted-foreground'>{t('Current Period')}</div>
        <div className='mt-0.5 text-base font-semibold'>{fmt(cur)}</div>
      </div>
      <div className='rounded border bg-muted/30 px-3 py-2'>
        <div className='text-xs text-muted-foreground'>{t('Compare Period')}</div>
        <div className='mt-0.5 text-base font-semibold'>
          {compareEnabled ? fmt(cmp) : '—'}
        </div>
      </div>
      <div className='rounded border bg-muted/30 px-3 py-2'>
        <div className='text-xs text-muted-foreground'>{t('Difference')}</div>
        <div className='mt-0.5 text-base font-semibold'>
          {compareEnabled ? `${diff >= 0 ? '+' : ''}${fmt(diff)}` : '—'}
        </div>
      </div>
      <div className='rounded border bg-muted/30 px-3 py-2'>
        <div className='text-xs text-muted-foreground'>{t('Change Rate')}</div>
        <div className={cn('mt-0.5 text-base font-semibold', compareEnabled ? deltaColorClass(delta) : '')}>
          {compareEnabled ? fmtDelta(delta) : '—'}
        </div>
      </div>
    </div>
  )
}

// ============================
// 失败原因 Top10 + 行点击联动右侧折线（左右 2:3 布局）
// ============================

function ErrorTopPanel({
  timeRange,
  channelType,
  userId,
}: {
  timeRange: TimeRange
  channelType: number
  userId?: number
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<ErrorTopRow[]>([])
  const [selectedCode, setSelectedCode] = useState<string | null>(null)
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])

  useEffect(() => {
    if (validateTimeRange(timeRange) != null) return
    const params: Record<string, string | number> = { ...winParams, limit: 10 }
    if (channelType > 0) params.channel_type = channelType
    if (userId && userId > 0) params.user_id = userId
    api
      .get('/api/metrics/errors/top', { params })
      .then((res) => {
        const data: ErrorTopRow[] = res.data?.data ?? []
        setRows(data)
        // 保留已选中态（若仍在 top10 内）；否则置空 → 右侧默认展示"全部失败汇总趋势"。
        setSelectedCode((prev) => (prev && data.some((r) => r.error_code === prev) ? prev : null))
      })
      .catch(() => {})
  }, [winParams, timeRange, channelType, userId])

  const safeRows = rows ?? []

  return (
    <div className='grid grid-cols-1 gap-3 lg:grid-cols-5'>
      {/* 左 Top10（占 2 列） */}
      <Card className='lg:col-span-2'>
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
            <div className='space-y-1'>
              {safeRows.map((r) => {
                const delta = timeRange.compareEnabled ? pctDelta(r.err_count, r.compare_count ?? 0) : null
                const selected = selectedCode === r.error_code
                return (
                  <button
                    key={r.error_code}
                    type='button'
                    // 再点已选中行 = 取消选中，回到汇总趋势
                    onClick={() => setSelectedCode(selected ? null : r.error_code)}
                    className={cn(
                      'flex w-full items-center justify-between gap-2 rounded px-2 py-1 text-left text-sm transition-colors hover:bg-muted/40',
                      selected && 'bg-muted',
                    )}
                  >
                    <span className='truncate' title={r.sample_message}>
                      {r.error_code}
                    </span>
                    <span className='flex shrink-0 items-center gap-2 whitespace-nowrap'>
                      <span className='font-medium'>{fmtNum(r.err_count)} {t('times')}</span>
                      {timeRange.compareEnabled && (
                        <>
                          <span className={cn('text-[11px]', deltaColorClass(delta))}>{fmtDelta(delta)}</span>
                          <span className='text-[11px] text-muted-foreground'>
                            {t('Compare')}: {fmtNum(r.compare_count ?? 0)}
                          </span>
                        </>
                      )}
                    </span>
                  </button>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* 右联动折线（占 3 列）— 未选中时汇总 top10 这些 error_code 的趋势，选中后只展示该项 */}
      <Card className='lg:col-span-3'>
        <CardHeader className='pb-1'>
          <CardTitle className='text-sm font-medium text-muted-foreground'>
            {selectedCode ? `${t('Failure Trend')}: ${selectedCode}` : t('Failure Trend Summary')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ErrorTrendChart
            timeRange={timeRange}
            channelType={channelType}
            userId={userId}
            // selectedCode 非空 → 单错误码；为空 → 汇总 top10 列表里所有 error_code
            codes={selectedCode ? [selectedCode] : safeRows.map((r) => r.error_code)}
          />
        </CardContent>
      </Card>
    </div>
  )
}

function ErrorTrendChart({
  timeRange,
  channelType,
  userId,
  codes,
}: {
  timeRange: TimeRange
  channelType: number
  userId?: number
  codes: string[]
}) {
  const [data, setData] = useState<TrendResult | null>(null)
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])
  // 用 JSON 串作为 effect 依赖键，避免数组引用变化引发的重复请求
  const codesKey = useMemo(() => codes.join(','), [codes])
  useEffect(() => {
    if (validateTimeRange(timeRange) != null) return
    if (codes.length === 0) {
      setData(null)
      return
    }
    const params: Record<string, string | number> = { ...winParams, error_codes: codesKey }
    if (channelType > 0) params.channel_type = channelType
    if (userId && userId > 0) params.user_id = userId
    api
      .get('/api/metrics/errors/trend', { params })
      .then((res) => setData(res.data?.data ?? null))
      .catch(() => {})
  }, [winParams, timeRange, channelType, userId, codes.length, codesKey])
  return (
    <div>
      <TrendChartSVG metric='err_count' series={data?.series ?? []} compareSeries={data?.compare_series} height={200} />
      <TrendSummaryRow
        metric='err_count'
        series={data?.series ?? []}
        compareSeries={data?.compare_series}
        compareEnabled={timeRange.compareEnabled}
      />
    </div>
  )
}

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
  timeRange,
  onDrill,
}: {
  timeRange: TimeRange
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
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])

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

  // 切换 timeRange/筛选时回到第一页
  useEffect(() => {
    setPage(1)
  }, [winParams, appliedUsername, channelTypeFilter])

  useEffect(() => {
    if (validateTimeRange(timeRange) != null) return
    setLoading(true)
    const params: Record<string, string | number> = { ...winParams, page, size: pageSize }
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
  }, [winParams, timeRange, appliedUsername, channelTypeFilter, page, t])

  // 平台下拉:走后端 /api/metrics/platforms 接口
  useEffect(() => {
    if (validateTimeRange(timeRange) != null) return
    api
      .get('/api/metrics/platforms', { params: { from: winParams.from, to: winParams.to } })
      .then((res) => setPlatformsFromApi(res.data?.data ?? []))
      .catch(() => {})
  }, [winParams, timeRange])

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

  const compareEnabled = timeRange.compareEnabled

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
                  <CompareCell value={r.req_total} compare={r.compare?.req_total} compareEnabled={compareEnabled} formatter={fmtNum} bold />
                  <CompareCell value={r.avg_duration_ms} compare={r.compare?.avg_duration_ms} compareEnabled={compareEnabled} formatter={fmtMs} />
                  <CompareCell value={r.p50_ms} compare={r.compare?.p50_ms} compareEnabled={compareEnabled} formatter={fmtMs} />
                  <CompareCell value={r.p95_ms} compare={r.compare?.p95_ms} compareEnabled={compareEnabled} formatter={fmtMs} />
                  <CompareCell value={r.error_rate} compare={r.compare?.error_rate} compareEnabled={compareEnabled} formatter={fmtPct} />
                  <CompareCell value={r.slow_resp_rate} compare={r.compare?.slow_resp_rate} compareEnabled={compareEnabled} formatter={fmtPct} />
                  <CompareCell value={r.slow_ttft_rate} compare={r.compare?.slow_ttft_rate} compareEnabled={compareEnabled} formatter={fmtPct} />
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

// 数值单元格 + 对比同期；compareEnabled 时显示"较同期 ↑/↓N.NN%"。
function CompareCell({
  value,
  compare,
  compareEnabled,
  formatter,
  bold,
}: {
  value: number
  compare?: number
  compareEnabled: boolean
  formatter: (v: number) => string
  bold?: boolean
}) {
  const delta = compareEnabled ? pctDelta(value, compare ?? 0) : null
  return (
    <TableCell className='text-right'>
      <div className='flex items-center justify-end gap-1 whitespace-nowrap'>
        <span className={cn(bold && 'font-medium')}>{formatter(value)}</span>
        {compareEnabled && (
          <span className={cn('text-[11px]', deltaColorClass(delta))}>({fmtDelta(delta)})</span>
        )}
      </div>
    </TableCell>
  )
}

// ============================
// 渠道-模型下钻 Dialog
// ============================

// 把 "YYYY-MM-DD" 转为 "YYYY-M-D"（月份/日不补 0），用于时段小字。
function trimDateZeros(ymd: string): string {
  const [y, m, d] = ymd.split('-')
  return `${y}-${Number(m)}-${Number(d)}`
}

// "2026-6-16 00:00-12:00 对比 2026-6-15 同时段"
function formatTimeRangeText(r: TimeRange, t: (k: string) => string): string {
  const main = `${trimDateZeros(r.date)} ${r.startHM}-${r.endHM}`
  if (!r.compareEnabled) return main
  return `${main} ${t('compared with')} ${trimDateZeros(r.compareDate)} ${t('same time period')}`
}

// 平均时长差值（ms），单位明确为绝对差，开启对比时非空。
function diffMs(cur: number, base: number | undefined | null): number | null {
  if (base == null) return null
  return cur - base
}

function fmtDeltaMs(d: number | null): string {
  if (d == null) return '—'
  if (d === 0) return '0ms'
  return `${d > 0 ? '+' : ''}${d}ms`
}

// ============================
// 用户下钻弹窗 — 平台 4 卡片（对齐原型）
//   1. 总请求：环比百分比 + "对比请求"
//   2. 平均响应时长：差值 ms + P50/P95 + "X% 慢"小徽标(slow_resp_rate)
//   3. 平均 TTFT 时长：差值 ms + TTFT-P50/P95 + "X% 慢"小徽标(slow_ttft_rate)
//   4. 请求错误率：环比百分比 + "错误次数 / 对比错误次数"
// ============================
function PlatformMetricCards({
  cur,
  cmp,
  compareEnabled,
}: {
  cur: OverviewPlatform
  cmp: OverviewPlatform | undefined
  compareEnabled: boolean
}) {
  const { t } = useTranslation()
  const dReq = compareEnabled ? pctDelta(cur.req_total, cmp?.req_total ?? 0) : null
  const dErr = compareEnabled ? pctDelta(cur.error_rate, cmp?.error_rate ?? 0) : null
  const dAvg = compareEnabled ? diffMs(cur.avg_duration_ms, cmp?.avg_duration_ms ?? 0) : null
  const dAvgTtft = compareEnabled ? diffMs(cur.avg_ttft_ms, cmp?.avg_ttft_ms ?? 0) : null

  return (
    <div className='grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4'>
      {/* 1. 总请求 */}
      <Card>
        <CardHeader className='pb-1'>
          <CardTitle className='text-sm font-medium text-muted-foreground'>
            {t('Total Requests')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className='flex items-baseline gap-2'>
            <span className='text-3xl font-semibold'>{fmtNum(cur.req_total)}</span>
            {compareEnabled && (
              <span className={cn('text-xs font-medium', deltaColorClass(dReq))}>
                {fmtDelta(dReq)}
              </span>
            )}
          </div>
          <div className='mt-2 text-xs text-muted-foreground'>
            {t('Compare Requests')}: {fmtNum(cmp?.req_total ?? 0)}
          </div>
        </CardContent>
      </Card>

      {/* 2. 平均响应时长 */}
      <Card>
        <CardHeader className='pb-1'>
          <CardTitle className='flex items-center justify-between gap-1 text-sm font-medium text-muted-foreground'>
            <span>{t('Average Response Time')}</span>
            <span className='rounded-full bg-orange-50 px-1.5 py-0.5 text-[10px] font-normal text-orange-600'>
              {fmtPct(cur.slow_resp_rate)} {t('slow')}
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className='flex items-baseline gap-1'>
            <span className='text-3xl font-semibold'>{fmtNum(cur.avg_duration_ms)}</span>
            <span className='text-xs text-muted-foreground'>ms</span>
            {compareEnabled && (
              <span className={cn('ms-1 text-xs font-medium', diffColorClass(dAvg))}>
                {fmtDeltaMs(dAvg)}
              </span>
            )}
          </div>
          <div className='mt-2 text-xs text-muted-foreground'>
            P50: {fmtNum(cur.p50_ms)}ms&nbsp;&nbsp;P95: {fmtNum(cur.p95_ms)}ms
          </div>
        </CardContent>
      </Card>

      {/* 3. 平均 TTFT 时长 */}
      <Card>
        <CardHeader className='pb-1'>
          <CardTitle className='flex items-center justify-between gap-1 text-sm font-medium text-muted-foreground'>
            <span>{t('Average TTFT (Stats)')}</span>
            <span className='rounded-full bg-blue-50 px-1.5 py-0.5 text-[10px] font-normal text-blue-600'>
              {fmtPct(cur.slow_ttft_rate ?? 0)} {t('slow')}
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className='flex items-baseline gap-1'>
            <span className='text-3xl font-semibold'>{fmtNum(cur.avg_ttft_ms)}</span>
            <span className='text-xs text-muted-foreground'>ms</span>
            {compareEnabled && (
              <span className={cn('ms-1 text-xs font-medium', diffColorClass(dAvgTtft))}>
                {fmtDeltaMs(dAvgTtft)}
              </span>
            )}
          </div>
          <div className='mt-2 text-xs text-muted-foreground'>
            P50: {fmtNum(cur.ttft_p50_ms)}ms&nbsp;&nbsp;P95: {fmtNum(cur.ttft_p95_ms)}ms
          </div>
        </CardContent>
      </Card>

      {/* 4. 请求错误率 */}
      <Card>
        <CardHeader className='pb-1'>
          <CardTitle className='text-sm font-medium text-muted-foreground'>
            {t('Error Rate')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className='flex items-baseline gap-2'>
            <span className='text-3xl font-semibold'>{fmtPct(cur.error_rate)}</span>
            {compareEnabled && (
              <span className={cn('text-xs font-medium', deltaColorClass(dErr))}>
                {fmtDelta(dErr)}
              </span>
            )}
          </div>
          <div className='mt-2 text-xs text-muted-foreground'>
            {t('Error Count')}: {fmtNum(cur.err_count)}
            {compareEnabled && (
              <>
                &nbsp;|&nbsp;{t('Compare Errors')}: {fmtNum(cmp?.err_count ?? 0)}
              </>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

// 差值用染色：和 pctDelta 一样按"上涨红 / 下降绿"
function diffColorClass(d: number | null): string {
  if (d == null || d === 0) return 'text-muted-foreground'
  return d > 0 ? 'text-red-600' : 'text-green-600'
}

function DrilldownDialog({
  userId,
  username,
  timeRange,
  onClose,
}: {
  userId: number | null
  username: string
  timeRange: TimeRange
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [platforms, setPlatforms] = useState<OverviewPlatform[]>([])
  const [comparePlatforms, setComparePlatforms] = useState<OverviewPlatform[]>([])
  const [overviewTotal, setOverviewTotal] = useState<OverviewTotal | null>(null)
  const [allChannels, setAllChannels] = useState<Array<ChannelRow & { channel_type: number }>>([])
  const [expandedChannel, setExpandedChannel] = useState<number | null>(null)
  // 选中的 channel_type:0 表示全部(不过滤),其它为某个平台
  const [selectedChannelType, setSelectedChannelType] = useState<number>(0)
  // 弹窗内部完整传 compare_*，所有数据展示同比与主页面保持一致
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])

  const open = userId != null
  const compareEnabled = timeRange.compareEnabled

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

  // 1. 用户级别总览 + 平台子卡（含对比）
  useEffect(() => {
    if (!open || userId == null) return
    api
      .get('/api/metrics/overview', { params: { ...winParams, user_id: userId } })
      .then((res) => {
        setPlatforms(res.data?.data?.platforms ?? [])
        setComparePlatforms(res.data?.data?.compare_platforms ?? [])
        setOverviewTotal(res.data?.data?.total ?? null)
      })
      .catch(() => {})
  }, [open, userId, winParams])

  // 2. 用户所有渠道(从每个 platform 拉 channels 然后合并；后端已支持 compare_*，每行带 compare 字段)
  useEffect(() => {
    if (!open || userId == null || platforms.length === 0) {
      setAllChannels([])
      return
    }
    Promise.all(
      platforms.map((p) =>
        api
          .get(`/api/metrics/platform/${p.channel_type}/channels`, {
            params: { ...winParams, user_id: userId },
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
  }, [open, userId, winParams, platforms])

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

  // 对比平台映射：channel_type → OverviewPlatform，用于卡片同比计算
  const comparePlatformMap = useMemo(() => {
    return new Map<number, OverviewPlatform>(
      (comparePlatforms ?? []).map((p) => [p.channel_type, p])
    )
  }, [comparePlatforms])

  // 平台 Tab 默认选中第一个；切换用户时也复位到第一个
  useEffect(() => {
    if (platforms.length > 0 && (selectedChannelType <= 0 || !platforms.some((p) => p.channel_type === selectedChannelType))) {
      setSelectedChannelType(platforms[0].channel_type)
    }
  }, [platforms, selectedChannelType])

  // 当前选中平台的主/对比数据
  const currentPlatform = useMemo(
    () => platforms.find((p) => p.channel_type === selectedChannelType),
    [platforms, selectedChannelType],
  )
  const currentCompare = currentPlatform ? comparePlatformMap.get(currentPlatform.channel_type) : undefined

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
          {/* 时段小字：2026-6-16 00:00-12:00 对比 2026-6-15 同时段 */}
          <div className='text-xs text-muted-foreground'>{formatTimeRangeText(timeRange, t)}</div>
        </DialogHeader>
        <div className='flex-1 space-y-4 overflow-y-auto overflow-x-hidden pe-1'>
          {/* 平台 Tab — 切换后所有数据按选中平台过滤 */}
          {platforms.length > 0 && (
            <div className='flex flex-wrap items-center gap-2 border-b'>
              {platforms.map((p) => {
                const active = selectedChannelType === p.channel_type
                return (
                  <button
                    key={p.channel_type}
                    type='button'
                    onClick={() => setSelectedChannelType(p.channel_type)}
                    className={cn(
                      'border-b-2 px-3 py-2 text-sm transition-colors',
                      active
                        ? 'border-blue-600 font-semibold text-blue-600'
                        : 'border-transparent text-muted-foreground hover:text-foreground',
                    )}
                  >
                    {p.platform_name}
                  </button>
                )
              })}
            </div>
          )}

          {/* 4 个卡片：总请求 / 平均响应时长 / 平均 TTFT 时长 / 请求错误率 */}
          {currentPlatform && (
            <PlatformMetricCards
              cur={currentPlatform}
              cmp={currentCompare}
              compareEnabled={compareEnabled}
            />
          )}
          {platforms.length === 0 && overviewTotal && (
            <div className='text-sm text-muted-foreground'>{t('No Data (Stats)')}</div>
          )}

          {/* 折线图区：5 Tab 切换；前 4 个 metric 用普通折线，err_count 切换为 Top10+联动折线 */}
          <UserTrendSection
            timeRange={timeRange}
            userId={userId ?? 0}
            channelType={selectedChannelType}
          />

          {/* 渠道耗时明细 — 单行 | 分隔的字段名严格对齐原型，加同比小字 */}
          <div>
            <div className='mb-2 text-sm font-semibold'>{t('Channel Latency Details')}</div>
            {filteredChannels.length === 0 ? (
              <div className='rounded border bg-muted/30 px-3 py-2 text-xs text-muted-foreground'>
                {t('No Data (Stats)')}
              </div>
            ) : (
              <div className='space-y-2'>
                {filteredChannels.map((r) => {
                  const c = r.compare
                  const dAvg = compareEnabled ? pctDelta(r.avg_duration_ms, c?.avg_duration_ms ?? 0) : null
                  const dP95 = compareEnabled ? pctDelta(r.p95_ms, c?.p95_ms ?? 0) : null
                  const dReq = compareEnabled ? pctDelta(r.req_total, c?.req_total ?? 0) : null
                  const dErr = compareEnabled ? pctDelta(r.error_rate, c?.error_rate ?? 0) : null
                  const dST = compareEnabled ? pctDelta(r.slow_ttft_rate, c?.slow_ttft_rate ?? 0) : null
                  const dSR = compareEnabled ? pctDelta(r.slow_resp_rate, c?.slow_resp_rate ?? 0) : null
                  return (
                    <div key={r.channel_id} className='rounded border'>
                      <div className='flex items-center justify-between gap-3 px-3 py-2'>
                        <div className='min-w-0 flex-1'>
                          <div className='font-medium'>{r.channel_name || `#${r.channel_id}`}</div>
                          <div className='mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground'>
                            <span>
                              {t('Average Duration')}: {fmtMs(r.avg_duration_ms)}
                              {compareEnabled && <span className={cn('ms-1', deltaColorClass(dAvg))}>({fmtDelta(dAvg)})</span>}
                            </span>
                            <span>
                              | {t('Request Duration P95')}: {fmtMs(r.p95_ms)}
                              {compareEnabled && <span className={cn('ms-1', deltaColorClass(dP95))}>({fmtDelta(dP95)})</span>}
                            </span>
                            <span>
                              | {t('Request Times')}: {fmtNum(r.req_total)}
                              {compareEnabled && <span className={cn('ms-1', deltaColorClass(dReq))}>({fmtDelta(dReq)})</span>}
                            </span>
                            <span>
                              | {t('Error (Stats)')}: {fmtPct(r.error_rate)}
                              {compareEnabled && <span className={cn('ms-1', deltaColorClass(dErr))}>({fmtDelta(dErr)})</span>}
                            </span>
                            <span>
                              | {t('Slow TTFT')}: {fmtPct(r.slow_ttft_rate)}
                              {compareEnabled && <span className={cn('ms-1', deltaColorClass(dST))}>({fmtDelta(dST)})</span>}
                            </span>
                            <span>
                              | {t('Slow Response')}: {fmtPct(r.slow_resp_rate)}
                              {compareEnabled && <span className={cn('ms-1', deltaColorClass(dSR))}>({fmtDelta(dSR)})</span>}
                            </span>
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
                            timeRange={timeRange}
                          />
                        </div>
                      )}
                    </div>
                  )
                })}
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

// ============================
// 弹窗专用折线图区：5 个 metric Tab
//   前 4 个：普通折线 + 4 块汇总（请求量 / 响应慢 / 首字慢 / 平均响应时长）
//   第 5 个：err_count → 左 Top10 + 右联动折线（复用 ErrorTopPanel）
// ============================

type UserTrendMetric = 'req_total' | 'slow_resp_count' | 'slow_ttft_count' | 'avg_duration_ms' | 'err_count'

const USER_METRIC_LABEL: Record<UserTrendMetric, string> = {
  req_total: 'Request Count (Stats)',
  slow_resp_count: 'Slow Response Requests',
  slow_ttft_count: 'Slow TTFT Requests',
  avg_duration_ms: 'Average Response Time',
  err_count: 'Request Errors (count)',
}

function UserTrendSection({
  timeRange,
  userId,
  channelType,
}: {
  timeRange: TimeRange
  userId: number
  channelType: number
}) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<UserTrendMetric>('req_total')
  const [data, setData] = useState<TrendResult | null>(null)
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])

  // 仅前 4 个 metric 需要拉 /trend；err_count 切换到 ErrorTopPanel 自己拉。
  useEffect(() => {
    if (metric === 'err_count') return
    if (validateTimeRange(timeRange) != null) return
    const params: Record<string, string | number> = { ...winParams }
    if (channelType > 0) params.channel_type = channelType
    if (userId > 0) params.user_id = userId
    api
      .get('/api/metrics/trend', { params })
      .then((res) => setData(res.data?.data ?? null))
      .catch(() => {})
  }, [metric, winParams, timeRange, channelType, userId])

  return (
    <Card>
      <CardHeader className='flex flex-row items-center justify-between gap-2 pb-1'>
        <CardTitle className='text-sm font-medium text-muted-foreground'>{t('Trend')}</CardTitle>
        <div className='flex items-center gap-1 rounded bg-muted p-0.5'>
          {(Object.keys(USER_METRIC_LABEL) as UserTrendMetric[]).map((m) => (
            <button
              key={m}
              type='button'
              className={cn(
                'rounded px-2.5 py-0.5 text-xs transition-colors',
                metric === m ? 'bg-background shadow-sm' : 'text-muted-foreground hover:text-foreground',
              )}
              onClick={() => setMetric(m)}
            >
              {t(USER_METRIC_LABEL[m])}
            </button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        {metric === 'err_count' ? (
          // 复用主页面 ErrorTopPanel —— 左 Top10 + 右联动折线；带 userId/channelType 过滤
          <ErrorTopPanel timeRange={timeRange} channelType={channelType} userId={userId} />
        ) : (
          <>
            <TrendChartSVG metric={metric} series={data?.series ?? []} compareSeries={data?.compare_series} />
            <TrendSummaryRow
              metric={metric}
              series={data?.series ?? []}
              compareSeries={data?.compare_series}
              compareEnabled={timeRange.compareEnabled}
            />
          </>
        )}
      </CardContent>
    </Card>
  )
}

// 渠道展开的模型表格(带搜索 + 内存分页)
function UserChannelModels({
  channelId,
  userId,
  timeRange,
}: {
  channelId: number
  userId: number
  timeRange: TimeRange
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<ModelRow[]>([])
  const [search, setSearch] = useState<string>('')
  const [page, setPage] = useState<number>(1)
  const pageSize = 10
  // 模型表单元格透传 compare_*，用 CompareCell 渲染同比
  const winParams = useMemo(() => toWindowParams(timeRange), [timeRange])
  const compareEnabled = timeRange.compareEnabled

  useEffect(() => {
    api
      .get(`/api/metrics/channel/${channelId}/models`, {
        params: { ...winParams, user_id: userId || undefined },
      })
      .then((res) => setRows(res.data?.data ?? []))
      .catch(() => {})
    setPage(1)
  }, [channelId, userId, winParams])

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
              <CompareCell value={r.req_total} compare={r.compare?.req_total} compareEnabled={compareEnabled} formatter={fmtNum} bold />
              <CompareCell value={r.avg_duration_ms} compare={r.compare?.avg_duration_ms} compareEnabled={compareEnabled} formatter={fmtMs} />
              <CompareCell value={r.p50_ms} compare={r.compare?.p50_ms} compareEnabled={compareEnabled} formatter={fmtMs} />
              <CompareCell value={r.p95_ms} compare={r.compare?.p95_ms} compareEnabled={compareEnabled} formatter={fmtMs} />
              <CompareCell value={r.error_rate} compare={r.compare?.error_rate} compareEnabled={compareEnabled} formatter={fmtPct} />
              <CompareCell value={r.slow_resp_rate} compare={r.compare?.slow_resp_rate} compareEnabled={compareEnabled} formatter={fmtPct} />
              <CompareCell value={r.slow_ttft_rate} compare={r.compare?.slow_ttft_rate} compareEnabled={compareEnabled} formatter={fmtPct} />
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
    const to = Math.floor(Date.now() / 1000)
    const from = to - 48 * 3600
    api
      .get('/api/metrics/platforms', { params: { from, to } })
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
