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
import type { MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { User } from '../types'
import { UserQuotaCell } from './user-quota-cell'
import { useUsers } from './users-provider'

type UserQuotaHistoryCellProps = {
  user: User
}

export function UserQuotaHistoryCell(props: UserQuotaHistoryCellProps) {
  const { t } = useTranslation()
  const { setCurrentRow, setOpen } = useUsers()
  const total =
    props.user.total_quota ?? props.user.quota + props.user.used_quota

  const openQuotaHistory = (event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation()
    setCurrentRow(props.user)
    setOpen('quota-history')
  }

  return (
    <button
      type='button'
      aria-label={t('Quota change history')}
      title={t('Quota change history')}
      onClick={openQuotaHistory}
      className='focus-visible:ring-ring w-[150px] rounded-sm text-left focus-visible:ring-2 focus-visible:outline-none'
    >
      <UserQuotaCell total={total} remaining={props.user.quota} />
    </button>
  )
}
