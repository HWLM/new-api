/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { TablePlaceholderRows } from './table-states'
import type { CreditMonthlyListResp, CreditMonthlyRow } from './types'

function formatAmount(n: number): string {
  return n.toFixed(2)
}

export function DetailsMonthlyTable({
  data,
  isLoading,
  page,
  pageSize,
  onPageChange,
  onRepay,
  onViewRecords,
}: {
  data: CreditMonthlyListResp | undefined
  isLoading: boolean
  page: number
  pageSize: number
  onPageChange: (next: number) => void
  onRepay: (row: CreditMonthlyRow) => void
  onViewRecords: (row: CreditMonthlyRow) => void
}) {
  const { t } = useTranslation()
  const rows = data?.rows ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className='space-y-3'>
      {/* 橙色汇总条：整个筛选范围（未分页）的授信与回款合计 */}
      <div className='flex flex-wrap gap-6 rounded-md bg-amber-50 px-4 py-2 text-sm dark:bg-amber-950/40'>
        <span>
          {t('Credit Amount')}:{' '}
          <span className='tabular-nums'>
            {formatAmount(data?.summary_credit ?? 0)}
          </span>
        </span>
        <span>
          {t('Repayment Amount')}:{' '}
          <span className='tabular-nums'>
            {formatAmount(data?.summary_repayment ?? 0)}
          </span>
        </span>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Month')}</TableHead>
            <TableHead>{t('Customer')}</TableHead>
            <TableHead>{t('Display Name')}</TableHead>
            <TableHead>{t('Business Channel')}</TableHead>
            <TableHead>{t('Sales')}</TableHead>
            <TableHead className='text-right'>
              {t('Beginning Outstanding Credit')}
            </TableHead>
            <TableHead className='text-right'>
              {t('Credit Amount This Month')}
            </TableHead>
            <TableHead className='text-right'>
              {t('Repayment Amount This Month')}
            </TableHead>
            <TableHead className='text-right'>
              {t('Ending Outstanding Credit')}
            </TableHead>
            <TableHead>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading || rows.length === 0 ? (
            <TablePlaceholderRows isLoading={isLoading} colSpan={10} />
          ) : (
            rows.map((row) => (
              <TableRow key={`${row.month}-${row.user_id}`}>
                <TableCell>{row.month}</TableCell>
                <TableCell>{row.username}</TableCell>
                <TableCell>{row.display_name}</TableCell>
                <TableCell>{row.business_channel}</TableCell>
                <TableCell>{row.inviter_display_name}</TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.beginning_outstanding)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.month_credit)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.month_repayment)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.ending_outstanding)}
                </TableCell>
                <TableCell>
                  <div className='flex gap-1'>
                    <Button
                      variant='link'
                      size='sm'
                      className='h-auto p-0'
                      onClick={() => onRepay(row)}
                    >
                      {t('Repayment')}
                    </Button>
                    <Button
                      variant='link'
                      size='sm'
                      className='h-auto p-0'
                      onClick={() => onViewRecords(row)}
                    >
                      {t('View Details')}
                    </Button>
                  </div>
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
          onClick={() => onPageChange(page - 1)}
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
          onClick={() => onPageChange(page + 1)}
        >
          {t('Next')}
        </Button>
      </div>
    </div>
  )
}
