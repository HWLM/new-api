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
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  type SortingState,
  type VisibilityState,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table'
import dayjs from '@/lib/dayjs'
import { useMediaQuery } from '@/hooks'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import {
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePage,
} from '@/components/data-table'
import { DatePicker } from '@/components/date-picker'
import { Label } from '@/components/ui/label'
import { getGroups, getUsers, searchUsers } from '../api'
import {
  USER_STATUS,
  getUserStatusOptions,
  getUserRoleOptions,
  isUserDeleted,
} from '../constants'
import type { User } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useUsersColumns } from './users-columns'
import { useUsers } from './users-provider'

const route = getRouteApi('/_authenticated/users/')

function isDisabledUserRow(user: User) {
  return isUserDeleted(user) || user.status === USER_STATUS.DISABLED
}

export function UsersTable() {
  const { t } = useTranslation()
  const columns = useUsersColumns()
  const { refreshTrigger } = useUsers()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [rowSelection, setRowSelection] = useState({})
  const [sorting, setSorting] = useState<SortingState>([])
  // is_vip_customer 列默认隐藏，仅作为 toolbar 筛选锚点
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({
    is_vip_customer: false,
  })

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'role', searchKey: 'role', type: 'array' },
      { columnId: 'group', searchKey: 'group', type: 'string' },
      { columnId: 'is_vip_customer', searchKey: 'vip', type: 'array' },
    ],
  })
  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const roleFilter =
    (columnFilters.find((filter) => filter.id === 'role')?.value as
      | string[]
      | undefined) ?? []
  const groupFilter =
    (columnFilters.find((filter) => filter.id === 'group')?.value as string) ??
    ''
  const vipFilter =
    (columnFilters.find((filter) => filter.id === 'is_vip_customer')?.value as
      | string[]
      | undefined) ?? []
  // 'all' 视为不筛选（"全部"语义），仅在选中 true/false 时透传 is_vip
  const vipFilterValue =
    vipFilter[0] && vipFilter[0] !== 'all' ? vipFilter[0] : ''

  // 创建时间区间：YYYY-MM-DD 字符串放在 URL，传给后端时换算成 unix 秒
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const createdStart = (search as { createdStart?: string }).createdStart ?? ''
  const createdEnd = (search as { createdEnd?: string }).createdEnd ?? ''

  const createdAtStartTs = useMemo(() => {
    if (!createdStart) return undefined
    const d = dayjs(createdStart).startOf('day')
    return d.isValid() ? d.unix() : undefined
  }, [createdStart])
  const createdAtEndTs = useMemo(() => {
    if (!createdEnd) return undefined
    const d = dayjs(createdEnd).endOf('day')
    return d.isValid() ? d.unix() : undefined
  }, [createdEnd])

  const setCreatedStart = (date: Date | undefined) => {
    navigate({
      search: (prev) => ({
        ...prev,
        createdStart: date ? dayjs(date).format('YYYY-MM-DD') : '',
        page: 1,
      }),
      replace: true,
    })
  }
  const setCreatedEnd = (date: Date | undefined) => {
    navigate({
      search: (prev) => ({
        ...prev,
        createdEnd: date ? dayjs(date).format('YYYY-MM-DD') : '',
        page: 1,
      }),
      replace: true,
    })
  }

  // 加载分组列表，作为 toolbar group filter 的 options 来源
  const { data: groupListData } = useQuery({
    queryKey: ['user-groups'],
    queryFn: () => getGroups(),
    staleTime: 5 * 60_000,
  })
  const groupOptions = useMemo(() => {
    const list = groupListData?.success ? groupListData.data ?? [] : []
    return list.map((g) => ({ label: g, value: g }))
  }, [groupListData])

  // Fetch data with React Query
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'users',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilter,
      roleFilter,
      groupFilter,
      vipFilterValue,
      createdAtStartTs,
      createdAtEndTs,
      refreshTrigger,
    ],
    queryFn: async () => {
      const hasFilter = globalFilter?.trim()
      const hasColumnFilter =
        statusFilter.length > 0 ||
        roleFilter.length > 0 ||
        Boolean(groupFilter) ||
        Boolean(vipFilterValue) ||
        Boolean(createdAtStartTs) ||
        Boolean(createdAtEndTs)
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }

      const result =
        hasFilter || hasColumnFilter
          ? await searchUsers({
              ...params,
              keyword: globalFilter,
              status: statusFilter[0] ?? '',
              role: roleFilter[0] ?? '',
              group: groupFilter,
              is_vip: vipFilterValue,
              created_at_start: createdAtStartTs,
              created_at_end: createdAtEndTs,
            })
          : await getUsers(params)

      if (!result.success) {
        toast.error(
          result.message || `Failed to ${hasFilter ? 'search' : 'load'} users`
        )
        return { items: [], total: 0 }
      }

      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const users = data?.items || []

  const table = useReactTable({
    data: users,
    columns,
    state: {
      sorting,
      columnVisibility,
      rowSelection,
      columnFilters,
      globalFilter,
      pagination,
    },
    enableRowSelection: true,
    onRowSelectionChange: setRowSelection,
    onSortingChange: setSorting,
    onColumnVisibilityChange: setColumnVisibility,
    globalFilterFn: (row, _columnId, filterValue) => {
      const searchValue = String(filterValue).toLowerCase()
      const fields = [
        row.getValue('username'),
        row.original.display_name,
        row.original.email,
      ]
      return fields.some((field) =>
        String(field || '')
          .toLowerCase()
          .includes(searchValue)
      )
    },
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    pageCount: Math.ceil((data?.total || 0) / pagination.pageSize),
  })

  const pageCount = table.getPageCount()
  useEffect(() => {
    ensurePageInRange(pageCount)
  }, [pageCount, ensurePageInRange])

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Users Found')}
      emptyDescription={t(
        'No users available. Try adjusting your search or filters.'
      )}
      skeletonKeyPrefix='users-skeleton'
      toolbarProps={{
        searchPlaceholder: t('Filter by username, name or email...'),
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            options: getUserStatusOptions(t),
            singleSelect: true,
          },
          {
            columnId: 'role',
            title: t('Role'),
            options: getUserRoleOptions(t),
            singleSelect: true,
          },
          {
            columnId: 'is_vip_customer',
            title: t('VIP Customer'),
            options: [
              { label: t('All'), value: 'all' },
              { label: t('Yes'), value: 'true' },
              { label: t('No'), value: 'false' },
            ],
            singleSelect: true,
          },
          {
            columnId: 'group',
            title: t('Group'),
            options: groupOptions,
            singleSelect: true,
          },
        ],
        additionalSearch: (
          <div className='flex flex-wrap items-center gap-2'>
            <Label className='text-muted-foreground text-xs'>
              {t('Created At')}
            </Label>
            <DatePicker
              selected={createdStart ? dayjs(createdStart).toDate() : undefined}
              onSelect={setCreatedStart}
              placeholder={t('Start date')}
            />
            <span className='text-muted-foreground text-sm'>~</span>
            <DatePicker
              selected={createdEnd ? dayjs(createdEnd).toDate() : undefined}
              onSelect={setCreatedEnd}
              placeholder={t('End date')}
            />
          </div>
        ),
        hasAdditionalFilters: Boolean(createdStart) || Boolean(createdEnd),
        onReset: () => {
          navigate({
            search: (prev) => ({
              ...prev,
              createdStart: '',
              createdEnd: '',
              page: 1,
            }),
            replace: true,
          })
        },
      }}
      getRowClassName={(row, { isMobile }) =>
        isDisabledUserRow(row.original)
          ? isMobile
            ? DISABLED_ROW_MOBILE
            : DISABLED_ROW_DESKTOP
          : undefined
      }
      bulkActions={<DataTableBulkActions table={table} />}
    />
  )
}
