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
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { parseHeaderNavModulesFromStatus } from '@/lib/nav-modules'
import { parseCustomMenuPages } from '@/features/system-settings/maintenance/config'
import { useStatus } from '@/hooks/use-status'

export type TopNavLink = {
  title: string
  href: string
  disabled?: boolean
  requiresAuth?: boolean
  external?: boolean
}

/**
 * Generate top navigation links based on HeaderNavModules configuration from backend /api/status
 * Backend format example (stringified JSON):
 * {
 *   home: true,
 *   console: true,
 *   pricing: { enabled: true, requireAuth: false },
 *   rankings: { enabled: true, requireAuth: false },
 *   docs: true,
 *   about: true
 * }
 */
export function useTopNavLinks(): TopNavLink[] {
  const { t } = useTranslation()
  const { status } = useStatus()
  const { auth } = useAuthStore()

  // Parse HeaderNavModules
  const modules = useMemo(() => {
    return parseHeaderNavModulesFromStatus(
      status as Record<string, unknown> | null
    )
  }, [status])

  // Documentation link (may be external)
  const docsLink: string | undefined = status?.docs_link as string | undefined

  const isAuthed = !!auth?.user

  const links: TopNavLink[] = []

  // Home
  if (modules?.home !== false) {
    links.push({ title: t('Home'), href: '/' })
  }

  // Console -> /dashboard (new console path)
  if (modules?.console !== false) {
    links.push({ title: t('Console'), href: '/dashboard' })
  }

  // Pricing
  const pricing = modules?.pricing
  if (pricing && typeof pricing === 'object' && pricing.enabled) {
    const requiresAuth = pricing.requireAuth && !isAuthed
    links.push({ title: t('Model Square'), href: '/pricing', requiresAuth })
  }

  // Rankings
  const rankings = modules?.rankings
  if (rankings && typeof rankings === 'object' && rankings.enabled) {
    const requiresAuth = rankings.requireAuth && !isAuthed
    links.push({ title: t('Rankings'), href: '/rankings', requiresAuth })
  }

  // Docs (supports external links)
  if (modules?.docs !== false) {
    if (docsLink) {
      links.push({ title: t('Docs'), href: docsLink, external: true })
    } else {
      links.push({ title: t('Docs'), href: '/docs' })
    }
  }

  // About
  if (modules?.about !== false) {
    links.push({ title: t('About'), href: '/about' })
  }

  // Admin-configured custom iframe menus.
  // Visibility rules:
  //   - requireLogin='no'  → public: visible to everyone (guests + all logged-in users).
  //     For guests, sidebar-layout iframe items are still hidden because that route
  //     lives inside _authenticated and cannot render without an auth context.
  //   - requireLogin='yes' → filtered by visibleTo:
  //       - visibleTo='user'  → only regular users (logged in, non-admin)
  //       - visibleTo='admin' → admins (role >= ADMIN, includes root/super-admin)
  //       - Guests (no role) see none
  const role = auth?.user?.role
  const isAdmin = role !== undefined && role >= ROLE.ADMIN
  const customMenus = parseCustomMenuPages(
    status?.SidebarCustomMenuPages as string | undefined
  )
  for (const item of customMenus.items) {
    if (!item.enabled) continue
    if (item.requireLogin === 'no') {
      // Public item: hide sidebar-layout iframe items from guests (route requires auth).
      if (
        !isAuthed &&
        item.openMode === 'iframe' &&
        item.layoutMode === 'sidebar'
      ) {
        continue
      }
    } else {
      // requireLogin='yes' → strict role match, guests see none.
      const allowed =
        item.visibleTo === 'admin' ? isAdmin : isAuthed && !isAdmin
      if (!allowed) continue
    }
    if (item.openMode === 'newWindow') {
      // Direct external open — bypass the /custom/$id iframe wrapper.
      links.push({
        title: item.name,
        href: item.url,
        external: true,
      })
    } else if (item.layoutMode === 'fullwidth') {
      // Iframe + fullwidth: route to top-level /custom-full/$id (no sidebar layout).
      links.push({
        title: item.name,
        href: `/custom-full/${item.id}`,
      })
    } else {
      // Iframe + sidebar (default): route to /custom/$id inside _authenticated layout.
      links.push({
        title: item.name,
        href: `/custom/${item.id}`,
      })
    }
  }

  return links
}
