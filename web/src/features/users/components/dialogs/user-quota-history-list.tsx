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
import { AlertCircle, History } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatQuota, formatTimestamp } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { UserQuotaHistoryItem } from '../../types'

const LOG_TYPE_TOPUP = 1
const LOG_TYPE_CONSUME = 2
const LOG_TYPE_MANAGE = 3
const LOG_TYPE_REFUND = 6

interface UserQuotaHistoryListProps {
  currentQuota: number
  items: UserQuotaHistoryItem[]
  isLoading: boolean
  isError: boolean
  isFetching: boolean
  onRetry: () => void
}

function QuotaHistoryTypeBadge(props: { item: UserQuotaHistoryItem }) {
  const { t } = useTranslation()
  let label = t('Change')
  let variant: 'success' | 'danger' | 'neutral' = 'neutral'

  switch (props.item.type) {
    case LOG_TYPE_TOPUP:
      label = t('Top-up')
      variant = 'success'
      break
    case LOG_TYPE_CONSUME:
      label = t('Consume')
      variant = 'danger'
      break
    case LOG_TYPE_MANAGE:
      label = props.item.quota_type || t('Quota adjustment')
      if ((props.item.delta_quota ?? 0) > 0) variant = 'success'
      if ((props.item.delta_quota ?? 0) < 0) variant = 'danger'
      break
    case LOG_TYPE_REFUND:
      label = t('Refund')
      variant = 'success'
      break
  }

  return <StatusBadge label={label} variant={variant} copyable={false} />
}

function UserQuotaHistoryContent(props: UserQuotaHistoryListProps) {
  const { t } = useTranslation()

  if (props.isLoading) {
    return (
      <div className='flex flex-col gap-2 py-2' aria-label={t('Loading...')}>
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className='h-12 w-full' />
        ))}
      </div>
    )
  }

  if (props.isError) {
    return (
      <Empty className='min-h-64'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <AlertCircle />
          </EmptyMedia>
          <EmptyTitle>{t('Failed to load')}</EmptyTitle>
          <EmptyDescription>{t('Quota change history')}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button variant='outline' size='sm' onClick={props.onRetry}>
            {t('Retry')}
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  if (props.items.length === 0) {
    return (
      <Empty className='min-h-64'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <History />
          </EmptyMedia>
          <EmptyTitle>{t('No quota change records')}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <StaticDataTable
      data={props.items}
      getRowKey={(item) => item.id}
      tableClassName='min-w-[980px]'
      tableContainerClassName='h-full overflow-auto'
      headerClassName='bg-background sticky top-0 z-10'
      className={cn('h-full', props.isFetching && 'opacity-70')}
      columns={[
        {
          id: 'time',
          header: t('Time'),
          className: 'w-[168px]',
          cell: (item) => formatTimestamp(item.created_at),
        },
        {
          id: 'type',
          header: t('Type'),
          className: 'w-[80px]',
          cell: (item) => <QuotaHistoryTypeBadge item={item} />,
        },
        {
          id: 'change',
          header: t('Change'),
          className: 'w-[120px] text-center',
          cellClassName: 'text-center',
          cell: (item) => {
            if (item.delta_quota == null) return '-'
            const sign = item.delta_quota > 0 ? '+' : '-'
            return (
              <span
                className={cn(
                  'font-medium tabular-nums',
                  item.delta_quota > 0 && 'text-success',
                  item.delta_quota < 0 && 'text-destructive'
                )}
              >
                {sign}
                {formatQuota(Math.abs(item.delta_quota))}
              </span>
            )
          },
        },
        {
          id: 'before',
          header: t('Before change'),
          className: 'w-[120px] text-center',
          cellClassName: 'tabular-nums text-center',
          cell: (item) =>
            item.before_quota == null ? '-' : formatQuota(item.before_quota),
        },
        {
          id: 'after',
          header: t('After change'),
          className: 'w-[120px]',
          cellClassName: ' tabular-nums',
          cell: (item) =>
            item.after_quota == null ? '-' : formatQuota(item.after_quota),
        },
        {
          id: 'details',
          header: t('Details'),
          cell: (item) => (
            <Tooltip>
              <TooltipTrigger
                render={
                  <div
                    data-slot='quota-history-details-trigger'
                    tabIndex={0}
                    className='focus-visible:ring-ring w-full max-w-[480px] cursor-help rounded-sm focus-visible:ring-2 focus-visible:outline-none'
                  />
                }
              >
                <div className='min-w-0'>
                  <p className='truncate text-sm'>{item.content || '-'}</p>
                  {item.model_name ? (
                    <p className='text-muted-foreground truncate text-xs'>
                      {t('Model')}: {item.model_name}
                    </p>
                  ) : null}
                  {item.request_id ? (
                    <p className='text-muted-foreground truncate font-mono text-xs'>
                      {item.request_id}
                    </p>
                  ) : null}
                </div>
              </TooltipTrigger>
              <TooltipContent
                side='top'
                className='max-w-sm items-start text-left whitespace-normal'
              >
                <div className='flex min-w-0 flex-col items-start gap-1'>
                  <p className='break-words whitespace-pre-wrap'>
                    {item.content || '-'}
                  </p>
                  {item.model_name ? (
                    <p className='break-all'>
                      {t('Model')}: {item.model_name}
                    </p>
                  ) : null}
                  {item.request_id ? (
                    <p className='font-mono break-all'>{item.request_id}</p>
                  ) : null}
                </div>
              </TooltipContent>
            </Tooltip>
          ),
        },
      ]}
    />
  )
}

export function UserQuotaHistoryList(props: UserQuotaHistoryListProps) {
  const { t } = useTranslation()

  return (
    <div className='flex h-full min-h-0 flex-col gap-3'>
      <div
        data-slot='current-quota-summary'
        className='flex shrink-0 items-baseline justify-between gap-3 border-b px-1 pb-3'
      >
        <span className='text-muted-foreground text-sm'>
          {t('Current quota')}
        </span>
        <span className='min-w-0 text-right text-base font-semibold break-all tabular-nums'>
          {formatQuota(props.currentQuota)}
        </span>
      </div>
      <div className='min-h-0 flex-1'>
        <UserQuotaHistoryContent {...props} />
      </div>
    </div>
  )
}
