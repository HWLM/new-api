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
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'
import type React from 'react'

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
const { UserQuotaHistoryList } =
  await import('../dialogs/user-quota-history-list')
const { formatQuota } = await import('@/lib/format')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        Change: 'Change',
        'Before change': 'Before change',
        'After change': 'After change',
        Consume: 'Consume',
        'Current quota': 'Current quota',
        Details: 'Details',
        'Failed to load': 'Failed to load',
        Loading: 'Loading',
        'Loading...': 'Loading...',
        Model: 'Model',
        'No quota change records': 'No quota change records',
        'Quota adjustment': 'Quota adjustment',
        'Quota change history': 'Quota change history',
        Refund: 'Refund',
        Retry: 'Retry',
        Time: 'Time',
        'Top-up': 'Top-up',
        Type: 'Type',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

type ListProps = React.ComponentProps<typeof UserQuotaHistoryList>

async function renderList(props: ListProps) {
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)

  await act(async () => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <UserQuotaHistoryList {...props} />
      </I18nextProvider>
    )
  })

  return { container, root }
}

async function unmountList(rendered: Awaited<ReturnType<typeof renderList>>) {
  await act(async () => rendered.root.unmount())
  rendered.container.remove()
}

function normalizedText(value: string | null): string {
  return (value ?? '').replaceAll(/\s/g, '')
}

describe('user quota history list', () => {
  after(() => {
    domWindow.close()
  })

  test('shows signed consume and refund changes with their details', async () => {
    const rendered = await renderList({
      currentQuota: 1250000,
      items: [
        {
          id: 1,
          created_at: 1785722940,
          type: 2,
          delta_quota: -500000,
          before_quota: 1500000,
          after_quota: 1000000,
          content: 'Seedance generation',
          model_name: 'doubao-seedance-2-0',
          token_name: 'seedance',
          request_id: 'request-consume',
        },
        {
          id: 2,
          created_at: 1785723000,
          type: 6,
          delta_quota: 250000,
          before_quota: 1000000,
          after_quota: 1250000,
          content: 'Token recalculation refund',
          model_name: 'doubao-seedance-2-0',
          token_name: 'seedance',
          request_id: 'request-refund',
        },
      ],
      isLoading: false,
      isError: false,
      isFetching: false,
      onRetry: () => {},
    })

    const text = normalizedText(rendered.container.textContent)
    assert.equal(text.includes('Consume'), true)
    assert.equal(text.includes('Refund'), true)
    assert.equal(text.includes(`-${formatQuota(500000)}`), true)
    assert.equal(text.includes(`+${formatQuota(250000)}`), true)
    assert.equal(text.includes(formatQuota(1500000)), true)
    assert.equal(text.includes(formatQuota(1000000)), true)
    assert.equal(text.includes(formatQuota(1250000)), true)
    assert.equal(text.includes('Seedancegeneration'), true)
    assert.equal(text.includes('Tokenrecalculationrefund'), true)
    const detailsTrigger = rendered.container.querySelector<HTMLElement>(
      '[data-slot="quota-history-details-trigger"]'
    )
    assert.ok(detailsTrigger)
    assert.equal(detailsTrigger.tabIndex, 0)

    await act(async () => {
      detailsTrigger.focus()
      await Promise.resolve()
    })
    const detailsTooltip = document.querySelector(
      '[data-slot="tooltip-content"]'
    )
    assert.ok(detailsTooltip)
    const tooltipText = normalizedText(detailsTooltip.textContent)
    assert.equal(tooltipText.includes('Seedancegeneration'), true)
    assert.equal(tooltipText.includes('doubao-seedance-2-0'), true)
    assert.equal(tooltipText.includes('request-consume'), true)

    const quotaSummary = rendered.container.querySelector(
      '[data-slot="current-quota-summary"]'
    )
    assert.ok(quotaSummary)
    assert.equal(
      normalizedText(quotaSummary.textContent).includes(
        normalizedText(`Current quota ${formatQuota(1250000)}`)
      ),
      true
    )

    await unmountList(rendered)
  })

  test('shows the empty state when the user has no quota changes', async () => {
    const rendered = await renderList({
      currentQuota: 0,
      items: [],
      isLoading: false,
      isError: false,
      isFetching: false,
      onRetry: () => {},
    })

    assert.equal(
      rendered.container.textContent?.includes('No quota change records'),
      true
    )

    await unmountList(rendered)
  })

  test('keeps the table header fixed while quota rows scroll', async () => {
    const rendered = await renderList({
      currentQuota: 1000000,
      items: [
        {
          id: 1,
          created_at: 1785722940,
          type: 2,
          delta_quota: -500000,
          before_quota: 1500000,
          after_quota: 1000000,
          content: 'Seedance generation',
          model_name: 'doubao-seedance-2-0',
          token_name: 'seedance',
          request_id: 'request-consume',
        },
      ],
      isLoading: false,
      isError: false,
      isFetching: false,
      onRetry: () => {},
    })

    const tableContainer = rendered.container.querySelector(
      '[data-slot="table-container"]'
    )
    const tableHeader = rendered.container.querySelector(
      '[data-slot="table-header"]'
    )
    assert.ok(tableContainer)
    assert.ok(tableHeader)
    assert.equal(tableContainer.classList.contains('h-full'), true)
    assert.equal(tableContainer.classList.contains('overflow-auto'), true)
    assert.equal(tableHeader.classList.contains('sticky'), true)
    assert.equal(tableHeader.classList.contains('top-0'), true)

    await unmountList(rendered)
  })

  test('offers a working retry action when loading fails', async () => {
    let retryCount = 0
    const rendered = await renderList({
      currentQuota: 0,
      items: [],
      isLoading: false,
      isError: true,
      isFetching: false,
      onRetry: () => {
        retryCount++
      },
    })

    const retryButton = [...rendered.container.querySelectorAll('button')].find(
      (button) => button.textContent?.includes('Retry')
    )
    assert.ok(retryButton)

    await act(async () => {
      retryButton.dispatchEvent(new Event('click', { bubbles: true }))
    })
    assert.equal(retryCount, 1)

    await unmountList(rendered)
  })
})
