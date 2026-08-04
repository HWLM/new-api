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
import { describe, expect, it } from 'bun:test'

import type { Channel } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from './channel-form'

describe('Seedance channel routes', () => {
  it('stores and restores per-model video request formats', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 54,
      video_request_format_by_model: JSON.stringify({
        '*': 'openai',
        'doubao-seedance-2-0-filter-off': 'seedance_v3',
      }),
    })

    const settings = JSON.parse(String(payload.channel.settings))
    expect(settings.video_request_format_by_model).toEqual({
      '*': 'openai',
      'doubao-seedance-2-0-filter-off': 'seedance_v3',
    })

    const defaults = transformChannelToFormDefaults({
      ...payload.channel,
      channel_info: {},
    } as Channel)
    expect(JSON.parse(defaults.video_request_format_by_model || '{}')).toEqual(
      settings.video_request_format_by_model
    )
  })

  it('stores only configured routes for supported video channels', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 54,
      seedance_asset_create_method: 'PUT',
      seedance_asset_create_target: 'https://asset.example.com/create',
      seedance_asset_create_parameters: JSON.stringify({ region: 'global' }),
      seedance_asset_create_response_mapping: JSON.stringify({
        id: '{data.asset_id}',
        data: null,
      }),
      seedance_asset_get_method: 'POST',
      seedance_asset_get_target: 'https://asset.example.com/query',
      seedance_asset_get_parameters: JSON.stringify({ asset_id: '{Id}' }),
      seedance_asset_get_response_mapping: JSON.stringify({
        Id: '{data.asset_id}',
        data: null,
      }),
      seedance_task_create_method: 'POST',
      seedance_task_create_target: '/tasks',
      seedance_task_get_method: 'GET',
      seedance_task_get_target: '/tasks/{task_id}',
    })

    expect(JSON.parse(String(payload.channel.settings))).toMatchObject({
      seedance_v3_routes: {
        asset_create: {
          method: 'PUT',
          target: 'https://asset.example.com/create',
          parameters: { region: 'global' },
          response_mapping: { id: '{data.asset_id}', data: null },
        },
        asset_get: {
          method: 'POST',
          target: 'https://asset.example.com/query',
          parameters: { asset_id: '{Id}' },
          response_mapping: { Id: '{data.asset_id}', data: null },
        },
        task_create: { method: 'POST', target: '/tasks' },
        task_get: { method: 'GET', target: '/tasks/{task_id}' },
      },
    })
  })

  it('keeps route configuration absent when every target is empty', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      type: 81,
    })

    expect(JSON.parse(String(payload.channel.settings))).not.toHaveProperty(
      'seedance_v3_routes'
    )
  })

  it('restores configured routes when editing a channel', () => {
    const defaults = transformChannelToFormDefaults({
      ...transformFormDataToCreatePayload({
        ...CHANNEL_FORM_DEFAULT_VALUES,
        type: 54,
        seedance_asset_create_method: 'PUT',
        seedance_asset_create_target: '/assets',
        seedance_asset_get_method: 'POST',
        seedance_asset_get_target: '/assets/query',
        seedance_asset_get_parameters: JSON.stringify({ asset_id: '{Id}' }),
        seedance_asset_get_response_mapping: JSON.stringify({
          Id: '{data.asset_id}',
          data: null,
        }),
        seedance_task_create_method: 'PATCH',
        seedance_task_create_target: '/tasks/create',
        seedance_task_create_parameters: JSON.stringify({ priority: 10 }),
        seedance_task_create_response_mapping: JSON.stringify({
          id: '{task.id}',
        }),
        seedance_task_get_method: 'POST',
        seedance_task_get_target: '/tasks/query',
        seedance_task_get_parameters: JSON.stringify({
          task_id: null,
          job_id: '{task_id}',
        }),
        seedance_task_get_response_mapping: JSON.stringify({
          task: '{data.task}',
          data: null,
        }),
      }).channel,
      channel_info: {},
    } as Channel)

    expect(defaults.seedance_asset_create_method).toBe('PUT')
    expect(defaults.seedance_asset_create_target).toBe('/assets')
    expect(defaults.seedance_asset_get_method).toBe('POST')
    expect(defaults.seedance_asset_get_target).toBe('/assets/query')
    expect(JSON.parse(defaults.seedance_asset_get_parameters || '{}')).toEqual({
      asset_id: '{Id}',
    })
    expect(
      JSON.parse(defaults.seedance_asset_get_response_mapping || '{}')
    ).toEqual({ Id: '{data.asset_id}', data: null })
    expect(defaults.seedance_task_create_method).toBe('PATCH')
    expect(defaults.seedance_task_create_target).toBe('/tasks/create')
    expect(
      JSON.parse(defaults.seedance_task_create_parameters || '{}')
    ).toEqual({ priority: 10 })
    expect(
      JSON.parse(defaults.seedance_task_create_response_mapping || '{}')
    ).toEqual({ id: '{task.id}' })
    expect(defaults.seedance_task_get_method).toBe('POST')
    expect(defaults.seedance_task_get_target).toBe('/tasks/query')
    expect(JSON.parse(defaults.seedance_task_get_parameters || '{}')).toEqual({
      task_id: null,
      job_id: '{task_id}',
    })
    expect(
      JSON.parse(defaults.seedance_task_get_response_mapping || '{}')
    ).toEqual({ task: '{data.task}', data: null })
  })
})
