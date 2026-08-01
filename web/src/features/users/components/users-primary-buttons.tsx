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
import { useNavigate } from '@tanstack/react-router'
import { BarChart3, MessageCircle, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { useUsers } from './users-provider'

export function UsersPrimaryButtons() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { setOpen, setCurrentRow } = useUsers()

  const handleCreate = () => {
    setCurrentRow(null)
    setOpen('create')
  }

  const handleTgGroupSettings = () => {
    setOpen('tg-settings')
  }

  const handleViewStats = () => {
    navigate({ to: '/vip-stats' })
  }

  return (
    <div className='flex gap-2'>
      <Button variant='outline' size='sm' onClick={handleViewStats}>
        <BarChart3 className='h-4 w-4' />
        {t('VIP Customer Statistics')}
      </Button>
      <Button variant='outline' size='sm' onClick={handleTgGroupSettings}>
        <MessageCircle className='h-4 w-4' />
        {t('TG Notification Group Settings')}
      </Button>
      <Button size='sm' onClick={handleCreate}>
        <Plus className='h-4 w-4' />
        {t('Add User')}
      </Button>
    </div>
  )
}
