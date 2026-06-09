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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Skeleton } from '@/components/ui/skeleton'
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
import { getTokenStatsExhausting } from '@/features/dashboard/api'

const PAGE_SIZE = 10
const REFRESH_INTERVAL = 5 * 60 * 1000

export function TokenExhaustingList() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)

  const { data, isLoading, isRefetching, refetch } = useQuery({
    queryKey: ['token-stats-exhausting', page],
    queryFn: () => getTokenStatsExhausting(page, PAGE_SIZE),
    refetchInterval: REFRESH_INTERVAL,
    refetchOnWindowFocus: false,
  })

  const items = data?.data.items ?? []
  const total = data?.data.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3 sm:px-5'>
        <div className='flex items-baseline gap-2 text-sm font-semibold'>
          <span>{t('Exhausting Soon')}</span>
          <span className='text-muted-foreground text-xs font-normal'>
            ({t('Summary: tokens with remaining quota below 5%')})
          </span>
        </div>
        <div className='text-muted-foreground ml-auto flex items-center gap-1 text-xs whitespace-nowrap'>
          <span>
            {t('Total {{count}} tokens', { count: total })}（
            {t('Updated every 5 minutes')}）
          </span>
          <button
            type='button'
            onClick={() => refetch()}
            className='hover:text-foreground inline-flex items-center justify-center rounded p-1 transition'
            aria-label={t('Refresh')}
          >
            <RefreshCw
              className={`size-3.5 ${isRefetching ? 'animate-spin' : ''}`}
            />
          </button>
        </div>
      </div>

      <div className='min-h-[18rem]'>
        {isLoading ? (
          <div className='p-4 space-y-2'>
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className='h-7 w-full' />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className='text-muted-foreground flex h-72 items-center justify-center text-sm'>
            {t('No data')}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-16'>{t('Serial No.')}</TableHead>
                <TableHead>{t('Secret Key Name')}</TableHead>
                <TableHead>{t('Key')}</TableHead>
                <TableHead>{t('Group')}</TableHead>
                <TableHead className='text-right'>{t('Consumed')}</TableHead>
                <TableHead className='text-right'>
                  {t('Remaining Quota')}
                </TableHead>
                <TableHead className='text-right'>
                  {t('Remaining %')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((it, idx) => (
                <TableRow key={it.id}>
                  <TableCell>{(page - 1) * PAGE_SIZE + idx + 1}</TableCell>
                  <TableCell className='font-medium'>
                    {it.token_name || `#${it.token_id}`}
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {it.token_key || '-'}
                  </TableCell>
                  <TableCell>{it.group_name || '-'}</TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {formatQuota(it.used_quota)}
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {formatQuota(it.remain_quota)}
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums text-rose-500 dark:text-rose-400'>
                    {(it.remain_ratio * 100).toFixed(1)}%
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      {total > PAGE_SIZE && (
        <div className='border-t px-4 py-2 sm:px-5'>
          <Pagination>
            <PaginationContent>
              <PaginationItem>
                <PaginationPrevious
                  href='#'
                  onClick={(e) => {
                    e.preventDefault()
                    if (page > 1) setPage(page - 1)
                  }}
                  className={page === 1 ? 'pointer-events-none opacity-50' : ''}
                />
              </PaginationItem>
              <PaginationItem>
                <PaginationLink href='#' isActive>
                  {page}/{totalPages}
                </PaginationLink>
              </PaginationItem>
              <PaginationItem>
                <PaginationNext
                  href='#'
                  onClick={(e) => {
                    e.preventDefault()
                    if (page < totalPages) setPage(page + 1)
                  }}
                  className={
                    page === totalPages ? 'pointer-events-none opacity-50' : ''
                  }
                />
              </PaginationItem>
            </PaginationContent>
          </Pagination>
        </div>
      )}
    </div>
  )
}
