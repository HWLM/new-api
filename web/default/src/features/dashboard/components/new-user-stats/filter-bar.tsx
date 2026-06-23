/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { MultiSelect } from '@/components/multi-select'
import type { FilterOptions, StatsFilter } from './types'

// 时间快捷选项
type QuickRange = 'today' | '3d' | '7d' | '1m' | 'custom'

function rangeToDates(range: QuickRange): {
  start_date?: string
  end_date?: string
} {
  if (range === 'custom') return {}
  const now = new Date()
  const end = now
  let start = new Date(now)
  switch (range) {
    case 'today':
      start = end
      break
    case '3d':
      start.setDate(end.getDate() - 2)
      break
    case '7d':
      start.setDate(end.getDate() - 6)
      break
    case '1m':
      start.setMonth(end.getMonth() - 1)
      start.setDate(start.getDate() + 1)
      break
  }
  return {
    start_date: formatDateLocal(start),
    end_date: formatDateLocal(end),
  }
}

function formatDateLocal(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// 是否重点客户的 items；items prop 让 Base UI Select 反查显示 label 而不是 raw value
const VIP_ITEMS_FACTORY = (yes: string, no: string) =>
  [
    { value: '1', label: yes },
    { value: '0', label: no },
  ] as const

export function FilterBar({
  value,
  onChange,
  options,
}: {
  value: StatsFilter
  onChange: (next: StatsFilter) => void
  options: FilterOptions | undefined
}) {
  const { t } = useTranslation()
  const [activeRange, setActiveRange] = useState<QuickRange>('today')
  const [draft, setDraft] = useState<StatsFilter>(value)
  // 用 key 强制 Select 在重置时重新挂载，避免 BaseUI 受控 → undefined 时 UI 不同步
  const [vipKey, setVipKey] = useState(0)

  // 快捷时间按钮：立即触发 onChange（不需要点查询）
  const setRange = (r: QuickRange) => {
    setActiveRange(r)
    const dates = rangeToDates(r)
    const next = { ...draft, ...dates }
    setDraft(next)
    onChange(next)
  }

  // 自定义起止日期：start 和 end 都有效时立即触发 onChange
  const setCustomDate = (key: 'start_date' | 'end_date', val: string) => {
    setActiveRange('custom')
    const next = { ...draft, [key]: val }
    setDraft(next)
    if (next.start_date && next.end_date && next.start_date <= next.end_date) {
      onChange(next)
    }
  }

  const applyFilter = () => {
    onChange(draft)
  }
  const resetFilter = () => {
    const def: StatsFilter = {
      ...rangeToDates('today'),
      username: '',
      channel: [],
      sales: [],
      is_vip: '',
    }
    setDraft(def)
    setActiveRange('today')
    setVipKey((k) => k + 1)
    onChange(def)
  }

  const rangeButtons: Array<{ key: QuickRange; label: string }> = [
    { key: 'today', label: t('Today') },
    { key: '3d', label: t('Last 3 Days') },
    { key: '7d', label: t('Last 7 Days') },
    { key: '1m', label: t('Last Month') },
  ]

  // MultiSelect options 由后端返回的字符串列表映射成 {label, value}
  const channelOpts = (options?.channels ?? []).map((c) => ({
    label: c,
    value: c,
  }))
  const salesOpts = (options?.sales ?? []).map((s) => ({
    label: s,
    value: s,
  }))

  const vipItems = VIP_ITEMS_FACTORY(t('Yes'), t('No'))

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center gap-2'>
        {rangeButtons.map((b) => (
          <Button
            key={b.key}
            variant={activeRange === b.key ? 'default' : 'outline'}
            size='sm'
            onClick={() => setRange(b.key)}
          >
            {b.label}
          </Button>
        ))}
        <div className='flex items-center gap-1'>
          <Input
            type='date'
            value={draft.start_date ?? ''}
            onChange={(e) => setCustomDate('start_date', e.target.value)}
            className='w-36'
          />
          <span className='text-muted-foreground text-xs'>-</span>
          <Input
            type='date'
            value={draft.end_date ?? ''}
            onChange={(e) => setCustomDate('end_date', e.target.value)}
            className='w-36'
          />
        </div>

        <Input
          placeholder={t('Please enter username')}
          value={draft.username ?? ''}
          onChange={(e) => setDraft({ ...draft, username: e.target.value })}
          className='w-48'
        />

        {/* 渠道：多选 + 搜索；未选时显示 placeholder，等于查全部 */}
        <div className='w-56'>
          <MultiSelect
            options={channelOpts}
            selected={draft.channel ?? []}
            onChange={(vals) => setDraft({ ...draft, channel: vals })}
            placeholder={t('Please select channel')}
          />
        </div>

        {/* 销售：多选 + 搜索；未选时显示 placeholder，等于查全部 */}
        <div className='w-56'>
          <MultiSelect
            options={salesOpts}
            selected={draft.sales ?? []}
            onChange={(vals) => setDraft({ ...draft, sales: vals })}
            placeholder={t('Please select sales')}
          />
        </div>

        {/* 是否重点客户：items prop 让 trigger 显示 "是/否" 而不是 raw value 1/0 */}
        <Select
          key={`vip-${vipKey}`}
          items={vipItems}
          value={draft.is_vip || undefined}
          onValueChange={(v) =>
            setDraft({
              ...draft,
              is_vip: (v ?? '') as StatsFilter['is_vip'],
            })
          }
        >
          <SelectTrigger className='w-36'>
            <SelectValue placeholder={t('VIP Customer?')} />
          </SelectTrigger>
          <SelectContent>
            {vipItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Button onClick={applyFilter}>{t('Search')}</Button>
        <Button variant='outline' onClick={resetFilter}>
          {t('Reset')}
        </Button>
      </div>
    </div>
  )
}
