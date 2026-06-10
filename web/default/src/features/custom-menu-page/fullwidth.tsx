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
import { Link, useLocation, useParams } from '@tanstack/react-router'
import { Ban, FileQuestion, LogIn } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { PublicLayout } from '@/components/layout'
import { useStatus } from '@/hooks/use-status'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { parseCustomMenuPages } from '@/features/system-settings/maintenance/config'

function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon: React.ReactNode
  title: string
  description: string
  action: React.ReactNode
}) {
  return (
    <div className='flex min-h-[60vh] items-center justify-center p-8'>
      <div className='max-w-md space-y-4 text-center'>
        <div className='flex justify-center'>{icon}</div>
        <h2 className='text-xl font-semibold'>{title}</h2>
        <p className='text-muted-foreground text-sm'>{description}</p>
        {action}
      </div>
    </div>
  )
}

/**
 * Full-width variant of the custom-menu iframe page.
 *
 * Mounted at /custom-full/$id (outside the _authenticated layout) so it can
 * render with just the public top header — no sidebar — matching the marketing
 * pages' look. Auth gating still happens at the route's beforeLoad.
 */
type GuardStatus = 'ok' | 'notfound' | 'loginRequired' | 'forbidden'

export function CustomMenuPageFullwidth() {
  const { t } = useTranslation()
  const { id } = useParams({ from: '/custom-full/$id' })
  const location = useLocation()
  const { status } = useStatus()
  const role = useAuthStore((s) => s.auth.user?.role)

  const { item, guard } = useMemo<{
    item: ReturnType<typeof parseCustomMenuPages>['items'][number] | null
    guard: GuardStatus
  }>(() => {
    const cfg = parseCustomMenuPages(
      status?.SidebarCustomMenuPages as string | undefined
    )
    // Strict guard: only render items that are enabled, iframe-mode AND set to
    // fullwidth layout. Sidebar-mode items live at /custom/$id; navigating to
    // /custom-full/$id for a sidebar-only item is treated as "not found" rather
    // than silently switching layouts.
    const found = cfg.items.find(
      (it) =>
        it.id === id &&
        it.enabled &&
        it.openMode === 'iframe' &&
        it.layoutMode === 'fullwidth'
    )
    if (!found) return { item: null, guard: 'notfound' as const }

    // requireLogin='no' → public: anyone can access (visibleTo ignored).
    if (found.requireLogin === 'no') {
      return { item: found, guard: 'ok' as const }
    }

    // requireLogin='yes' → require login + role match.
    const isAuthed = role !== undefined && role >= ROLE.USER
    if (!isAuthed) return { item: found, guard: 'loginRequired' as const }
    const isAdmin = role !== undefined && role >= ROLE.ADMIN
    const allowed = found.visibleTo === 'admin' ? isAdmin : !isAdmin
    if (!allowed) return { item: found, guard: 'forbidden' as const }
    return { item: found, guard: 'ok' as const }
  }, [status?.SidebarCustomMenuPages, id, role])

  if (guard === 'notfound') {
    return (
      <PublicLayout showMainContainer={false}>
        <EmptyState
          icon={<FileQuestion className='text-muted-foreground h-16 w-16' />}
          title={t('Custom page not found')}
          description={t(
            'The page may have been deleted or disabled by an administrator.'
          )}
          action={
            <Link to='/'>
              <Button variant='outline'>{t('Back to home')}</Button>
            </Link>
          }
        />
      </PublicLayout>
    )
  }

  if (guard === 'loginRequired') {
    return (
      <PublicLayout showMainContainer={false}>
        <EmptyState
          icon={<LogIn className='text-muted-foreground h-16 w-16' />}
          title={t('Login required')}
          description={t('Please sign in to view this custom page.')}
          action={
            <Link to='/sign-in' search={{ redirect: location.href }}>
              <Button>{t('Sign in')}</Button>
            </Link>
          }
        />
      </PublicLayout>
    )
  }

  if (guard === 'forbidden') {
    return (
      <PublicLayout showMainContainer={false}>
        <EmptyState
          icon={<Ban className='text-muted-foreground h-16 w-16' />}
          title={t('Access denied')}
          description={t(
            'You do not have permission to view this custom page.'
          )}
          action={
            <Link to='/'>
              <Button variant='outline'>{t('Back to home')}</Button>
            </Link>
          }
        />
      </PublicLayout>
    )
  }

  return (
    <PublicLayout showMainContainer={false}>
      <iframe
        src={item!.url}
        title={item!.name}
        className='h-[calc(100svh-4rem)] w-full border-0'
        referrerPolicy='no-referrer-when-downgrade'
      />
    </PublicLayout>
  )
}
