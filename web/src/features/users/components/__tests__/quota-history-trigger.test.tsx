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
import assert from 'node:assert/strict'
import { after, test } from 'node:test'

import { Window } from 'happy-dom'

import type { User } from '../../types'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { UserQuotaHistoryCell } = await import('../user-quota-history-cell')
const { UsersProvider, useUsers } = await import('../users-provider')
const { formatQuota } = await import('@/lib/format')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'No Quota': 'No Quota',
        'Percentage:': 'Percentage:',
        'Quota change history': 'Quota change history',
        'Remaining:': 'Remaining:',
        'Total:': 'Total:',
        'Used:': 'Used:',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const user: User = {
  id: 88,
  username: 'quota-user',
  display_name: 'Quota User',
  quota: 900000,
  used_quota: 100000,
  total_quota: 980000,
  request_count: 3,
  group: 'default',
  status: 1,
  role: 1,
}

function DialogStateProbe() {
  const { currentRow, open } = useUsers()

  return (
    <output
      data-slot='dialog-state'
      data-open={open ?? ''}
      data-user-id={currentRow?.id ?? ''}
    />
  )
}

after(() => {
  domWindow.close()
})

test('clicking quota content selects the user and opens quota history', async () => {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <UsersProvider>
          <UserQuotaHistoryCell user={user} />
          <DialogStateProbe />
        </UsersProvider>
      </I18nextProvider>
    )
  })

  const trigger = container.querySelector<HTMLButtonElement>(
    'button[aria-label="Quota change history"]'
  )
  const dialogState = container.querySelector<HTMLOutputElement>(
    '[data-slot="dialog-state"]'
  )
  assert.ok(trigger)
  assert.ok(dialogState)
  assert.equal(trigger.textContent?.includes(formatQuota(980000)), true)
  assert.equal(dialogState.dataset.open, '')

  await act(async () => {
    trigger.dispatchEvent(new Event('click', { bubbles: true }))
  })

  assert.equal(dialogState.dataset.open, 'quota-history')
  assert.equal(dialogState.dataset.userId, String(user.id))

  await act(async () => root.unmount())
  container.remove()
})
