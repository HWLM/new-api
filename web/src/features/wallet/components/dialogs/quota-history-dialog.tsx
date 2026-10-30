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
import { UserQuotaHistoryList } from '@/features/users/components/dialogs/user-quota-history-list'

import { getSelfQuotaHistory } from '../../api'

const PAGE_SIZE = 10

// Composite filter values: "<type>" or "3:<quota_type>". The self endpoint
// only accepts top-up (1) and quota adjustment (3); keep in sync with
// controller.GetSelfQuotaHistory.
const FILTER_TOPUP = '1'
const FILTER_MANAGE = '3'
const FILTER_MANAGE_RECHARGE = '3:充值'
const FILTER_MANAGE_GIFT = '3:赠送'
const FILTER_MANAGE_CREDIT = '3:授信'

interface QuotaHistoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentQuota: number
}

export function QuotaHistoryDialog(props: QuotaHistoryDialogProps) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [typeFilter, setTypeFilter] = useState<string>(FILTER_MANAGE)
  const [typePart, quotaType] = typeFilter.split(':')
  const query = useQuery({
    queryKey: ['wallet', 'quota-history', page, typeFilter],
    queryFn: () =>
      getSelfQuotaHistory(page, PAGE_SIZE, Number(typePart), quotaType),
    enabled: props.open,
    placeholderData: (previousData) => previousData,
  })

  const items = query.data?.data?.items ?? []
  const total = query.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentQuota = query.data?.data?.current_quota ?? props.currentQuota

  const typeOptions = [
    { value: FILTER_TOPUP, label: t('Top-up') },
    { value: FILTER_MANAGE, label: t('Quota adjustment') },
    { value: FILTER_MANAGE_RECHARGE, label: `· ${t('Recharge')}` },
    { value: FILTER_MANAGE_GIFT, label: `· ${t('Gift')}` },
    { value: FILTER_MANAGE_CREDIT, label: `· ${t('Credit')}` },
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
          setTypeFilter(FILTER_MANAGE)
          setPage(1)
        }
        props.onOpenChange(open)
      }}
      title={t('Quota change history')}
      contentClassName='sm:max-w-3xl'
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
              <SelectValue />
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
            showDetails={false}
          />
        </div>
      </div>
    </Dialog>
  )
}
