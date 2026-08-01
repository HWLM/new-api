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

import type { PricingModel } from '../../types'
import { buildSupportedParameters } from '../mock-stats'
import {
  buildSeedanceV3Sample,
  isSeedanceV3ModelName,
  SEEDANCE_V3_ENDPOINT_PATH,
} from '../seedance-v3-api'

function pricingModel(modelName: string): PricingModel {
  return { model_name: modelName } as PricingModel
}

describe('Seedance v3 marketplace API contract', () => {
  it('recognizes every Seedance 2.0 model family without matching older models', () => {
    expect(isSeedanceV3ModelName('dreamina-seedance-2-0-hc')).toBe(true)
    expect(isSeedanceV3ModelName('doubao-seedance-2-0-fast-260128')).toBe(true)
    expect(isSeedanceV3ModelName('doubao-seedance-1-0')).toBe(false)
  })

  it('shows the HC v3 request fields and supported values', () => {
    const params = buildSupportedParameters(
      pricingModel('dreamina-seedance-2-0-hc')
    )

    expect(params.map((param) => param.name)).toEqual([
      'content',
      'duration',
      'resolution',
      'ratio',
      'generate_audio',
      'watermark',
    ])
    expect(params.find((param) => param.name === 'duration')?.range).toBe(
      '4 ~ 15'
    )
    expect(params.find((param) => param.name === 'resolution')?.enumValues).toEqual(
      ['480p', '720p', '1080p']
    )
  })

  it('includes Doubao v3 values and passthrough parameters', () => {
    const params = buildSupportedParameters(
      pricingModel('doubao-seedance-2-0-filter-off')
    )

    expect(params.find((param) => param.name === 'duration')?.range).toBe(
      '-1 or 4 ~ 15'
    )
    expect(params.find((param) => param.name === 'resolution')?.enumValues).toContain(
      '4K'
    )
    expect(params.find((param) => param.name === 'ratio')?.enumValues).toContain(
      'adaptive'
    )
    expect(params.map((param) => param.name)).toEqual(
      expect.arrayContaining([
        'tools',
        'service_tier',
        'draft',
        'frames',
        'camera_fixed',
      ])
    )
  })

  it('builds copyable samples against the unified v3 endpoint and body', () => {
    const sample = buildSeedanceV3Sample('curl', {
      baseUrl: 'https://api.example.com',
      apiKeyEnv: 'NEW_API_KEY',
      modelName: 'doubao-seedance-2-0',
    })

    expect(sample).toContain(`https://api.example.com${SEEDANCE_V3_ENDPOINT_PATH}`)
    expect(sample).toContain('"content"')
    expect(sample).toContain('"resolution": "720p"')
    expect(sample).toContain('"ratio": "16:9"')
    expect(sample).not.toContain('"prompt"')
    expect(sample).not.toContain('"aspect_ratio"')
    expect(sample).not.toContain('"fps"')

    const pythonSample = buildSeedanceV3Sample('python', {
      baseUrl: 'https://api.example.com',
      apiKeyEnv: 'NEW_API_KEY',
      modelName: 'doubao-seedance-2-0',
    })
    expect(pythonSample).toContain('"generate_audio": True')
    expect(pythonSample).toContain('"watermark": False')
    expect(pythonSample).not.toContain('"generate_audio": true')
  })
})
