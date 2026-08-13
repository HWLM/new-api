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
import type { OfficialPriceBasis, PricingModel } from '../types'

export const OFFICIAL_PRICE_BASIS = {
  AUTO: 'auto',
  ONE_TO_ONE: 'one_to_one',
  CONSUME_USD_EXCHANGE_RATE: 'consume_usd_exchange_rate',
} as const satisfies Record<string, OfficialPriceBasis>

export function normalizeOfficialPriceBasis(
  value: string | null | undefined
): OfficialPriceBasis {
  switch ((value || '').trim().toLowerCase()) {
    case OFFICIAL_PRICE_BASIS.ONE_TO_ONE:
    case '1:1':
    case 'one-to-one':
      return OFFICIAL_PRICE_BASIS.ONE_TO_ONE
    case OFFICIAL_PRICE_BASIS.CONSUME_USD_EXCHANGE_RATE:
    case 'consume':
    case 'exchange_rate':
    case 'usd_exchange_rate':
      return OFFICIAL_PRICE_BASIS.CONSUME_USD_EXCHANGE_RATE
    default:
      return OFFICIAL_PRICE_BASIS.AUTO
  }
}

export function getEffectiveOfficialPriceBasis(
  model: PricingModel
): Exclude<OfficialPriceBasis, 'auto'> {
  const basis = normalizeOfficialPriceBasis(model.official_price_basis)
  return basis === OFFICIAL_PRICE_BASIS.AUTO
    ? OFFICIAL_PRICE_BASIS.CONSUME_USD_EXCHANGE_RATE
    : basis
}
