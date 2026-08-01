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
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { quotaUnitsToDollars } from '@/lib/format'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getPromotionStats } from '@/features/dashboard/api'

type PromotionTablesProps = {
  /** 与 user-charts 共享的时间窗口（秒） */
  timeRange: { start_timestamp: number; end_timestamp: number }
  /** 与 user-charts 共享的 Top N（前 5/10/20/50） */
  topN: number
}

const quotaToUsd = (quota: number) => quotaUnitsToDollars(quota)

export function PromotionTables(props: PromotionTablesProps) {
  const { t } = useTranslation()

  const { data, isLoading } = useQuery({
    queryKey: ['dashboard', 'promotion', props.timeRange],
    queryFn: () => getPromotionStats(props.timeRange),
    select: (res) => (res.success ? res.data : undefined),
    staleTime: 60_000,
  })

  // 后端已按 total_consumed 倒序，前端按 topN 截取。渠道空名兜底过滤（后端 SQL 已排除，这里防御性）
  const channels = useMemo(
    () =>
      (data?.channels ?? [])
        .filter((c) => c.channel && c.channel.length > 0)
        .slice(0, props.topN),
    [data, props.topN]
  )
  const sales = useMemo(
    () => (data?.sales ?? []).slice(0, props.topN),
    [data, props.topN]
  )

  return (
    <div className='grid grid-cols-1 gap-3 lg:grid-cols-2'>
      <Card>
        <CardHeader>
          <CardTitle className='text-base'>
            {t('Channel Promotion')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Channel Name')}</TableHead>
                <TableHead className='text-center'>
                  {t('Invited Users Count')}
                </TableHead>
                <TableHead className='text-center'>
                  {t('Total Consumed ($)')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {channels.map((row) => (
                <TableRow key={row.channel}>
                  <TableCell className='font-medium'>{row.channel}</TableCell>
                  <TableCell className='text-center tabular-nums'>
                    {row.invited_count.toLocaleString()}
                  </TableCell>
                  <TableCell className='text-center tabular-nums'>
                    {formatCurrencyFromUSD(quotaToUsd(row.total_consumed))}
                  </TableCell>
                </TableRow>
              ))}
              {!isLoading && channels.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={3}
                    className='text-muted-foreground text-center'
                  >
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
              {isLoading && (
                <TableRow>
                  <TableCell
                    colSpan={3}
                    className='text-muted-foreground text-center'
                  >
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className='text-base'>{t('Sales Promotion')}</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Sales')}</TableHead>
                <TableHead className='text-center'>
                  {t('Owning Channel')}
                </TableHead>
                <TableHead className='text-center'>
                  {t('Invited Users Count')}
                </TableHead>
                <TableHead className='text-center'>
                  {t('Total Consumed ($)')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sales.map((row) => (
                <TableRow key={row.username}>
                  <TableCell className='font-medium'>{row.username}</TableCell>
                  <TableCell className='text-center'>{row.channel}</TableCell>
                  <TableCell className='text-center tabular-nums'>
                    {row.invited_count.toLocaleString()}
                  </TableCell>
                  <TableCell className='text-center tabular-nums'>
                    {formatCurrencyFromUSD(quotaToUsd(row.total_consumed))}
                  </TableCell>
                </TableRow>
              ))}
              {!isLoading && sales.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className='text-muted-foreground text-center'
                  >
                    {t('No data')}
                  </TableCell>
                </TableRow>
              )}
              {isLoading && (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className='text-muted-foreground text-center'
                  >
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
