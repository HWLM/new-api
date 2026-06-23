/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { Button } from '@/components/ui/button'
import { DatePicker } from '@/components/date-picker'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { MultiSelect } from '@/components/multi-select'
import type { DetailsFilter, FilterOptions } from './types'

// 该组件同时服务汇总 / 按天两种模式：
//   mode='summary' → 显示「最后一次消耗日期」单 DatePicker
//   mode='daily'   → 显示「开始日期 ~ 结束日期」双 DatePicker
// 时间选择立即触发 onDateChange（特殊回调，不走 onChange + 查询按钮流程）。
// 其他筛选（用户名、渠道、销售、分组、VIP）仍需点查询按钮。
type FilterMode = 'summary' | 'daily'

type DetailsFilterBarProps = {
  mode: FilterMode
  value: DetailsFilter
  onChange: (next: DetailsFilter) => void
  options: FilterOptions | undefined
  userGroupOptions: string[]
  // daily 模式专用的日期范围；mode='daily' 时由父组件维护
  dailyStartDate?: string
  dailyEndDate?: string
  onDailyDateChange?: (start: string, end: string) => void
}

export function DetailsFilterBar({
  mode,
  value,
  onChange,
  options,
  userGroupOptions,
  dailyStartDate,
  dailyEndDate,
  onDailyDateChange,
}: DetailsFilterBarProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<DetailsFilter>(value)
  const [vipKey, setVipKey] = useState(0)

  // 切 mode / 父组件 value 变了 → 同步 draft 状态
  useEffect(() => {
    setDraft(value)
  }, [value, mode])

  const apply = () => onChange({ ...draft, page: 1 })
  const reset = () => {
    const def: DetailsFilter = {
      username: '',
      channel: [],
      sales: [],
      user_group: [],
      is_vip: '',
      last_consume_date_from: '',
      page: 1,
      page_size: draft.page_size ?? 20,
      sort_by: '',
      sort_dir: 'desc',
    }
    setDraft(def)
    setVipKey((k) => k + 1)
    onChange(def)
    if (mode === 'daily' && onDailyDateChange) {
      // 重置 daily 日期范围为近 7 天
      const today = dayjs()
      const start = today.subtract(6, 'day').format('YYYY-MM-DD')
      const end = today.format('YYYY-MM-DD')
      onDailyDateChange(start, end)
    }
  }

  // summary 模式：最后一次消耗日期，即时触发
  const setLastConsumeDate = (d: Date | undefined) => {
    const formatted = d ? dayjs(d).format('YYYY-MM-DD') : ''
    const next = { ...draft, last_consume_date_from: formatted, page: 1 }
    setDraft(next)
    onChange(next)
  }
  const lastConsumeDateValue = draft.last_consume_date_from
    ? dayjs(draft.last_consume_date_from).toDate()
    : undefined

  // daily 模式：日期范围，即时触发
  const dailyStart = dailyStartDate ? dayjs(dailyStartDate).toDate() : undefined
  const dailyEnd = dailyEndDate ? dayjs(dailyEndDate).toDate() : undefined
  const setDailyStart = (d: Date | undefined) => {
    if (!d || !onDailyDateChange) return
    const s = dayjs(d).format('YYYY-MM-DD')
    const e = dailyEndDate ?? s
    onDailyDateChange(s, e < s ? s : e)
  }
  const setDailyEnd = (d: Date | undefined) => {
    if (!d || !onDailyDateChange) return
    const e = dayjs(d).format('YYYY-MM-DD')
    const s = dailyStartDate ?? e
    onDailyDateChange(s > e ? e : s, e)
  }

  const vipItems = [
    { value: '1', label: t('Yes') },
    { value: '0', label: t('No') },
  ]

  return (
    <div className='flex flex-wrap items-center gap-2'>
      {mode === 'daily' ? (
        <div className='flex items-center gap-1'>
          <DatePicker
            selected={dailyStart}
            onSelect={setDailyStart}
            placeholder={t('Start Date')}
          />
          <span className='text-muted-foreground text-xs'>-</span>
          <DatePicker
            selected={dailyEnd}
            onSelect={setDailyEnd}
            placeholder={t('End Date')}
          />
        </div>
      ) : (
        <DatePicker
          selected={lastConsumeDateValue}
          onSelect={setLastConsumeDate}
          placeholder={t('Last Consume Date')}
        />
      )}

      <Input
        placeholder={t('Username or display name')}
        value={draft.username ?? ''}
        onChange={(e) => setDraft({ ...draft, username: e.target.value })}
        className='w-48'
      />

      <div className='w-52'>
        <MultiSelect
          options={(options?.channels ?? []).map((c) => ({ label: c, value: c }))}
          selected={draft.channel ?? []}
          onChange={(vals) => setDraft({ ...draft, channel: vals })}
          placeholder={t('Please select channel')}
        />
      </div>

      <div className='w-52'>
        <MultiSelect
          options={(options?.sales ?? []).map((s) => ({ label: s, value: s }))}
          selected={draft.sales ?? []}
          onChange={(vals) => setDraft({ ...draft, sales: vals })}
          placeholder={t('Please select sales')}
        />
      </div>

      <div className='w-44'>
        <MultiSelect
          options={userGroupOptions.map((g) => ({ label: g, value: g }))}
          selected={draft.user_group ?? []}
          onChange={(vals) => setDraft({ ...draft, user_group: vals })}
          placeholder={t('User Group')}
        />
      </div>

      <Select
        key={`vip-${vipKey}`}
        items={vipItems}
        value={draft.is_vip || undefined}
        onValueChange={(v) =>
          setDraft({
            ...draft,
            is_vip: (v ?? '') as DetailsFilter['is_vip'],
          })
        }
      >
        <SelectTrigger className='w-36'>
          <SelectValue placeholder={t('VIP Customer?')} />
        </SelectTrigger>
        <SelectContent>
          {vipItems.map((it) => (
            <SelectItem key={it.value} value={it.value}>
              {it.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Button onClick={apply}>{t('Search')}</Button>
      <Button variant='outline' onClick={reset}>
        {t('Reset')}
      </Button>
    </div>
  )
}
