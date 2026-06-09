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
import { useMemo } from 'react'
import { Link, useParams } from '@tanstack/react-router'
import { Ban, FileQuestion } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { parseCustomMenuPages } from '@/features/system-settings/maintenance/config'

function EmptyState({
  icon,
  title,
  description,
  backLabel,
}: {
  icon: React.ReactNode
  title: string
  description: string
  backLabel: string
}) {
  return (
    <div className='flex min-h-[60vh] items-center justify-center p-8'>
      <div className='max-w-md space-y-4 text-center'>
        <div className='flex justify-center'>{icon}</div>
        <h2 className='text-xl font-semibold'>{title}</h2>
        <p className='text-muted-foreground text-sm'>{description}</p>
        <Link to='/'>
          <Button variant='outline'>{backLabel}</Button>
        </Link>
      </div>
    </div>
  )
}

export function CustomMenuPage() {
  const { t } = useTranslation()
  const { id } = useParams({ from: '/_authenticated/custom/$id' })
  const { status } = useStatus()
  const role = useAuthStore((s) => s.auth.user?.role)

  const { item, forbidden } = useMemo(() => {
    const cfg = parseCustomMenuPages(
      status?.SidebarCustomMenuPages as string | undefined
    )
    // Strict guard: this route only renders sidebar-layout iframe items.
    // Fullwidth items live at /custom-full/$id; treat layout mismatch as not
    // found rather than silently switching layouts.
    const found = cfg.items.find(
      (it) =>
        it.id === id &&
        it.enabled &&
        it.openMode === 'iframe' &&
        it.layoutMode === 'sidebar'
    )
    if (!found) return { item: null, forbidden: false }

    const isAuthed = role !== undefined && role >= ROLE.USER
    const isAdmin = role !== undefined && role >= ROLE.ADMIN
    const allowed =
      found.visibleTo === 'admin' ? isAdmin : isAuthed && !isAdmin
    if (!allowed) return { item: found, forbidden: true }
    return { item: found, forbidden: false }
  }, [status?.SidebarCustomMenuPages, id, role])

  if (!item) {
    return (
      <EmptyState
        icon={<FileQuestion className='text-muted-foreground h-16 w-16' />}
        title={t('Custom page not found')}
        description={t(
          'The page may have been deleted or disabled by an administrator.'
        )}
        backLabel={t('Back to home')}
      />
    )
  }

  if (forbidden) {
    return (
      <EmptyState
        icon={<Ban className='text-muted-foreground h-16 w-16' />}
        title={t('Access denied')}
        description={t(
          'You do not have permission to view this custom page.'
        )}
        backLabel={t('Back to home')}
      />
    )
  }

  return (
    <iframe
      src={item.url}
      title={item.name}
      className='h-[calc(100vh-3.5rem)] w-full border-0'
      referrerPolicy='no-referrer-when-downgrade'
    />
  )
}
