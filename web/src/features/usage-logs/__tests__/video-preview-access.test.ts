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
import { afterEach, describe, test } from 'node:test'

import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'

import { api } from '@/lib/api'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { getTaskVideo } from '../api'

const originalAdapter = api.defaults.adapter

const adminBundle: AuthBundle = {
  access_token: 'admin-access-token',
  token_type: 'Bearer',
  access_expires_at: Math.floor(Date.now() / 1000) + 600,
  user: {
    id: 10,
    username: 'admin',
    role: 10,
  },
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

afterEach(() => {
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.reset('idle')
})

describe('task video preview access', () => {
  test('loads another user task video through the authenticated API client', async () => {
    useAuthStore.getState().auth.setBundle(adminBundle)
    let capturedConfig: InternalAxiosRequestConfig | undefined
    const responseBlob = new Blob(['video'], { type: 'video/mp4' })

    const adapter: AxiosAdapter = async (config) => {
      capturedConfig = config
      return {
        data: responseBlob,
        status: 200,
        statusText: 'OK',
        headers: { 'content-type': 'video/mp4' },
        config,
      }
    }
    api.defaults.adapter = adapter

    const result = await getTaskVideo('task other/user')

    assert.strictEqual(result, responseBlob)
    assert.equal(capturedConfig?.url, '/v1/videos/task%20other%2Fuser/content')
    assert.equal(capturedConfig?.responseType, 'blob')
    assert.equal(
      capturedConfig?.headers.get('Authorization'),
      'Bearer admin-access-token'
    )
  })
})
