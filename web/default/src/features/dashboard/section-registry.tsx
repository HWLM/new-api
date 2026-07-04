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
import type { TFunction } from 'i18next'

import { createSectionRegistry } from '@/features/system-settings/utils/section-registry'

/**
 * 可见性：
 * - 'all'        所有登录用户均可见（默认）
 * - 'admin'      仅管理员可见
 * - 'commonUser' 仅非管理员可见
 */
type DashboardSectionVisibility = 'all' | 'admin' | 'commonUser'

/**
 * Dashboard page section definitions
 *
 * `visibility` 是当前 section 的可见性单一真相来源（single source of truth），
 * 任何"哪些 section 对哪类用户可见"的判断都应基于该字段或下方派生的工具函数，
 * 不要在调用方再次硬编码 id 列表。
 */
const DASHBOARD_SECTIONS = [
  {
    id: 'overview',
    titleKey: 'Overview',
    visibility: 'all',
    build: () => null,
  },
  {
    id: 'models',
    titleKey: 'Model Call Analytics',
    visibility: 'all',
    build: () => null,
  },
  {
    id: 'flow',
    titleKey: 'Flow',
    visibility: 'all',
    build: () => null,
  },
  {
    id: 'users',
    titleKey: 'User Analytics',
    visibility: 'admin',
    build: () => null,
  },
  {
    id: 'tokens',
    titleKey: 'Token Statistics',
    visibility: 'all',
    build: () => null,
  },
  {
    id: 'request-analytics',
    titleKey: 'Request Response Analytics',
    visibility: 'admin',
    build: () => null,
  },
  {
    id: 'inviter',
    titleKey: 'Inviter Statistics',
    visibility: 'commonUser',
    build: () => null,
  },
  {
    id: 'new-user-stats',
    titleKey: 'New User Statistics',
    visibility: 'admin',
    build: () => null,
  },
] as const satisfies ReadonlyArray<{
  id: string
  titleKey: string
  visibility: DashboardSectionVisibility
  build: () => null
}>

export type DashboardSectionId = (typeof DASHBOARD_SECTIONS)[number]['id']

const dashboardRegistry = createSectionRegistry<
  DashboardSectionId,
  Record<string, never>,
  []
>({
  sections: DASHBOARD_SECTIONS,
  defaultSection: 'overview',
  basePath: '/dashboard',
  urlStyle: 'path',
})

export const DASHBOARD_SECTION_IDS = dashboardRegistry.sectionIds
export const DASHBOARD_DEFAULT_SECTION = dashboardRegistry.defaultSection

function isSectionVisibleTo(
  visibility: DashboardSectionVisibility,
  isAdmin: boolean
): boolean {
  switch (visibility) {
    case 'admin':
      return isAdmin
    case 'commonUser':
      return !isAdmin
    case 'all':
    default:
      return true
  }
}

/**
 * 根据用户角色返回当前可见的 section id 列表，按 sections 数组顺序保留。
 * 这是"哪些 section 对当前用户可见"的唯一入口，所有调用方都应使用它。
 */
export function getVisibleDashboardSectionIds(options: {
  isAdmin: boolean
}): DashboardSectionId[] {
  return DASHBOARD_SECTIONS.filter((section) =>
    isSectionVisibleTo(section.visibility, options.isAdmin)
  ).map((section) => section.id)
}

export function getDashboardSectionNavItems(
  t: TFunction,
  options?: { isAdmin?: boolean }
) {
  const all = dashboardRegistry.getSectionNavItems(t)
  const isAdmin = !!options?.isAdmin
  return all.filter((_, idx) =>
    isSectionVisibleTo(DASHBOARD_SECTIONS[idx].visibility, isAdmin)
  )
}
