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
import { Pencil, Plus } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { ProviderBadge } from '@/components/provider-badge'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

import { getVendors } from '../../api'
import { getOfficialPriceBasisConfig } from '../../constants'
import { vendorsQueryKeys } from '../../lib'
import type { Vendor } from '../../types'
import { useModels } from '../models-provider'

type VendorManagementDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const VENDOR_SKELETON_ROWS = ['vendor-skeleton-1', 'vendor-skeleton-2']

export function VendorManagementDialog(props: VendorManagementDialogProps) {
  const { t } = useTranslation()
  const { setCurrentVendor, setOpen } = useModels()
  const basisConfig = getOfficialPriceBasisConfig(t)

  const vendorsQuery = useQuery({
    queryKey: vendorsQueryKeys.list({ page_size: 1000 }),
    queryFn: () => getVendors({ page_size: 1000 }),
    enabled: props.open,
  })

  const vendors = vendorsQuery.data?.data?.items ?? []
  let vendorListContent: ReactNode
  if (vendorsQuery.isLoading) {
    vendorListContent = VENDOR_SKELETON_ROWS.map((key) => (
      <div
        key={key}
        className='flex items-center justify-between gap-3 rounded-lg border px-3 py-3'
      >
        <Skeleton className='h-5 w-40' />
        <Skeleton className='h-8 w-16' />
      </div>
    ))
  } else if (vendors.length === 0) {
    vendorListContent = (
      <div className='text-muted-foreground rounded-lg border border-dashed px-3 py-8 text-center text-sm'>
        {t('No data')}
      </div>
    )
  } else {
    vendorListContent = vendors.map((vendor) => {
      const basis =
        vendor.official_price_basis === 'one_to_one'
          ? 'one_to_one'
          : 'consume_usd_exchange_rate'
      const config = basisConfig[basis]

      return (
        <div
          key={vendor.id}
          className='flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5'
        >
          <div className='flex min-w-0 flex-1 items-center gap-2'>
            <ProviderBadge iconKey={vendor.icon} label={vendor.name} />
            <StatusBadge
              variant={basis === 'one_to_one' ? 'info' : 'neutral'}
              size='sm'
              copyable={false}
              className='max-w-none shrink-0'
            >
              {config.label}
            </StatusBadge>
          </div>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => handleEdit(vendor)}
          >
            <Pencil className='h-4 w-4' />
            {t('Edit')}
          </Button>
        </div>
      )
    })
  }

  const handleCreate = () => {
    setCurrentVendor(null)
    setOpen('create-vendor')
  }

  const handleEdit = (vendor: Vendor) => {
    setCurrentVendor(vendor)
    setOpen('update-vendor')
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Manage Vendors')}
      contentHeight='min(520px,calc(100vh-14rem))'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Close')}
          </Button>
          <Button type='button' onClick={handleCreate}>
            <Plus className='h-4 w-4' />
            {t('Create Vendor')}
          </Button>
        </>
      }
    >
      <div className='space-y-2'>{vendorListContent}</div>
    </Dialog>
  )
}
