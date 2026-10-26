/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import { fetchAmountDetails } from './api'
import { TablePlaceholderRows } from './table-states'
import type { AmountDetailsDimension } from './types'

const PAGE_SIZE = 20

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  dimension: AmountDetailsDimension
  dimensionLabel: string
  startDate: string
  endDate: string
}

export function AmountDetailsDialog({
  open,
  onOpenChange,
  dimension,
  dimensionLabel,
  startDate,
  endDate,
}: Props) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)

  const { data, isLoading } = useQuery({
    queryKey: ['amount-details', dimension, startDate, endDate, page],
    queryFn: () =>
      fetchAmountDetails({
        dimension,
        start_date: startDate,
        end_date: endDate,
        page,
        page_size: PAGE_SIZE,
      }),
    enabled: open,
  })

  const rows = data?.rows ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='flex max-h-[80vh] max-w-3xl flex-col'>
        <DialogHeader>
          <DialogTitle>
            {dimensionLabel} {t('Details')}
          </DialogTitle>
        </DialogHeader>

        <div className='flex-1 overflow-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Customer')}</TableHead>
                <TableHead>{t('Display Name')}</TableHead>
                <TableHead className='text-right'>{t('Amount (¥)')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading || rows.length === 0 ? (
                <TablePlaceholderRows isLoading={isLoading} colSpan={3} />
              ) : (
                rows.map((row) => (
                  <TableRow key={row.user_id}>
                    <TableCell>{row.username}</TableCell>
                    <TableCell>{row.display_name}</TableCell>
                    <TableCell className='text-right tabular-nums'>
                      {row.amount.toFixed(2)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        <div className='flex items-center justify-between border-t pt-4'>
          <div className='text-muted-foreground text-sm'>
            {t('Total')}: {total}
          </div>
          <Pagination className='mx-0 w-auto justify-end'>
            <PaginationContent>
              <PaginationItem>
                <PaginationPrevious
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  className={
                    page <= 1
                      ? 'pointer-events-none opacity-50'
                      : 'cursor-pointer'
                  }
                />
              </PaginationItem>
              <PaginationItem>
                <PaginationLink isActive className='pointer-events-none'>
                  {page}
                </PaginationLink>
              </PaginationItem>
              <PaginationItem>
                <PaginationNext
                  onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                  className={
                    page >= totalPages
                      ? 'pointer-events-none opacity-50'
                      : 'cursor-pointer'
                  }
                />
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        </div>
      </DialogContent>
    </Dialog>
  )
}
