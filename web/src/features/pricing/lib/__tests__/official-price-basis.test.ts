/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or (at your
option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT
ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License for more
details.

You should have received a copy of the GNU Affero General Public License along
with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'bun:test'

import type { PricingModel } from '../../types'
import {
  getEffectiveOfficialPriceBasis,
  normalizeOfficialPriceBasis,
  OFFICIAL_PRICE_BASIS,
} from '../official-price-basis'

function makeModel(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'test-model',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: [],
    ...overrides,
  } as PricingModel
}

describe('official price basis resolution', () => {
  it('normalizes legacy values', () => {
    expect(normalizeOfficialPriceBasis('1:1')).toBe(
      OFFICIAL_PRICE_BASIS.ONE_TO_ONE
    )
    expect(normalizeOfficialPriceBasis('consume')).toBe(
      OFFICIAL_PRICE_BASIS.CONSUME_USD_EXCHANGE_RATE
    )
    expect(normalizeOfficialPriceBasis('')).toBe(OFFICIAL_PRICE_BASIS.AUTO)
  })

  it('prefers model override over vendor basis', () => {
    const model = makeModel({
      official_price_basis: OFFICIAL_PRICE_BASIS.ONE_TO_ONE,
      vendor_official_price_basis:
        OFFICIAL_PRICE_BASIS.CONSUME_USD_EXCHANGE_RATE,
    })

    expect(getEffectiveOfficialPriceBasis(model)).toBe(
      OFFICIAL_PRICE_BASIS.ONE_TO_ONE
    )
  })

  it('defaults to foreign exchange-rate basis when model is auto', () => {
    const model = makeModel({
      official_price_basis: OFFICIAL_PRICE_BASIS.AUTO,
      vendor_official_price_basis: OFFICIAL_PRICE_BASIS.ONE_TO_ONE,
    })

    expect(getEffectiveOfficialPriceBasis(model)).toBe(
      OFFICIAL_PRICE_BASIS.CONSUME_USD_EXCHANGE_RATE
    )
  })

  it('defaults to foreign exchange-rate basis when no explicit basis is set', () => {
    const videoModel = makeModel({
      official_price_basis: OFFICIAL_PRICE_BASIS.AUTO,
      vendor_official_price_basis: OFFICIAL_PRICE_BASIS.AUTO,
      model_name: 'seedance-v3',
      owner_by: 'seedance',
    })

    expect(getEffectiveOfficialPriceBasis(videoModel)).toBe(
      OFFICIAL_PRICE_BASIS.CONSUME_USD_EXCHANGE_RATE
    )
  })
})
