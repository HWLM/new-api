/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'
import { CalendarDays } from 'lucide-react'
import { enUS, fr, ja, ru, vi, zhCN } from 'react-day-picker/locale'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

const calendarLocales = {
  en: enUS,
  zh: zhCN,
  fr,
  ru,
  ja,
  vi,
} as const

type SingleDayPickerProps = {
  value: string // YYYY-MM-DD
  onChange: (date: string) => void
  className?: string
}

// 「当日统计」专用：单日选择 + 今天/昨天/前天预设
export function SingleDayPicker({
  value,
  onChange,
  className,
}: SingleDayPickerProps) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)

  const selected = useMemo(
    () => (value ? dayjs(value).toDate() : undefined),
    [value]
  )

  const label = value || t('Select date')

  const apply = (date: string) => {
    onChange(date)
    setOpen(false)
  }

  const lang = (i18n.language ?? 'en').split('-')[0] as keyof typeof calendarLocales
  const locale = calendarLocales[lang] ?? enUS

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            className={cn(
              'w-full justify-start gap-2 px-2.5 text-sm leading-5 font-normal tabular-nums',
              !value && 'text-muted-foreground',
              className
            )}
          />
        }
      >
        <CalendarDays className='text-muted-foreground size-4 shrink-0' />
        <span className='truncate'>{label}</span>
      </PopoverTrigger>
      <PopoverContent align='start' className='w-auto p-3'>
        <div className='space-y-3'>
          <div className='flex flex-wrap gap-1.5'>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() => apply(dayjs().format('YYYY-MM-DD'))}
            >
              {t('Today')}
            </Button>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() =>
                apply(dayjs().subtract(1, 'day').format('YYYY-MM-DD'))
              }
            >
              {t('Yesterday')}
            </Button>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() =>
                apply(dayjs().subtract(2, 'day').format('YYYY-MM-DD'))
              }
            >
              {t('Day Before Yesterday')}
            </Button>
          </div>

          <Calendar
            mode='single'
            selected={selected}
            onSelect={(d) => {
              if (!d) return
              apply(dayjs(d).format('YYYY-MM-DD'))
            }}
            locale={locale}
          />
        </div>
      </PopoverContent>
    </Popover>
  )
}
