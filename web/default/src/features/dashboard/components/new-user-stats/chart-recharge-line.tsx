/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { RechargeTrendPoint } from './types'

// "2026-06-12" → "6-12"，对应截图里的 x 轴风格
function toShortLabel(date: string): string {
  const parts = date.split('-')
  if (parts.length !== 3) return date
  return `${parseInt(parts[1], 10)}-${parts[2]}`
}

export function ChartRechargeLine({
  data,
  isLoading,
}: {
  data: RechargeTrendPoint[] | undefined
  isLoading: boolean
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>{t('Recharge')}</CardTitle>
        <div className='text-muted-foreground text-xs'>{t('(Amount ¥)')}</div>
      </CardHeader>
      <CardContent className='h-80'>
        {isLoading ? (
          <Skeleton className='h-full w-full' />
        ) : !data || data.length === 0 ? (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No data')}
          </div>
        ) : (
          <ResponsiveContainer width='100%' height='100%'>
            <LineChart
              data={data.map((p) => ({ ...p, label: toShortLabel(p.date) }))}
              margin={{ top: 8, right: 16, left: 0, bottom: 8 }}
            >
              <CartesianGrid strokeDasharray='3 3' />
              <XAxis dataKey='label' tick={{ fontSize: 12 }} />
              <YAxis tick={{ fontSize: 12 }} />
              <Tooltip
                labelFormatter={(_, entries) => entries?.[0]?.payload?.date ?? ''}
                formatter={(value) => [`¥${Number(value ?? 0).toFixed(2)}`, t('Recharge')]}
              />
              <Line
                type='monotone'
                dataKey='recharge_cny'
                stroke='#ef4444'
                strokeWidth={2}
                dot={{ r: 3 }}
                activeDot={{ r: 5 }}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}
