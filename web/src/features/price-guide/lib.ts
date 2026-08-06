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
import { EXCLUDED_GROUPS } from '@/features/pricing/constants'
import type { PricingModel } from '@/features/pricing/types'

export type PriceGuideGroupOption = {
  value: string
  label: string
  description: string
  ratio: number
}

export type PriceGuidePriceKind = 'input' | 'output' | 'cache' | 'request'
export type PriceGuidePricingSource = 'converted' | 'official'

function toFiniteNumber(value: unknown): number | null {
  const number = Number(value)
  return Number.isFinite(number) ? number : null
}

export function isDynamicPricingGuideModel(model: PricingModel): boolean {
  return model.billing_mode === 'tiered_expr' && Boolean(model.billing_expr)
}

export function isVideoPricingGuideModel(model: PricingModel): boolean {
  const modalities = [
    ...(model.input_modalities || []),
    ...(model.output_modalities || []),
  ].map((item) => item.toLowerCase())
  const endpointTypes = (model.supported_endpoint_types || []).map((item) =>
    item.toLowerCase()
  )
  const tags = (model.tags || '').toLowerCase()
  const modelName = (model.model_name || '').toLowerCase()

  return (
    modalities.includes('video') ||
    endpointTypes.some(
      (item) => item.includes('video') || item.includes('seedance')
    ) ||
    tags.includes('video') ||
    /(?:seedance|sora|veo|kling|video|wan-|hunyuanvideo)/i.test(modelName)
  )
}

export function getGroupRatioSavingPercent(groupRatio: number): number | null {
  return getSavingPercent(1, groupRatio)
}

export function getPriceGuidePricingSource(
  model: PricingModel
): PriceGuidePricingSource {
  const ownerBy = (model.owner_by || '').trim().toLowerCase()
  if (
    ownerBy === 'converted' ||
    ownerBy === 'settlement' ||
    ownerBy === 'video' ||
    ownerBy === 'seedance'
  ) {
    return 'converted'
  }

  if (isVideoPricingGuideModel(model)) {
    return 'converted'
  }

  return 'official'
}

export function getPriceGuideSavingPercent(params: {
  model: PricingModel
  selectedGroupRatio: number
  priceRate: number
  usdExchangeRate: number
}): number | null {
  if (getPriceGuidePricingSource(params.model) === 'converted') {
    return getGroupRatioSavingPercent(params.selectedGroupRatio)
  }

  if (
    !Number.isFinite(params.selectedGroupRatio) ||
    !Number.isFinite(params.priceRate) ||
    !Number.isFinite(params.usdExchangeRate) ||
    params.selectedGroupRatio < 0 ||
    params.priceRate <= 0 ||
    params.usdExchangeRate <= 0
  ) {
    return null
  }

  return getSavingPercent(
    1,
    (params.selectedGroupRatio * params.priceRate) / params.usdExchangeRate
  )
}

export function getPriceGuideGroupOptions(
  usableGroup: Record<string, { desc: string; ratio: number | string }>,
  groupRatio: Record<string, number>
): PriceGuideGroupOption[] {
  return Object.entries(usableGroup)
    .filter(([group]) => !EXCLUDED_GROUPS.includes(group))
    .map(([group, info]) => {
      const configuredRatio = toFiniteNumber(groupRatio[group])
      const displayedRatio = toFiniteNumber(info.ratio)
      let ratio = 1
      if (configuredRatio != null && configuredRatio >= 0) {
        ratio = configuredRatio
      } else if (displayedRatio != null && displayedRatio >= 0) {
        ratio = displayedRatio
      }

      return {
        value: group,
        label: group,
        description: info.desc?.trim() || '',
        ratio,
      }
    })
    .sort((left, right) => {
      if (left.ratio !== right.ratio) return left.ratio - right.ratio
      return left.label.localeCompare(right.label)
    })
}

export function getBestPriceGuideGroup(
  groupOptions: PriceGuideGroupOption[]
): string {
  return groupOptions[0]?.value ?? ''
}

export function filterModelsByGroup(
  models: PricingModel[],
  group: string
): PricingModel[] {
  if (!group) return models
  return models.filter((model) => model.enable_groups?.includes(group))
}

export function getGroupRatio(
  groupRatio: Record<string, number>,
  group: string
): number {
  const ratio = toFiniteNumber(groupRatio[group])
  return ratio != null && ratio >= 0 ? ratio : 1
}

export function getSelectedGroupRatio(
  groupRatio: Record<string, number>,
  group: string
): number {
  return getGroupRatio(groupRatio, group)
}

export function getTokenBaseUsdPrice(
  model: PricingModel,
  kind: Exclude<PriceGuidePriceKind, 'request'>
): number | null {
  const inputUsd = toFiniteNumber(model.model_ratio)
  const outputRatio = toFiniteNumber(model.completion_ratio)

  if (inputUsd == null || inputUsd < 0) {
    return null
  }

  const baseUsd = inputUsd * 2
  switch (kind) {
    case 'input':
      return baseUsd
    case 'output':
      return outputRatio != null && outputRatio >= 0 ? baseUsd * outputRatio : null
    case 'cache': {
      const cacheRatio = toFiniteNumber(model.cache_ratio)
      return cacheRatio != null && cacheRatio >= 0
        ? baseUsd * cacheRatio
        : null
    }
  }
}

export function getRequestBaseUsdPrice(model: PricingModel): number | null {
  const requestUsd = toFiniteNumber(model.model_price)
  return requestUsd != null && requestUsd >= 0 ? requestUsd : null
}

export function getActualUsdPrice(
  baseUsd: number,
  selectedGroupRatio: number,
  priceRate: number,
  usdExchangeRate: number
): number | null {
  if (
    !Number.isFinite(baseUsd) ||
    !Number.isFinite(selectedGroupRatio) ||
    !Number.isFinite(priceRate) ||
    !Number.isFinite(usdExchangeRate) ||
    baseUsd < 0 ||
    selectedGroupRatio < 0 ||
    priceRate <= 0 ||
    usdExchangeRate <= 0
  ) {
    return null
  }

  return (baseUsd * selectedGroupRatio * priceRate) / usdExchangeRate
}

export function getSavingPercent(
  officialUsd: number | null,
  actualUsd: number | null
): number | null {
  if (
    officialUsd == null ||
    actualUsd == null ||
    !Number.isFinite(officialUsd) ||
    !Number.isFinite(actualUsd) ||
    officialUsd <= 0
  ) {
    return null
  }

  const saving = 1 - actualUsd / officialUsd
  if (!Number.isFinite(saving)) return null
  return Math.max(0, Math.min(1, saving)) * 100
}
