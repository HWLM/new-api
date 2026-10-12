/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  rangeToDates,
  type CreditDateRange,
  type CreditQuickRange,
} from './date-range'

export function FilterBar({
  value,
  onChange,
}: {
  value: CreditDateRange
  onChange: (next: CreditDateRange) => void
}) {
  const { t } = useTranslation()
  const [activeRange, setActiveRange] = useState<CreditQuickRange>('7d')

  const setRange = (r: Exclude<CreditQuickRange, 'custom'>) => {
    setActiveRange(r)
    onChange(rangeToDates(r))
  }

  // 自定义起止：两端都填好且顺序正确才触发查询
  const setCustomDate = (key: keyof CreditDateRange, val: string) => {
    setActiveRange('custom')
    const next = { ...value, [key]: val }
    if (next.start_date && next.end_date && next.start_date <= next.end_date) {
      onChange(next)
    }
  }

  const rangeButtons: Array<{
    key: Exclude<CreditQuickRange, 'custom'>
    label: string
  }> = [
    { key: 'today', label: t('Today') },
    { key: '3d', label: t('Last 3 Days') },
    { key: '7d', label: t('Last 7 Days') },
    { key: '1m', label: t('Last Month') },
  ]

  return (
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
      <span className='text-muted-foreground ml-2 text-sm'>
        {t('Custom Time')}
      </span>
      <Input
        type='date'
        className='w-36'
        value={value.start_date}
        onChange={(e) => setCustomDate('start_date', e.target.value)}
      />
      <span className='text-muted-foreground text-xs'>-</span>
      <Input
        type='date'
        className='w-36'
        value={value.end_date}
        onChange={(e) => setCustomDate('end_date', e.target.value)}
      />
    </div>
  )
}
