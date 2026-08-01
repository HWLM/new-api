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
import { useState } from 'react'
import type { Table } from '@tanstack/react-table'
import { Ban, CreditCard, Star, StarOff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { batchMarkVipCustomer, batchSetAllowOnlineTopup } from '../api'
import type { User } from '../types'
import { useUsers } from './users-provider'

type Translate = ReturnType<typeof useTranslation>['t']

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedCount = selectedRows.length

  const [pendingAction, setPendingAction] = useState<
    'mark' | 'unmark' | 'enableTopup' | 'disableTopup' | null
  >(null)
  const [isSubmitting, setIsSubmitting] = useState(false)

  const handleConfirm = async () => {
    if (!pendingAction) return
    const ids = selectedRows.map((row) => row.original.id)
    setIsSubmitting(true)
    try {
      const isVipAction = pendingAction === 'mark' || pendingAction === 'unmark'
      const isEnableTopup = pendingAction === 'enableTopup'
      const result = isVipAction
        ? await batchMarkVipCustomer(ids, pendingAction === 'mark')
        : await batchSetAllowOnlineTopup(ids, isEnableTopup)
      if (result.success) {
        toast.success(getSuccessMessage(pendingAction, ids.length, t))
        table.resetRowSelection()
        triggerRefresh()
        setPendingAction(null)
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <>
      <BulkActionsToolbar table={table} entityName='user'>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setPendingAction('mark')}
                className='size-8'
                aria-label={t('Mark as VIP Customer')}
                title={t('Mark as VIP Customer')}
              />
            }
          >
            <Star />
            <span className='sr-only'>{t('Mark as VIP Customer')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Mark as VIP Customer')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setPendingAction('unmark')}
                className='size-8'
                aria-label={t('Remove VIP Customer')}
                title={t('Remove VIP Customer')}
              />
            }
          >
            <StarOff />
            <span className='sr-only'>{t('Remove VIP Customer')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Remove VIP Customer')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setPendingAction('enableTopup')}
                className='size-8'
                aria-label={t('Enable online top-up')}
                title={t('Enable online top-up')}
              />
            }
          >
            <CreditCard />
            <span className='sr-only'>{t('Enable online top-up')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Enable online top-up')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setPendingAction('disableTopup')}
                className='size-8'
                aria-label={t('Disable online top-up')}
                title={t('Disable online top-up')}
              />
            }
          >
            <Ban />
            <span className='sr-only'>{t('Disable online top-up')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Disable online top-up')}</p>
          </TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <ConfirmDialog
        open={pendingAction !== null}
        onOpenChange={(isOpen) => !isOpen && setPendingAction(null)}
        handleConfirm={handleConfirm}
        isLoading={isSubmitting}
        className='max-w-md'
        title={getConfirmTitle(pendingAction, t)}
        desc={getConfirmDescription(pendingAction, selectedCount, t)}
        confirmText={getConfirmTitle(pendingAction, t)}
      />
    </>
  )
}

function getSuccessMessage(
  action: 'mark' | 'unmark' | 'enableTopup' | 'disableTopup',
  count: number,
  t: Translate
) {
  if (action === 'mark') {
    return t('Marked {{count}} user(s) as VIP', { count })
  }
  if (action === 'unmark') {
    return t('Removed VIP mark from {{count}} user(s)', { count })
  }
  if (action === 'enableTopup') {
    return t('Enabled online top-up for {{count}} user(s)', { count })
  }
  return t('Disabled online top-up for {{count}} user(s)', { count })
}

function getConfirmTitle(
  action: 'mark' | 'unmark' | 'enableTopup' | 'disableTopup' | null,
  t: Translate
) {
  if (action === 'mark') {
    return t('Mark as VIP Customer')
  }
  if (action === 'unmark') {
    return t('Remove VIP Customer')
  }
  if (action === 'enableTopup') {
    return t('Enable online top-up')
  }
  return t('Disable online top-up')
}

function getConfirmDescription(
  action: 'mark' | 'unmark' | 'enableTopup' | 'disableTopup' | null,
  count: number,
  t: Translate
) {
  if (action === 'mark') {
    return t('Mark VIP Confirm', { count })
  }
  if (action === 'unmark') {
    return t('Unmark VIP Confirm', { count })
  }
  if (action === 'enableTopup') {
    return t('Enable online top-up for {{count}} selected user(s)?', { count })
  }
  return t('Disable online top-up for {{count}} selected user(s)?', { count })
}
