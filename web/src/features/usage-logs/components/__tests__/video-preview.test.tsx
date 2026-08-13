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
import { after, afterEach, describe, test } from 'node:test'

import type {
  AxiosAdapter,
  AxiosResponse,
  InternalAxiosRequestConfig,
} from 'axios'
import { Window } from 'happy-dom'

import { api } from '@/lib/api'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLVideoElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
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
const { QueryClient, QueryClientProvider, notifyManager } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { TaskVideoPreview } = await import('../task-video-preview')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'Click to preview video': 'Click to preview video',
        'Failed to load': 'Failed to load',
        'Task ID:': 'Task ID:',
        Video: 'Video',
      },
    },
  },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const originalAdapter = api.defaults.adapter
const createObjectURLDescriptor = Object.getOwnPropertyDescriptor(
  URL,
  'createObjectURL'
)
const revokeObjectURLDescriptor = Object.getOwnPropertyDescriptor(
  URL,
  'revokeObjectURL'
)

const adminBundle: AuthBundle = {
  access_token: 'admin-access-token',
  token_type: 'Bearer',
  access_expires_at: Math.floor(Date.now() / 1000) + 600,
  user: { id: 10, username: 'admin', role: 10 },
  session: {
    sid: 'admin-session',
    current: true,
    login_method: 'password',
    ip: '127.0.0.1',
    user_agent: 'test',
    created_at: 100,
    last_active_at: 100,
    expires_at: 1000,
  },
}

function restoreURLMethod(
  key: 'createObjectURL' | 'revokeObjectURL',
  descriptor: PropertyDescriptor | undefined
) {
  if (descriptor) {
    Object.defineProperty(URL, key, descriptor)
  } else {
    Reflect.deleteProperty(URL, key)
  }
}

afterEach(() => {
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.reset('idle')
  restoreURLMethod('createObjectURL', createObjectURLDescriptor)
  restoreURLMethod('revokeObjectURL', revokeObjectURLDescriptor)
  notifyManager.setScheduler((callback) => {
    setTimeout(callback, 0)
  })
  document.body.replaceChildren()
})

after(() => {
  domWindow.close()
})

describe('task video preview', () => {
  test('opens an authenticated video blob in the preview dialog and releases it', async () => {
    notifyManager.setScheduler((callback) => callback())
    useAuthStore.getState().auth.setBundle(adminBundle)
    const revokedUrls: string[] = []
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: () => 'blob:task-video',
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      configurable: true,
      value: (url: string) => revokedUrls.push(url),
    })

    let resolveRequest: ((response: AxiosResponse<Blob>) => void) | undefined
    let requestedConfig: InternalAxiosRequestConfig | undefined
    const responseBlob = new Blob(['video'], { type: 'video/mp4' })
    const adapter: AxiosAdapter = (config) =>
      new Promise((resolve) => {
        requestedConfig = config
        resolveRequest = resolve
      })
    api.defaults.adapter = adapter

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <I18nextProvider i18n={i18n}>
            <TaskVideoPreview taskId='task_other_user' />
          </I18nextProvider>
        </QueryClientProvider>
      )
    })

    const trigger = container.querySelector<HTMLButtonElement>('button')
    assert.ok(trigger)
    assert.equal(trigger.textContent?.includes('Click to preview video'), true)

    await act(async () => {
      trigger.dispatchEvent(new Event('click', { bubbles: true }))
      await Promise.resolve()
    })

    assert.ok(resolveRequest)
    assert.ok(requestedConfig)
    const responseConfig = requestedConfig
    assert.equal(
      responseConfig.headers.get('Authorization'),
      'Bearer admin-access-token'
    )

    const queryKey = ['task-video-preview', 'task_other_user']
    let unsubscribe = () => {}
    const querySucceeded = new Promise<void>((resolve) => {
      unsubscribe = queryClient.getQueryCache().subscribe(() => {
        if (queryClient.getQueryState(queryKey)?.status === 'success') {
          resolve()
        }
      })
    })
    await act(async () => {
      resolveRequest?.({
        data: responseBlob,
        status: 200,
        statusText: 'OK',
        headers: { 'content-type': 'video/mp4' },
        config: responseConfig,
      })
      await querySucceeded
    })
    unsubscribe()
    await act(async () => {})

    const video = document.querySelector<HTMLVideoElement>(
      'video[aria-label="Video"]'
    )
    assert.ok(video)
    assert.equal(video.getAttribute('src'), 'blob:task-video')
    assert.equal(document.body.textContent?.includes('task_other_user'), true)

    await act(async () => root.unmount())
    assert.deepEqual(revokedUrls, ['blob:task-video'])
    queryClient.clear()
  })
})
