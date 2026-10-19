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
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { getUserQuotaHistory } from '../../api'
import { UserQuotaHistoryList } from './user-quota-history-list'

const PAGE_SIZE = 10

// Composite filter values. Format:
//   "all"         → no filter
//   "<type>"      → ?type=<type>
//   "3:<quota>"   → ?type=3&quota_type=<quota> (Recharge/Gift/Credit sub-filter)
// Keep type ids in sync with backend model.LogType* and quota_type strings in
// sync with model.QuotaType* / IsUserQuotaHistoryFilterQuotaType.
const FILTER_ALL = 'all'
const FILTER_TOPUP = '1'
const FILTER_CONSUME = '2'
const FILTER_MANAGE = '3'
const FILTER_MANAGE_RECHARGE = '3:充值'
const FILTER_MANAGE_GIFT = '3:赠送'
const FILTER_MANAGE_CREDIT = '3:授信'
const FILTER_REFUND = '6'

function parseFilterValue(value: string): {
  type?: number
  quotaType?: string
} {
  if (value === FILTER_ALL) return {}
  const [typePart, quotaPart] = value.split(':')
  const type = Number(typePart)
  return {
    type: Number.isFinite(type) && type > 0 ? type : undefined,
    quotaType: quotaPart || undefined,
  }
}

interface UserQuotaHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username: string; quota: number }
}

export function UserQuotaHistoryDialog(props: UserQuotaHistoryDialogProps) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [typeFilter, setTypeFilter] = useState<string>(FILTER_ALL)
  const { type, quotaType } = parseFilterValue(typeFilter)
  const query = useQuery({
    queryKey: ['users', 'quota-history', props.user.id, page, typeFilter],
    queryFn: () =>
      getUserQuotaHistory(props.user.id, page, PAGE_SIZE, type, quotaType),
    enabled: props.open,
    placeholderData: (previousData) => previousData,
  })

  const items = query.data?.data?.items ?? []
  const total = query.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentQuota = query.data?.data?.current_quota ?? props.user.quota

  const typeOptions = [
    { value: FILTER_ALL, label: t('All types') },
    { value: FILTER_TOPUP, label: t('Top-up') },
    { value: FILTER_CONSUME, label: t('Consume') },
    { value: FILTER_MANAGE, label: t('Quota adjustment') },
    { value: FILTER_MANAGE_RECHARGE, label: `· ${t('Recharge')}` },
    { value: FILTER_MANAGE_GIFT, label: `· ${t('Gift')}` },
    { value: FILTER_MANAGE_CREDIT, label: `· ${t('Credit')}` },
    { value: FILTER_REFUND, label: t('Refund') },
  ]

  const footer = (
    <div className='flex w-full items-center justify-between gap-3'>
      <span className='text-muted-foreground text-xs tabular-nums'>
        {t('Page {{current}} of {{total}}', {
          current: page,
          total: totalPages,
        })}
      </span>
      <div className='flex items-center gap-1'>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Previous page')}
          title={t('Previous page')}
          disabled={page <= 1 || query.isFetching}
          onClick={() => setPage((current) => Math.max(1, current - 1))}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant='outline'
          size='icon-sm'
          aria-label={t('Next page')}
          title={t('Next page')}
          disabled={page >= totalPages || query.isFetching}
          onClick={() =>
            setPage((current) => Math.min(totalPages, current + 1))
          }
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  )

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) {
          setTypeFilter(FILTER_ALL)
          setPage(1)
        }
        props.onOpenChange(open)
      }}
      title={t('Quota change history')}
      description={`${props.user.username} (ID: ${props.user.id})`}
      contentClassName='sm:max-w-6xl'
      contentHeight='min(60vh, 34rem)'
      bodyClassName='h-full min-h-64'
      footer={footer}
    >
      <div className='flex h-full min-h-0 flex-col gap-3'>
        <div className='flex shrink-0 items-center gap-2'>
          <Label className='text-muted-foreground text-xs'>{t('Type')}</Label>
          <Select
            items={typeOptions}
            value={typeFilter}
            onValueChange={(value) => {
              if (value !== null) {
                setTypeFilter(value)
                setPage(1)
              }
            }}
          >
            <SelectTrigger size='sm' className='w-40'>
              <SelectValue placeholder={t('All types')} />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {typeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
        <div className='min-h-0 flex-1'>
          <UserQuotaHistoryList
            currentQuota={currentQuota}
            items={items}
            isLoading={query.isLoading}
            isError={query.isError}
            isFetching={query.isFetching}
            onRetry={() => query.refetch()}
          />
        </div>
      </div>
    </Dialog>
  )
}
