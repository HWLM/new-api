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
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, ArrowUpDown, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination'
import { getTokenStatsDaily } from '@/features/dashboard/api'
import type { TokenDailyDetailFilters } from '@/features/dashboard/types'

const PAGE_SIZE = 10
const STATUS_ALL = 'all'
const GROUP_ALL = 'all'

type SortBy = 'date' | 'daily_quota' | 'cumulative_quota'
type SortOrder = 'asc' | 'desc'

function formatDate(ts: number): string {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function toDayStartTs(dateStr: string): number | undefined {
  if (!dateStr) return undefined
  const t = new Date(dateStr + 'T00:00:00').getTime()
  if (Number.isNaN(t)) return undefined
  return Math.floor(t / 1000)
}

export function TokenDailyDetail() {
  const { t } = useTranslation()
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [groupName, setGroupName] = useState(GROUP_ALL)
  const [status, setStatus] = useState(STATUS_ALL)
  const [tokenName, setTokenName] = useState('')

  // 已提交的筛选（用于发起查询）
  const [committed, setCommitted] = useState<TokenDailyDetailFilters>({
    page: 1,
    page_size: PAGE_SIZE,
    sort_by: 'date',
    sort_order: 'desc',
  })

  const handleQuery = () => {
    setCommitted((prev) => ({
      ...prev,
      page: 1,
      start_date: toDayStartTs(startDate),
      end_date: toDayStartTs(endDate),
      group: groupName !== GROUP_ALL ? groupName : undefined,
      status: status !== STATUS_ALL ? Number(status) : undefined,
      token_name: tokenName || undefined,
    }))
  }

  const handleReset = () => {
    setStartDate('')
    setEndDate('')
    setGroupName(GROUP_ALL)
    setStatus(STATUS_ALL)
    setTokenName('')
    setCommitted({
      page: 1,
      page_size: PAGE_SIZE,
      sort_by: 'date',
      sort_order: 'desc',
    })
  }

  const toggleSort = (col: SortBy) => {
    setCommitted((prev) => {
      if (prev.sort_by !== col) {
        return { ...prev, sort_by: col, sort_order: 'desc', page: 1 }
      }
      const next: SortOrder = prev.sort_order === 'desc' ? 'asc' : 'desc'
      return { ...prev, sort_order: next, page: 1 }
    })
  }

  const setPage = (p: number) => {
    setCommitted((prev) => ({ ...prev, page: p }))
  }

  const { data, isLoading } = useQuery({
    queryKey: ['token-stats-daily', committed],
    queryFn: () => getTokenStatsDaily(committed),
    refetchOnWindowFocus: false,
  })

  const items = data?.data.items ?? []
  const total = data?.data.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  // append-only 收集出现过的分组，避免选中某分组后下拉里其他分组消失
  const [knownGroups, setKnownGroups] = useState<Set<string>>(new Set())
  useEffect(() => {
    if (!items.length) return
    setKnownGroups((prev) => {
      let changed = false
      const next = new Set(prev)
      for (const it of items) {
        if (it.group_name && !next.has(it.group_name)) {
          next.add(it.group_name)
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [items])

  const groupItems = useMemo(
    () => [
      { value: GROUP_ALL, label: t('All') },
      ...Array.from(knownGroups)
        .sort()
        .map((g) => ({ value: g, label: g })),
    ],
    [knownGroups, t]
  )

  const statusItems = useMemo(
    () => [
      { value: STATUS_ALL, label: t('All') },
      { value: '1', label: t('Enabled') },
      { value: '2', label: t('Disabled') },
      { value: '3', label: t('Expired') },
      { value: '4', label: t('Exhausted') },
    ],
    [t]
  )

  const SortIcon = ({ col }: { col: SortBy }) => {
    if (committed.sort_by !== col)
      return <ArrowUpDown className='text-muted-foreground/40 ml-1 inline size-3.5' />
    return committed.sort_order === 'desc' ? (
      <ArrowDown className='ml-1 inline size-3.5' />
    ) : (
      <ArrowUp className='ml-1 inline size-3.5' />
    )
  }

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const filterBar = useMemo(
    () => (
      <div className='flex flex-wrap items-end gap-2'>
        <div className='flex flex-col gap-1'>
          <label className='text-muted-foreground text-xs'>{t('Time')}</label>
          <div className='flex items-center gap-1'>
            <Input
              type='date'
              value={startDate}
              onChange={(e) => setStartDate(e.target.value)}
              placeholder={t('Start date')}
              className='w-36'
            />
            <span className='text-muted-foreground text-xs'>~</span>
            <Input
              type='date'
              value={endDate}
              onChange={(e) => setEndDate(e.target.value)}
              placeholder={t('End date')}
              className='w-36'
            />
          </div>
        </div>

        <div className='flex flex-col gap-1'>
          <label className='text-muted-foreground text-xs'>{t('Group')}</label>
          <Select
            items={groupItems}
            value={groupName}
            onValueChange={setGroupName}
          >
            <SelectTrigger className='w-32'>
              <SelectValue placeholder={t('Please Select')} />
            </SelectTrigger>
            <SelectContent>
              {groupItems.map((it) => (
                <SelectItem key={it.value} value={it.value}>
                  {it.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className='flex flex-col gap-1'>
          <label className='text-muted-foreground text-xs'>{t('Status')}</label>
          <Select
            items={statusItems}
            value={status}
            onValueChange={setStatus}
          >
            <SelectTrigger className='w-32'>
              <SelectValue placeholder={t('Please Select')} />
            </SelectTrigger>
            <SelectContent>
              {statusItems.map((it) => (
                <SelectItem key={it.value} value={it.value}>
                  {it.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className='flex flex-col gap-1'>
          <label className='text-muted-foreground text-xs'>
            {t('Secret Key Name')}
          </label>
          <Input
            value={tokenName}
            onChange={(e) => setTokenName(e.target.value)}
            placeholder={t('Please Enter')}
            className='w-44'
          />
        </div>

        <div className='flex items-center gap-2'>
          <Button size='sm' onClick={handleQuery}>
            <Search className='mr-1 size-3.5' />
            {t('Query')}
          </Button>
          <Button size='sm' variant='outline' onClick={handleReset}>
            {t('Reset')}
          </Button>
        </div>
      </div>
    ),
    [startDate, endDate, groupName, status, tokenName, t, groupItems, statusItems]
  )

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='border-b px-4 py-3 sm:px-5'>
        <div className='mb-3 text-sm font-semibold'>{t('Daily Details')}</div>
        {filterBar}
      </div>

      <div className='min-h-[18rem]'>
        {isLoading ? (
          <div className='space-y-2 p-4'>
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className='h-8 w-full' />
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
                <TableHead
                  className='cursor-pointer select-none'
                  onClick={() => toggleSort('date')}
                >
                  {t('Date')}
                  <SortIcon col='date' />
                </TableHead>
                <TableHead>{t('Secret Key Name')}</TableHead>
                <TableHead>{t('Key')}</TableHead>
                <TableHead>{t('Group')}</TableHead>
                <TableHead
                  className='cursor-pointer select-none text-right'
                  onClick={() => toggleSort('daily_quota')}
                >
                  {t('Daily Used')}
                  <SortIcon col='daily_quota' />
                </TableHead>
                <TableHead
                  className='cursor-pointer select-none text-right'
                  onClick={() => toggleSort('cumulative_quota')}
                >
                  {t('Cumulative Used')}
                  <SortIcon col='cumulative_quota' />
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((it) => (
                <TableRow key={`${it.token_id}-${it.date}`}>
                  <TableCell>{formatDate(it.date)}</TableCell>
                  <TableCell className='font-medium'>
                    {it.token_name || `#${it.token_id}`}
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {it.token_key || '-'}
                  </TableCell>
                  <TableCell>{it.group_name || '-'}</TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {formatQuota(it.daily_quota)}
                  </TableCell>
                  <TableCell className='text-right font-mono tabular-nums'>
                    {formatQuota(it.cumulative_quota)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      {total > PAGE_SIZE && (
        <div className='flex items-center justify-between border-t px-4 py-2 text-xs sm:px-5'>
          <div className='text-muted-foreground'>
            {t('Total {{count}} records', { count: total })}
          </div>
          <Pagination>
            <PaginationContent>
              <PaginationItem>
                <PaginationPrevious
                  href='#'
                  onClick={(e) => {
                    e.preventDefault()
                    if ((committed.page ?? 1) > 1)
                      setPage((committed.page ?? 1) - 1)
                  }}
                  className={
                    (committed.page ?? 1) === 1
                      ? 'pointer-events-none opacity-50'
                      : ''
                  }
                />
              </PaginationItem>
              <PaginationItem>
                <PaginationLink href='#' isActive>
                  {committed.page ?? 1}/{totalPages}
                </PaginationLink>
              </PaginationItem>
              <PaginationItem>
                <PaginationNext
                  href='#'
                  onClick={(e) => {
                    e.preventDefault()
                    if ((committed.page ?? 1) < totalPages)
                      setPage((committed.page ?? 1) + 1)
                  }}
                  className={
                    (committed.page ?? 1) === totalPages
                      ? 'pointer-events-none opacity-50'
                      : ''
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
