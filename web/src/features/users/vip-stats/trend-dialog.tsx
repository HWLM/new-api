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
import { useEffect, useMemo, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Loader2, X } from 'lucide-react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatCurrencyFromUSD } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
import { quotaUnitsToDollars } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { DatePicker } from '@/components/date-picker'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getVipStatsTrend } from '@/features/users/api'
import type {
  TrendGranularity,
  TrendMode,
  VipStatsTrend,
  VipStatsTrendParams,
} from '@/features/users/types'

const quotaToUsd = (q: number) => quotaUnitsToDollars(q)

// daily 模式时间段的视觉默认值
const DEFAULT_TIME_START = '00:00:00'
const DEFAULT_TIME_END = '23:59:59'

interface TrendDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 公开 vip-stats 页面用的密码（admin 复用时传空串即可 —— 改用 fetchTrend 注入） */
  password: string
  /** 重点客户 user_id（点哪一行就传哪个） */
  userId: number | null
  /** 客户名（弹框标题展示） */
  username?: string
  /**
   * 可选：自定义趋势请求函数（admin 复用场景下绕过密码门用）。
   * 不传时走默认 `getVipStatsTrend(password, params)`。
   */
  fetchTrend?: (
    params: VipStatsTrendParams
  ) => Promise<{ success: boolean; message?: string; data?: VipStatsTrend }>
}

/**
 * 把 "HH:MM:SS" 转成 0..23 整数（截断到小时；空串/解析失败 → 默认值）
 */
function parseHour(s: string, fallback: number): number {
  if (!s) return fallback
  const m = s.match(/^(\d{1,2})/)
  if (!m) return fallback
  const h = parseInt(m[1], 10)
  if (Number.isNaN(h) || h < 0 || h > 23) return fallback
  return h
}

/**
 * 根据 mode 把用户输入的「开始日期 / 结束日期」解析成后端要的 start/end 字符串。
 *  - daily:   start = end = startDate
 *  - monthly: start = startDate 月的 1 号, end = startDate 月的最后一天
 *  - custom:  原样 startDate ~ endDate
 */
function resolveDates(
  mode: TrendMode,
  startDate: Date | undefined,
  endDate: Date | undefined
): { start: string; end: string } | null {
  if (!startDate) return null
  const start = dayjs(startDate)
  let endD: dayjs.Dayjs
  if (mode === 'daily') {
    endD = start
  } else if (mode === 'monthly') {
    return {
      start: start.startOf('month').format('YYYY-MM-DD'),
      end: start.endOf('month').format('YYYY-MM-DD'),
    }
  } else {
    if (!endDate) return null
    endD = dayjs(endDate)
    if (endD.isBefore(start, 'day')) return null
  }
  return { start: start.format('YYYY-MM-DD'), end: endD.format('YYYY-MM-DD') }
}

/**
 * 按 mode 计算各字段的默认值（点开弹框时、切换 mode 时调用）。
 *  - daily:   当前=今天, 对比=昨天, 时间=00:00:00 ~ 23:59:59
 *  - monthly: 当前=本月, 对比=上月
 *  - custom:  当前=[today-7, today], 对比=[today-14, today-7]
 */
function computeDefaults(mode: TrendMode) {
  const now = dayjs()
  switch (mode) {
    case 'daily':
      return {
        currentStartDate: now.toDate(),
        currentEndDate: undefined as Date | undefined,
        compareStartDate: now.subtract(1, 'day').toDate(),
        compareEndDate: undefined as Date | undefined,
        timeStart: DEFAULT_TIME_START,
        timeEnd: DEFAULT_TIME_END,
      }
    case 'monthly':
      return {
        currentStartDate: now.startOf('month').toDate(),
        currentEndDate: undefined as Date | undefined,
        compareStartDate: now.subtract(1, 'month').startOf('month').toDate(),
        compareEndDate: undefined as Date | undefined,
        timeStart: '',
        timeEnd: '',
      }
    default:
      // custom：本期 [today-7, today]，对比 [today-15, today-8]（与本期不重叠，向前推 8 天）
      return {
        currentStartDate: now.subtract(7, 'day').toDate(),
        currentEndDate: now.toDate(),
        compareStartDate: now.subtract(15, 'day').toDate(),
        compareEndDate: now.subtract(8, 'day').toDate(),
        timeStart: '',
        timeEnd: '',
      }
  }
}

export function TrendDialog(props: TrendDialogProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<TrendMode>('daily')
  const [granularity, setGranularity] = useState<TrendGranularity>('day')

  const [currentStartDate, setCurrentStartDate] = useState<Date | undefined>()
  const [currentEndDate, setCurrentEndDate] = useState<Date | undefined>()
  const [compareStartDate, setCompareStartDate] = useState<Date | undefined>()
  const [compareEndDate, setCompareEndDate] = useState<Date | undefined>()

  // daily 模式：HH:MM:SS 一对时间段，应用到两个周期
  const [timeStart, setTimeStart] = useState('')
  const [timeEnd, setTimeEnd] = useState('')

  const [chartData, setChartData] = useState<VipStatsTrend | null>(null)

  // mode=daily 时数据 granularity 强制 hour
  const effectiveGranularity: TrendGranularity =
    mode === 'daily' ? 'hour' : granularity

  // 应用某种 mode 的默认值（切 mode 时、打开弹框时）
  const applyDefaults = (m: TrendMode) => {
    const d = computeDefaults(m)
    setCurrentStartDate(d.currentStartDate)
    setCurrentEndDate(d.currentEndDate)
    setCompareStartDate(d.compareStartDate)
    setCompareEndDate(d.compareEndDate)
    setTimeStart(d.timeStart)
    setTimeEnd(d.timeEnd)
  }

  // 弹框打开时按当前 mode 应用默认值；关闭时重置
  useEffect(() => {
    if (props.open) {
      applyDefaults(mode)
      setChartData(null)
    } else {
      setMode('daily')
      setGranularity('day')
      setCurrentStartDate(undefined)
      setCurrentEndDate(undefined)
      setCompareStartDate(undefined)
      setCompareEndDate(undefined)
      setTimeStart('')
      setTimeEnd('')
      setChartData(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.open])

  const changeMode = (m: TrendMode) => {
    setMode(m)
    applyDefaults(m)
    setChartData(null)
  }

  const clearTime = () => {
    setTimeStart(DEFAULT_TIME_START)
    setTimeEnd(DEFAULT_TIME_END)
  }

  const trendMutation = useMutation({
    mutationFn: async () => {
      if (!props.userId) throw new Error('user_id missing')
      const c = resolveDates(mode, currentStartDate, currentEndDate)
      const p = resolveDates(mode, compareStartDate, compareEndDate)
      if (!c || !p) {
        throw new Error(t('Please select both current and compare periods'))
      }
      // daily 模式：空串 → 默认全天 0~23
      const sh = mode === 'daily' ? parseHour(timeStart, 0) : 0
      const eh = mode === 'daily' ? parseHour(timeEnd, 23) : 23
      const trendParams: VipStatsTrendParams = {
        user_id: props.userId,
        granularity: effectiveGranularity,
        current_start: c.start,
        current_end: c.end,
        compare_start: p.start,
        compare_end: p.end,
        ...(effectiveGranularity === 'hour' && {
          current_start_hour: sh,
          current_end_hour: eh,
          compare_start_hour: sh,
          compare_end_hour: eh,
        }),
      }
      const fetcher =
        props.fetchTrend ??
        ((q: VipStatsTrendParams) => getVipStatsTrend(props.password, q))
      const res = await fetcher(trendParams)
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Failed to load trend'))
      }
      return res.data
    },
    onSuccess: (data) => setChartData(data),
    onError: (err: Error) => toast.error(err.message),
  })

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-w-5xl sm:max-w-5xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Trend Change')}
            {props.username ? ` — ${props.username}` : ''}
          </DialogTitle>
        </DialogHeader>

        {/* 顶部控件区：mode + 两段时间段 + （日环比时）时分秒 + 生成对比 */}
        <div className='flex flex-col gap-3 rounded-lg border p-4'>
          <div className='flex items-center gap-2'>
            <span className='text-muted-foreground w-[100px] text-sm'>
              {t('Compare Mode')}：
            </span>
            <Select
              items={[
                { value: 'daily', label: t('Daily Compare') },
                { value: 'monthly', label: t('Monthly Compare') },
                { value: 'custom', label: t('Custom') },
              ]}
              value={mode}
              onValueChange={(v) => {
                if (v) changeMode(v as TrendMode)
              }}
            >
              <SelectTrigger className='w-[140px]'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='daily'>{t('Daily Compare')}</SelectItem>
                <SelectItem value='monthly'>{t('Monthly Compare')}</SelectItem>
                <SelectItem value='custom'>{t('Custom')}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <PeriodRow
            label={t('Current Period') + '：'}
            mode={mode}
            startDate={currentStartDate}
            endDate={currentEndDate}
            onStartChange={setCurrentStartDate}
            onEndChange={setCurrentEndDate}
          />
          <PeriodRow
            label={t('Compare Period') + '：'}
            mode={mode}
            startDate={compareStartDate}
            endDate={compareEndDate}
            onStartChange={setCompareStartDate}
            onEndChange={setCompareEndDate}
          />

          {mode === 'daily' && (
            <div className='flex items-center gap-2'>
              <span className='text-muted-foreground w-[100px] text-sm'>
                {t('Time Range')}：
              </span>
              <Input
                type='time'
                step={1}
                value={timeStart}
                onChange={(e) => setTimeStart(e.target.value)}
                className='w-[140px]'
                placeholder='00:00:00'
              />
              <span className='text-muted-foreground text-sm'>~</span>
              <Input
                type='time'
                step={1}
                value={timeEnd}
                onChange={(e) => setTimeEnd(e.target.value)}
                className='w-[140px]'
                placeholder='23:59:59'
              />
              <Button
                variant='ghost'
                size='icon-sm'
                onClick={clearTime}
                title={t('Reset to full day')}
                aria-label={t('Reset to full day')}
              >
                <X className='h-4 w-4' />
              </Button>
              <span className='text-muted-foreground text-xs'>
                {t('Empty = full day 00:00:00 ~ 23:59:59')}
              </span>
            </div>
          )}

          <div className='flex justify-end'>
            <Button
              onClick={() => trendMutation.mutate()}
              disabled={trendMutation.isPending}
            >
              {trendMutation.isPending && (
                <Loader2 className='h-4 w-4 animate-spin' />
              )}
              {t('Generate Comparison')}
            </Button>
          </div>
        </div>

        {/* 折线图区 */}
        <div className='overflow-hidden rounded-lg border'>
          <div className='flex items-center justify-between border-b px-4 py-2'>
            <div className='text-sm font-medium'>
              {t('User Consumption')}{' '}
              <span className='text-muted-foreground text-xs'>
                {t('(Amount $)')}
              </span>
            </div>
            {mode !== 'daily' && (
              <Select
                items={[
                  { value: 'day', label: t('By Day') },
                  { value: 'hour', label: t('By Hour') },
                ]}
                value={granularity}
                onValueChange={(v) => {
                  if (v) {
                    setGranularity(v as TrendGranularity)
                    setChartData(null)
                  }
                }}
              >
                <SelectTrigger className='w-[90px]'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='day'>{t('By Day')}</SelectItem>
                  <SelectItem value='hour'>{t('By Hour')}</SelectItem>
                </SelectContent>
              </Select>
            )}
          </div>

          <div className='h-[300px] p-3'>
            <TrendChart data={chartData} loading={trendMutation.isPending} t={t} />
          </div>
        </div>

        {/* 4 张汇总卡片 */}
        <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
          <SummaryBox
            label={t('This Period')}
            value={chartData ? formatCurrencyFromUSD(quotaToUsd(chartData.current_total)) : '—'}
          />
          <SummaryBox
            label={t('Compare Period')}
            value={chartData ? formatCurrencyFromUSD(quotaToUsd(chartData.compare_total)) : '—'}
          />
          <SummaryBox
            label={t('Difference')}
            value={
              chartData
                ? `${chartData.diff >= 0 ? '+' : '-'}${formatCurrencyFromUSD(quotaToUsd(Math.abs(chartData.diff)))}`
                : '—'
            }
            valueClass={
              chartData
                ? chartData.diff > 0
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : chartData.diff < 0
                    ? 'text-rose-600 dark:text-rose-400'
                    : ''
                : ''
            }
          />
          <SummaryBox
            label={t('Change Rate')}
            value={
              chartData
                ? `${chartData.change_rate >= 0 ? '+' : ''}${chartData.change_rate.toFixed(1)}%`
                : '—'
            }
            valueClass={
              chartData
                ? chartData.change_rate > 0
                  ? 'text-emerald-600 dark:text-emerald-400'
                  : chartData.change_rate < 0
                    ? 'text-rose-600 dark:text-rose-400'
                    : ''
                : ''
            }
          />
        </div>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// 子组件
// ============================================================================

interface PeriodRowProps {
  label: string
  mode: TrendMode
  startDate: Date | undefined
  endDate: Date | undefined
  onStartChange: (d: Date | undefined) => void
  onEndChange: (d: Date | undefined) => void
}

function PeriodRow(props: PeriodRowProps) {
  const { mode } = props
  return (
    <div className='flex items-center gap-2'>
      <span className='text-muted-foreground w-[100px] text-sm'>{props.label}</span>
      {mode === 'monthly' ? (
        <MonthPicker selected={props.startDate} onSelect={props.onStartChange} />
      ) : (
        <DatePicker selected={props.startDate} onSelect={props.onStartChange} />
      )}
      {mode === 'custom' && (
        <>
          <span className='text-muted-foreground text-sm'>~</span>
          <DatePicker selected={props.endDate} onSelect={props.onEndChange} />
        </>
      )}
    </div>
  )
}

interface MonthPickerProps {
  selected: Date | undefined
  onSelect: (d: Date | undefined) => void
}

/** 月份选择控件（HTML5 原生 input[type=month]，值 = YYYY-MM） */
function MonthPicker(props: MonthPickerProps) {
  const value = props.selected ? dayjs(props.selected).format('YYYY-MM') : ''
  return (
    <Input
      type='month'
      value={value}
      onChange={(e) => {
        const v = e.target.value // "YYYY-MM" 或 ""
        if (!v) {
          props.onSelect(undefined)
          return
        }
        props.onSelect(dayjs(v + '-01').toDate())
      }}
      className='w-[200px]'
    />
  )
}

interface TrendChartProps {
  data: VipStatsTrend | null
  loading: boolean
  t: (key: string) => string
}

function TrendChart(props: TrendChartProps) {
  const { data, t } = props
  const chartRows = useMemo(() => {
    if (!data) return []
    // x 轴对齐：两条线长度后端已截断成等长，直接 zip
    return data.current.buckets.map((label, i) => ({
      label,
      current: quotaToUsd(data.current.values[i] ?? 0),
      compare: quotaToUsd(data.compare.values[i] ?? 0),
    }))
  }, [data])

  if (props.loading) {
    return (
      <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
        <Loader2 className='mr-2 h-4 w-4 animate-spin' />
        {t('Loading...')}
      </div>
    )
  }
  if (!data || chartRows.length === 0) {
    return (
      <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
        {t('Pick both periods then click Generate Comparison')}
      </div>
    )
  }

  return (
    <ResponsiveContainer width='100%' height='100%'>
      <LineChart data={chartRows} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
        <CartesianGrid strokeDasharray='3 3' className='stroke-border/40' />
        <XAxis
          dataKey='label'
          tick={{ fontSize: 11 }}
          interval='preserveStartEnd'
        />
        <YAxis tick={{ fontSize: 11 }} tickFormatter={(v) => `$${v}`} />
        <Tooltip
          contentStyle={{
            borderRadius: 6,
            fontSize: 12,
          }}
          formatter={(value) =>
            typeof value === 'number'
              ? formatCurrencyFromUSD(value)
              : String(value)
          }
          labelFormatter={(label) => String(label)}
        />
        <Line
          type='monotone'
          dataKey='current'
          name={t('Current')}
          stroke='#3b82f6'
          strokeWidth={2}
          dot={{ r: 3 }}
          activeDot={{ r: 5 }}
        />
        <Line
          type='monotone'
          dataKey='compare'
          name={t('Compare')}
          stroke='#ef4444'
          strokeWidth={2}
          dot={{ r: 3 }}
          activeDot={{ r: 5 }}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}

interface SummaryBoxProps {
  label: string
  value: string
  valueClass?: string
}

function SummaryBox(props: SummaryBoxProps) {
  return (
    <div className='bg-muted/40 rounded-md border px-3 py-2'>
      <div className='text-muted-foreground text-xs'>{props.label}：</div>
      <div
        className={cn(
          'mt-1 font-mono text-lg font-semibold tabular-nums',
          props.valueClass
        )}
      >
        {props.value}
      </div>
    </div>
  )
}
