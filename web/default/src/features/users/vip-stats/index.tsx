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
import React, { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Lock, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { quotaUnitsToDollars } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { getVipStatsDetail, verifyVipStatsPassword } from '../api'
import type { VipStatsDetail } from '../types'

const SESSION_KEY = 'vip_stats_password'

const quotaToUsd = (quota: number) => quotaUnitsToDollars(quota)

const formatMonthDay = (yyyymmdd: string) => {
  const parts = yyyymmdd.split('-')
  if (parts.length !== 3) return yyyymmdd
  return `${Number(parts[1])}/${Number(parts[2])}`
}

export function VipStatsPage() {
  const [password, setPassword] = useState<string>(
    () => sessionStorage.getItem(SESSION_KEY) ?? ''
  )

  const clearPassword = useCallback(() => {
    sessionStorage.removeItem(SESSION_KEY)
    setPassword('')
  }, [])

  if (!password) {
    return (
      <PasswordGate
        onSuccess={(pwd) => {
          sessionStorage.setItem(SESSION_KEY, pwd)
          setPassword(pwd)
        }}
      />
    )
  }
  return <VipStatsContent password={password} onAuthFail={clearPassword} />
}

function PasswordGate(props: { onSuccess: (password: string) => void }) {
  const { t } = useTranslation()
  const [input, setInput] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!input.trim()) {
      toast.error(t('Please enter password'))
      return
    }
    setIsSubmitting(true)
    try {
      const res = await verifyVipStatsPassword(input)
      if (res.success) {
        props.onSuccess(input)
      } else {
        toast.error(res.message || t('Password incorrect'))
      }
    } catch {
      toast.error(t('Password verification failed'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className='bg-background flex min-h-screen items-center justify-center p-6'>
      <Card className='w-full max-w-sm'>
        <CardHeader className='items-center'>
          <div className='bg-muted mb-2 flex h-12 w-12 items-center justify-center rounded-full'>
            <Lock className='text-muted-foreground h-5 w-5' />
          </div>
          <CardTitle className='text-center'>
            {t('VIP Customer Statistics')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className='flex flex-col gap-3'>
            <Label htmlFor='vip-stats-password'>{t('Access Password')}</Label>
            <Input
              id='vip-stats-password'
              type='password'
              autoFocus
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={t('Enter access password')}
              disabled={isSubmitting}
            />
            <Button type='submit' disabled={isSubmitting} className='mt-2'>
              {isSubmitting && <Loader2 className='h-4 w-4 animate-spin' />}
              {t('Enter')}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

function VipStatsContent(props: {
  password: string
  onAuthFail: () => void
}) {
  const { t } = useTranslation()
  const { data, isLoading, isFetching, refetch } = useQuery<VipStatsDetail>({
    queryKey: ['vip-stats-detail', props.password],
    queryFn: async () => {
      const res = await getVipStatsDetail(props.password)
      if (!res.success || !res.data) {
        // 401 / 密码错（管理员改了密码）→ 清掉 sessionStorage 重新走密码门
        if (res.message?.toLowerCase().includes('password')) {
          toast.error(t('Password expired, please re-enter'))
          props.onAuthFail()
        } else {
          toast.error(res.message || t('Failed to load VIP stats'))
        }
        throw new Error(res.message || 'Failed to load VIP stats')
      }
      return res.data
    },
    retry: false,
  })

  return (
    <div className='bg-background min-h-screen p-6'>
      <div className='mx-auto flex max-w-7xl flex-col gap-4'>
        <div className='flex items-center justify-between'>
          <h1 className='text-2xl font-semibold'>
            {t('VIP Customer Statistics')}
          </h1>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => refetch()}
            disabled={isFetching}
          >
            {isFetching ? (
              <Loader2 className='h-4 w-4 animate-spin' />
            ) : (
              <RefreshCw className='h-4 w-4' />
            )}
            <span>{t('Refresh')}</span>
          </Button>
        </div>

        <SummaryCards data={data} isLoading={isLoading} />
        <DetailTable data={data} isLoading={isLoading} />
        <RequestTokenTable data={data} isLoading={isLoading} />
      </div>
    </div>
  )
}

function SummaryCards(props: {
  data: VipStatsDetail | undefined
  isLoading: boolean
}) {
  const { t } = useTranslation()
  const s = props.data?.summary
  const fmtInt = (n: number) => n.toLocaleString()
  const cards = [
    {
      label: t('Customer Count'),
      value: s ? String(s.user_count) : '-',
    },
    {
      label: t('Today Consumed ($)'),
      value: s ? formatCurrencyFromUSD(quotaToUsd(s.today_consumed)) : '-',
    },
    {
      label: t('7-Day Consumed ($)'),
      value: s ? formatCurrencyFromUSD(quotaToUsd(s.weekly_consumed)) : '-',
    },
    {
      label: t('Total Remaining ($)'),
      value: s ? formatCurrencyFromUSD(quotaToUsd(s.current_remaining)) : '-',
    },
    {
      label: t('Today Requests'),
      value: s ? fmtInt(s.today_requests) : '-',
    },
    {
      label: t('Today Tokens'),
      value: s ? fmtInt(s.today_tokens) : '-',
    },
    {
      label: t('7-Day Requests'),
      value: s ? fmtInt(s.weekly_requests) : '-',
    },
    {
      label: t('7-Day Tokens'),
      value: s ? fmtInt(s.weekly_tokens) : '-',
    },
  ]

  return (
    <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4'>
      {cards.map((c) => (
        <Card key={c.label}>
          <CardHeader className='pb-2'>
            <CardTitle className='text-muted-foreground text-sm font-normal'>
              {c.label}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className='text-2xl font-semibold tabular-nums'>
              {props.isLoading ? '…' : c.value}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function DetailTable(props: {
  data: VipStatsDetail | undefined
  isLoading: boolean
}) {
  const { t } = useTranslation()
  const data = props.data
  const dates = data?.dates ?? []
  const rawRows = data?.rows ?? []
  const totals = data?.totals ?? []
  // 按"今天"列消耗倒序；并列时按 user_id 升序（稳定 fallback）
  // 第二张表不受影响（保留后端原顺序）
  const rows = useMemo(() => {
    const sorted = [...rawRows]
    sorted.sort((a, b) => {
      const ta = a.daily[a.daily.length - 1] ?? 0
      const tb = b.daily[b.daily.length - 1] ?? 0
      if (ta !== tb) return tb - ta
      return a.user_id - b.user_id
    })
    return sorted
  }, [rawRows])

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {t('Customer Consumption Detail ($)')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Customer')}</TableHead>
              {dates.map((d) => (
                <TableHead key={d} className='text-center'>
                  {formatMonthDay(d)}
                </TableHead>
              ))}
              <TableHead className='text-center'>{t('Remaining')}</TableHead>
              <TableHead className='text-center'>
                {t('Business Channel')}
              </TableHead>
              <TableHead className='text-center'>{t('Inviter')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data && rows.length > 0 && (
              <TableRow className='bg-amber-50 hover:bg-amber-50 dark:bg-amber-950/30 dark:hover:bg-amber-950/30'>
                <TableCell className='font-medium'>{t('Total')}</TableCell>
                {totals.map((v, i) => (
                  <TableCell
                    key={i}
                    className='text-center font-medium tabular-nums'
                  >
                    {formatCurrencyFromUSD(quotaToUsd(v))}
                  </TableCell>
                ))}
                <TableCell className='text-muted-foreground text-center'>
                  /
                </TableCell>
                <TableCell className='text-muted-foreground text-center'>
                  /
                </TableCell>
                <TableCell className='text-muted-foreground text-center'>
                  /
                </TableCell>
              </TableRow>
            )}

            {rows.map((r) => (
              <TableRow key={r.user_id}>
                <TableCell className='font-medium'>{r.username}</TableCell>
                {r.daily.map((v, i) => (
                  <TableCell
                    key={i}
                    className={cn(
                      'text-center tabular-nums',
                      v === 0 && 'text-muted-foreground'
                    )}
                  >
                    {formatCurrencyFromUSD(quotaToUsd(v))}
                  </TableCell>
                ))}
                <TableCell className='text-center tabular-nums'>
                  {formatCurrencyFromUSD(quotaToUsd(r.remaining))}
                </TableCell>
                <TableCell className='text-center'>
                  {r.inviter_business_channel || ''}
                </TableCell>
                <TableCell className='text-center'>
                  {r.inviter_username || ''}
                </TableCell>
              </TableRow>
            ))}

            {!props.isLoading && rows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={dates.length + 4}
                  className='text-muted-foreground text-center'
                >
                  {t('No VIP customers found')}
                </TableCell>
              </TableRow>
            )}
            {props.isLoading && (
              <TableRow>
                <TableCell
                  colSpan={Math.max(dates.length + 4, 11)}
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
  )
}

/**
 * 第二张表格："客户请求次数/token 数"。每个客户占两行(请求次数 / 消耗 TOKEN)，
 * 列头跟第一张表一样是动态 7 天日期 + 合计列。
 */
function RequestTokenTable(props: {
  data: VipStatsDetail | undefined
  isLoading: boolean
}) {
  const { t } = useTranslation()
  const data = props.data
  const dates = data?.dates ?? []
  const rows = data?.rows ?? []
  const totalRequests = data?.total_requests ?? []
  const totalTokens = data?.total_tokens ?? []
  const fmtInt = (n: number) => n.toLocaleString()

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          {t('Customer Requests / Tokens Detail')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Customer')}</TableHead>
              <TableHead>{t('Dimension')}</TableHead>
              {dates.map((d) => (
                <TableHead key={d} className='text-center'>
                  {formatMonthDay(d)}
                </TableHead>
              ))}
              <TableHead className='text-center'>{t('Total')}</TableHead>
              <TableHead className='text-center'>
                {t('Business Channel')}
              </TableHead>
              <TableHead className='text-center'>{t('Inviter')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {/* 合计两行：所有客户的每日请求次数 / token 合计 */}
            {data && rows.length > 0 && (
              <>
                <TableRow className='bg-amber-50 hover:bg-amber-50 dark:bg-amber-950/30 dark:hover:bg-amber-950/30'>
                  <TableCell rowSpan={2} className='align-middle font-medium'>
                    {t('Total')}
                  </TableCell>
                  <TableCell className='font-medium'>
                    {t('Requests')}
                  </TableCell>
                  {totalRequests.map((v, i) => (
                    <TableCell
                      key={i}
                      className='text-center font-medium tabular-nums'
                    >
                      {fmtInt(v)}
                    </TableCell>
                  ))}
                  <TableCell className='text-center font-medium tabular-nums'>
                    {fmtInt(totalRequests.reduce((a, b) => a + b, 0))}
                  </TableCell>
                  <TableCell
                    rowSpan={2}
                    className='text-muted-foreground text-center align-middle'
                  >
                    /
                  </TableCell>
                  <TableCell
                    rowSpan={2}
                    className='text-muted-foreground text-center align-middle'
                  >
                    /
                  </TableCell>
                </TableRow>
                <TableRow className='bg-amber-50 hover:bg-amber-50 dark:bg-amber-950/30 dark:hover:bg-amber-950/30'>
                  <TableCell className='font-medium'>
                    {t('Tokens')}
                  </TableCell>
                  {totalTokens.map((v, i) => (
                    <TableCell
                      key={i}
                      className='text-center font-medium tabular-nums'
                    >
                      {fmtInt(v)}
                    </TableCell>
                  ))}
                  <TableCell className='text-center font-medium tabular-nums'>
                    {fmtInt(totalTokens.reduce((a, b) => a + b, 0))}
                  </TableCell>
                </TableRow>
              </>
            )}

            {/* 每个客户两行：请求次数 / 消耗 TOKEN */}
            {rows.map((r) => {
              const rowRequestsTotal = r.daily_requests.reduce(
                (a, b) => a + b,
                0
              )
              const rowTokensTotal = r.daily_tokens.reduce((a, b) => a + b, 0)
              return (
                <React.Fragment key={r.user_id}>
                  <TableRow>
                    <TableCell rowSpan={2} className='align-middle font-medium'>
                      {r.username}
                    </TableCell>
                    <TableCell>{t('Requests')}</TableCell>
                    {r.daily_requests.map((v, i) => (
                      <TableCell
                        key={i}
                        className={cn(
                          'text-center tabular-nums',
                          v === 0 && 'text-muted-foreground'
                        )}
                      >
                        {fmtInt(v)}
                      </TableCell>
                    ))}
                    <TableCell className='text-center tabular-nums'>
                      {fmtInt(rowRequestsTotal)}
                    </TableCell>
                    <TableCell
                      rowSpan={2}
                      className='text-center align-middle'
                    >
                      {r.inviter_business_channel || ''}
                    </TableCell>
                    <TableCell
                      rowSpan={2}
                      className='text-center align-middle'
                    >
                      {r.inviter_username || ''}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell>{t('Tokens')}</TableCell>
                    {r.daily_tokens.map((v, i) => (
                      <TableCell
                        key={i}
                        className={cn(
                          'text-center tabular-nums',
                          v === 0 && 'text-muted-foreground'
                        )}
                      >
                        {fmtInt(v)}
                      </TableCell>
                    ))}
                    <TableCell className='text-center tabular-nums'>
                      {fmtInt(rowTokensTotal)}
                    </TableCell>
                  </TableRow>
                </React.Fragment>
              )
            })}

            {!props.isLoading && rows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={dates.length + 5}
                  className='text-muted-foreground text-center'
                >
                  {t('No VIP customers found')}
                </TableCell>
              </TableRow>
            )}
            {props.isLoading && (
              <TableRow>
                <TableCell
                  colSpan={Math.max(dates.length + 5, 12)}
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
  )
}
