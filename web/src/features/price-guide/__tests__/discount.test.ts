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

import type { PricingModel } from '../../pricing/types'
import {
  getMaxPriceGuideSavingPercent,
  getPriceGuideGroupSavingPercent,
} from '../lib'

function makeModel(overrides: Partial<PricingModel> = {}): PricingModel {
  return {
    id: 1,
    model_name: 'test-model',
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: ['default'],
    ...overrides,
  }
}

describe('price guide group discount', () => {
  it('returns the largest discount among the related models', () => {
    const savingPercent = getMaxPriceGuideSavingPercent({
      models: [
        makeModel({ official_price_basis: 'one_to_one' }),
        makeModel({
          id: 2,
          model_name: 'exchange-rate-model',
          official_price_basis: 'consume_usd_exchange_rate',
        }),
      ],
      selectedGroupRatio: 0.8,
      priceRate: 4.5,
      usdExchangeRate: 9,
    })

    expect(savingPercent).toBeCloseTo(60)
  })

  it('returns null when the selected group has no related models', () => {
    expect(
      getMaxPriceGuideSavingPercent({
        models: [],
        selectedGroupRatio: 0.8,
        priceRate: 4.5,
        usdExchangeRate: 9,
      })
    ).toBeNull()
  })

  it('returns null when no related model has a valid discount', () => {
    expect(
      getMaxPriceGuideSavingPercent({
        models: [makeModel()],
        selectedGroupRatio: 0.8,
        priceRate: 0,
        usdExchangeRate: 9,
      })
    ).toBeNull()
  })

  it('returns the largest discount for a specific group only', () => {
    const savingPercent = getPriceGuideGroupSavingPercent({
      models: [
        makeModel({
          id: 1,
          enable_groups: ['group-a'],
          official_price_basis: 'one_to_one',
        }),
        makeModel({
          id: 2,
          enable_groups: ['group-b'],
          official_price_basis: 'consume_usd_exchange_rate',
        }),
      ],
      group: 'group-a',
      selectedGroupRatio: 0.8,
      priceRate: 4.5,
      usdExchangeRate: 9,
    })

    expect(savingPercent).toBeCloseTo(20)
  })
})
