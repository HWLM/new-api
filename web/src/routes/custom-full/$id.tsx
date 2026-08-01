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
import { createFileRoute } from '@tanstack/react-router'
import { CustomMenuPageFullwidth } from '@/features/custom-menu-page/fullwidth'

export const Route = createFileRoute('/custom-full/$id')({
  // Auth gating is intentionally NOT done here — public items (requireLogin='no')
  // must be reachable by guests. The component inspects the target item's
  // requireLogin/visibleTo and renders an EmptyState with a sign-in link when
  // login is actually required.
  component: CustomMenuPageFullwidth,
})
