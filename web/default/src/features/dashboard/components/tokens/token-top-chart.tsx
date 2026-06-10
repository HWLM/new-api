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
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { useTranslation } from 'react-i18next'
import { VCHART_OPTION } from '@/lib/vchart'
import { useTheme } from '@/context/theme-provider'
import { formatQuota } from '@/lib/format'
import { Skeleton } from '@/components/ui/skeleton'
import { getTokenStatsTop } from '@/features/dashboard/api'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

const TOP_LIMIT = 10

export function TokenTopChart() {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  useEffect(() => {
    const update = async () => {
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
    update()
  }, [resolvedTheme])

  const { data, isLoading } = useQuery({
    queryKey: ['token-stats-top', TOP_LIMIT],
    queryFn: () => getTokenStatsTop(TOP_LIMIT),
    select: (res) => (res.success ? res.data : []),
    staleTime: 60_000,
  })

  const spec = useMemo(() => {
    const items = (data ?? []).map((it) => ({
      Token: it.token_name || `#${it.token_id}`,
      rawQuota: it.quota,
    }))
    return {
      type: 'bar',
      data: [{ id: 'tokenTop', values: items }],
      xField: 'rawQuota',
      yField: 'Token',
      seriesField: 'Token',
      direction: 'horizontal',
      legends: { visible: false },
      label: {
        visible: true,
        position: 'outside',
        formatMethod: (v: number) => formatQuota(Number(v) || 0),
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
              key: (datum: Record<string, unknown>) => datum?.Token,
              value: (datum: Record<string, unknown>) =>
                formatQuota(Number(datum?.rawQuota) || 0),
            },
          ],
        },
      },
      background: 'transparent',
    }
  }, [data])

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex items-center justify-between border-b px-4 py-3 sm:px-5'>
        <div className='text-sm font-semibold'>
          {t('Token Consumption Today Top')}{' '}
          <span className='text-muted-foreground'>({TOP_LIMIT})</span>
        </div>
      </div>
      <div className='h-80 p-2'>
        {isLoading || !themeReady ? (
          <Skeleton className='h-full w-full' />
        ) : (data?.length ?? 0) === 0 ? (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('No data')}
          </div>
        ) : (
          <VChart spec={spec} option={VCHART_OPTION} />
        )}
      </div>
    </div>
  )
}
