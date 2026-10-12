/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fetchCreditRecords } from './api'
import { TablePlaceholderRows } from './table-states'
import type { CreditRecordType } from './types'

const PAGE_SIZE = 10

// recentMonthRange 返回「近一个月」的月份区间（上月 ~ 本月），作为弹窗打开时的默认筛选。
function recentMonthRange(): { start: string; end: string } {
  const now = dayjs()
  return {
    start: now.subtract(1, 'month').format('YYYY-MM'),
    end: now.format('YYYY-MM'),
  }
}

export type RecordsTarget = {
  user_id: number
  username: string
}

export function RecordsDialog({
  target,
  onOpenChange,
}: {
  target: RecordsTarget | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [recordType, setRecordType] = useState<CreditRecordType>('credit')
  const [startMonth, setStartMonth] = useState('')
  const [endMonth, setEndMonth] = useState('')
  // 已提交的月份区间：输入框改动要等点「查询」才生效
  const [appliedRange, setAppliedRange] = useState({ start: '', end: '' })
  const [page, setPage] = useState(1)

  useEffect(() => {
    if (target) {
      // 默认查近一个月（上月至本月），避免一打开就全量扫历史
      const { start, end } = recentMonthRange()
      setRecordType('credit')
      setStartMonth(start)
      setEndMonth(end)
      setAppliedRange({ start, end })
      setPage(1)
    }
  }, [target])

  const targetUserId = target?.user_id
  const { data, isLoading } = useQuery({
    queryKey: [
      'credit-records',
      targetUserId,
      recordType,
      appliedRange.start,
      appliedRange.end,
      page,
    ],
    queryFn: () =>
      fetchCreditRecords({
        user_id: targetUserId as number,
        type: recordType,
        start_month: appliedRange.start || undefined,
        end_month: appliedRange.end || undefined,
        page,
        page_size: PAGE_SIZE,
      }),
    enabled: targetUserId !== undefined,
  })

  const rows = data?.rows ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const switchType = (next: CreditRecordType) => {
    setRecordType(next)
    setPage(1)
  }

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Operation Records')}</DialogTitle>
        </DialogHeader>

        <div className='space-y-4'>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              type='month'
              className='w-36'
              value={startMonth}
              onChange={(e) => setStartMonth(e.target.value)}
              placeholder={t('Start month')}
            />
            <span className='text-muted-foreground'>-</span>
            <Input
              type='month'
              className='w-36'
              value={endMonth}
              onChange={(e) => setEndMonth(e.target.value)}
              placeholder={t('End month')}
            />
            <Button
              onClick={() => {
                setAppliedRange({ start: startMonth, end: endMonth })
                setPage(1)
              }}
            >
              {t('Query')}
            </Button>
          </div>

          <div className='flex gap-2'>
            <Button
              variant={recordType === 'credit' ? 'default' : 'outline'}
              size='sm'
              onClick={() => switchType('credit')}
            >
              {t('Credit Records')}
            </Button>
            <Button
              variant={recordType === 'repayment' ? 'default' : 'outline'}
              size='sm'
              onClick={() => switchType('repayment')}
            >
              {t('Repayment Records')}
            </Button>
          </div>

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Time')}</TableHead>
                <TableHead className='text-right'>
                  {t('Operation Amount')}
                </TableHead>
                <TableHead>{t('Remark')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading || rows.length === 0 ? (
                <TablePlaceholderRows
                  isLoading={isLoading}
                  colSpan={3}
                  rowCount={3}
                />
              ) : (
                rows.map((r) => (
                  <TableRow key={`${r.created_at}-${r.amount}-${r.remark}`}>
                    <TableCell>
                      {dayjs.unix(r.created_at).format('YYYY-MM-DD HH:mm:ss')}
                    </TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {r.amount.toFixed(2)}
                    </TableCell>
                    <TableCell className='max-w-xs truncate'>
                      {r.remark}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>

          <div className='text-muted-foreground flex items-center justify-end gap-2 text-sm'>
            <span>
              {t('Total')} {total}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              {t('Previous')}
            </Button>
            <span className='tabular-nums'>
              {page} / {totalPages}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={page >= totalPages}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            >
              {t('Next')}
            </Button>
          </div>
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
