/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { VChart } from '@visactor/react-vchart'
import { useTranslation } from 'react-i18next'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { VCHART_OPTION } from '@/lib/vchart'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { TopUserRow } from './types'

// 与 dashboard/lib/charts.ts 中的 USER_COLOR_FALLBACKS 保持一致（主题色板不可用时兜底）
const USER_COLOR_FALLBACKS = [
  '#5B8FF9',
  '#5AD8A6',
  '#F6BD16',
  '#E8684A',
  '#6DC8EC',
  '#9270CA',
  '#FF9D4D',
  '#269A99',
  '#FF99C3',
  '#5D7092',
]

// VChart ThemeManager 是异步动态导入的；用 module-level promise 防重复加载
let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

export function ChartTopUsers({
  data,
  isLoading,
}: {
  data: TopUserRow[] | undefined
  isLoading: boolean
}) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  // ThemeManager 初始化（参考 features/dashboard/components/users/user-charts.tsx）
  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  // 构造 spec_user_rank 风格的 spec
  const spec = useMemo(() => {
    const values = (data ?? []).map((r) => ({
      User: r.username || `#${r.user_id}`,
      Usage: r.consumed_usd,
    }))
    const colorMap: Record<string, string> = {}
    values.forEach((v, i) => {
      colorMap[v.User] = USER_COLOR_FALLBACKS[i % USER_COLOR_FALLBACKS.length]
    })
    return {
      type: 'bar',
      data: [{ id: 'topUsersData', values }],
      xField: 'Usage',
      yField: 'User',
      seriesField: 'User',
      direction: 'horizontal',
      legends: { visible: false },
      bar: {
        state: { hover: { stroke: '#000', lineWidth: 1 } },
      },
      label: {
        visible: true,
        position: 'outside',
        formatMethod: (value: number) => `$${(Number(value) || 0).toFixed(2)}`,
        style: { fontSize: 11 },
      },
      axes: [
        { orient: 'left', type: 'band' },
        { orient: 'bottom', type: 'linear', visible: false },
      ],
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum?.User as string,
              value: (datum: Record<string, unknown>) =>
                `$${(Number(datum?.Usage) || 0).toFixed(2)}`,
            },
          ],
        },
      },
      color: { specified: colorMap },
      background: { fill: 'transparent' },
      animation: true,
    } as const
  }, [data])

  const isEmpty = !data || data.length === 0

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {t('User Consumption Top 10')}
        </CardTitle>
      </CardHeader>
      <CardContent className='h-80'>
        {isLoading ? (
          <Skeleton className='h-full w-full' />
        ) : isEmpty ? (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No data')}
          </div>
        ) : (
          themeReady && (
            <VChart
              key={`top-users-${resolvedTheme}-${customization.preset}`}
              spec={{
                ...spec,
                theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              option={VCHART_OPTION}
            />
          )
        )}
      </CardContent>
    </Card>
  )
}
