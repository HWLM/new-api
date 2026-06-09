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
import { type Table } from '@tanstack/react-table'
import { Star, StarOff } from 'lucide-react'
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
import { batchMarkVipCustomer } from '../api'
import { type User } from '../types'
import { useUsers } from './users-provider'

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedCount = selectedRows.length

  const [pendingAction, setPendingAction] = useState<'mark' | 'unmark' | null>(
    null
  )
  const [isSubmitting, setIsSubmitting] = useState(false)

  const handleConfirm = async () => {
    if (!pendingAction) return
    const isVip = pendingAction === 'mark'
    const ids = selectedRows.map((row) => row.original.id)
    setIsSubmitting(true)
    try {
      const result = await batchMarkVipCustomer(ids, isVip)
      if (result.success) {
        toast.success(
          isVip
            ? t('Marked {{count}} user(s) as VIP', { count: ids.length })
            : t('Removed VIP mark from {{count}} user(s)', {
                count: ids.length,
              })
        )
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
      </BulkActionsToolbar>

      <ConfirmDialog
        open={pendingAction !== null}
        onOpenChange={(isOpen) => !isOpen && setPendingAction(null)}
        handleConfirm={handleConfirm}
        isLoading={isSubmitting}
        className='max-w-md'
        title={
          pendingAction === 'mark'
            ? t('Mark as VIP Customer')
            : t('Remove VIP Customer')
        }
        desc={
          pendingAction === 'mark'
            ? t('Mark VIP Confirm', { count: selectedCount })
            : t('Unmark VIP Confirm', { count: selectedCount })
        }
        confirmText={
          pendingAction === 'mark'
            ? t('Mark as VIP Customer')
            : t('Remove VIP Customer')
        }
      />
    </>
  )
}
