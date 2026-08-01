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
import { useIsAdmin } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth-store'

import type { UsageLogAccessScope } from '../types'

export function useUsageLogAccess(): {
  scope: UsageLogAccessScope
  userId: number
  isAdmin: boolean
  canViewUsername: boolean
} {
  const isAdmin = useIsAdmin()
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)
  const businessChannel = useAuthStore(
    (state) => state.auth.user?.business_channel
  )
  const isBusinessAccount = Boolean(businessChannel?.trim())
  let scope: UsageLogAccessScope = 'user'
  if (isAdmin) {
    scope = 'admin'
  } else if (isBusinessAccount) {
    scope = 'business'
  }

  return {
    scope,
    userId,
    isAdmin,
    canViewUsername: scope !== 'user',
  }
}
