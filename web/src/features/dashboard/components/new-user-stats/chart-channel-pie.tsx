/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { ChannelPieRow } from './types'

const PIE_COLORS = [
  '#3b82f6',
  '#10b981',
  '#f59e0b',
  '#ef4444',
  '#8b5cf6',
  '#14b8a6',
  '#f97316',
  '#06b6d4',
  '#a855f7',
  '#84cc16',
]

export function ChartChannelPie({
  data,
  isLoading,
}: {
  data: ChannelPieRow[] | undefined
  isLoading: boolean
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {t('Channel Consumption Top 10')}
        </CardTitle>
      </CardHeader>
      <CardContent className='h-80'>
        {isLoading ? (
          <Skeleton className='h-full w-full' />
        ) : !data || data.length === 0 ? (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No data')}
          </div>
        ) : (
          <div className='flex h-full flex-col'>
            <div className='flex-1'>
              <ResponsiveContainer width='100%' height='100%'>
                <PieChart>
                  <Pie
                    data={data}
                    dataKey='consumed_usd'
                    nameKey='channel'
                    cx='50%'
                    cy='50%'
                    innerRadius='55%'
                    outerRadius='80%'
                    paddingAngle={2}
                  >
                    {data.map((_, i) => (
                      <Cell
                        key={`cell-${i}`}
                        fill={PIE_COLORS[i % PIE_COLORS.length]}
                      />
                    ))}
                  </Pie>
                  <Tooltip
                    formatter={(value, name) => [
                      `$${Number(value ?? 0).toFixed(2)}`,
                      String(name ?? ''),
                    ]}
                  />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className='flex flex-wrap justify-center gap-x-3 gap-y-1 text-xs'>
              {data.map((row, i) => (
                <div key={row.channel} className='flex items-center gap-1'>
                  <span
                    className='h-2 w-2 rounded-full'
                    style={{
                      backgroundColor: PIE_COLORS[i % PIE_COLORS.length],
                    }}
                  />
                  <span className='text-muted-foreground'>{row.channel}</span>
                  <span className='tabular-nums'>
                    {row.percent.toFixed(1)}%
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
