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
import type {
  CreditSummaryListResp,
  CreditSummaryRow,
  CreditSummarySortKey,
} from './types'

function formatAmount(n: number): string {
  return n.toFixed(2)
}

export function DetailsSummaryTable({
  data,
  isLoading,
  page,
  pageSize,
  onPageChange,
  sortBy,
  sortDir,
  onSort,
  onRepay,
  onViewRecords,
}: {
  data: CreditSummaryListResp | undefined
  isLoading: boolean
  page: number
  pageSize: number
  onPageChange: (next: number) => void
  sortBy: CreditSummarySortKey | ''
  sortDir: 'asc' | 'desc'
  onSort: (key: CreditSummarySortKey) => void
  onRepay: (row: CreditSummaryRow) => void
  onViewRecords: (row: CreditSummaryRow) => void
}) {
  const { t } = useTranslation()
  const rows = data?.rows ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const sortIndicator = (key: CreditSummarySortKey) => {
    if (sortBy !== key) return ''
    return sortDir === 'desc' ? ' ↓' : ' ↑'
  }

  return (
    <div className='space-y-3'>
      {/* 橙色汇总条：整个筛选范围（未分页）的合计 */}
      <div className='flex flex-wrap gap-6 rounded-md bg-amber-50 px-4 py-2 text-sm dark:bg-amber-950/40'>
        <span>
          {t('Total Credit Amount')}:{' '}
          <span className='tabular-nums'>
            {formatAmount(data?.summary_credit ?? 0)}
          </span>
        </span>
        <span>
          {t('Total Repayment Amount')}:{' '}
          <span className='tabular-nums'>
            {formatAmount(data?.summary_repayment ?? 0)}
          </span>
        </span>
        <span>
          {t('Outstanding Amount')}:{' '}
          <span className='tabular-nums'>
            {formatAmount(data?.summary_outstanding ?? 0)}
          </span>
        </span>
        <span>
          {t('Total Credit Consumption Amount')}:{' '}
          <span className='tabular-nums'>
            {formatAmount(data?.summary_consumption ?? 0)}
          </span>
        </span>
        <span>
          {t('Receivable Amount')}:{' '}
          <span className='tabular-nums'>
            {formatAmount(data?.summary_receivable ?? 0)}
          </span>
        </span>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Customer')}</TableHead>
            <TableHead>{t('Display Name')}</TableHead>
            <TableHead>{t('Business Channel')}</TableHead>
            <TableHead>{t('Sales')}</TableHead>
            <TableHead
              className='cursor-pointer text-right select-none'
              onClick={() => onSort('total_credit')}
            >
              {t('Total Credit Amount')}
              {sortIndicator('total_credit')}
            </TableHead>
            <TableHead
              className='cursor-pointer text-right select-none'
              onClick={() => onSort('total_repayment')}
            >
              {t('Total Repayment Amount')}
              {sortIndicator('total_repayment')}
            </TableHead>
            <TableHead
              className='cursor-pointer text-right select-none'
              onClick={() => onSort('outstanding')}
            >
              {t('Outstanding Credit Amount')}
              {sortIndicator('outstanding')}
            </TableHead>
            <TableHead className='text-right'>
              {t('Total Credit Consumption Amount')}
            </TableHead>
            <TableHead
              className='cursor-pointer text-right select-none'
              onClick={() => onSort('receivable')}
            >
              {t('Receivable Amount')}
              {sortIndicator('receivable')}
            </TableHead>
            <TableHead>{t('Actions')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading || rows.length === 0 ? (
            <TablePlaceholderRows isLoading={isLoading} colSpan={10} />
          ) : (
            rows.map((row) => (
              <TableRow key={row.user_id}>
                <TableCell>{row.username}</TableCell>
                <TableCell>{row.display_name}</TableCell>
                <TableCell>{row.business_channel}</TableCell>
                <TableCell>{row.inviter_display_name}</TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.total_credit)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.total_repayment)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.outstanding)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.total_consumption)}
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {formatAmount(row.receivable)}
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
