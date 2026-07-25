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

import {
  DOUBAO_SEEDANCE_2_PRICING_EXPR,
  LEGACY_TASK_PRECONSUME_TOKENS_PER_SECOND,
  MATRIX_UNMATCHED_TIER_LABEL,
  cellKeyFromCoords,
  cellKeyFromMatchedParams,
  cellKeyFromTierConditions,
  createDefaultEvalInputs,
  evalExprLocallyV2,
  generateExprFromVisualConfigV2,
  matrixToVisualConfigV2,
  tryParseMatrixFromVisualConfig,
  tryParseVisualConfigV2,
} from './tier-expr-v2'

describe('Doubao Seedance 2.0 pricing preset', () => {
  it('represents the complete resolution and video-input price matrix', () => {
    const config = tryParseVisualConfigV2(DOUBAO_SEEDANCE_2_PRICING_EXPR)
    if (!config) {
      throw new Error('Seedance preset must parse as a v2 visual config')
    }

    const matrix = tryParseMatrixFromVisualConfig(config)
    if (!matrix) {
      throw new Error('Seedance preset must parse as a price matrix')
    }
    expect(matrix.costUnit).toBe('per_mtok')
    expect(matrix.dimensions.map((dimension) => dimension.values)).toEqual([
      ['480p', '720p', '1080p', '4k'],
      ['true', 'false'],
    ])

    const prices = new Map([
      [['480p', 'true'], 4.3],
      [['480p', 'false'], 7.0],
      [['720p', 'true'], 4.3],
      [['720p', 'false'], 7.0],
      [['1080p', 'true'], 4.7],
      [['1080p', 'false'], 7.7],
      [['4k', 'true'], 2.4],
      [['4k', 'false'], 4.0],
    ])

    for (const [coords, price] of prices) {
      const key = cellKeyFromCoords(matrix.dimensions, coords)
      expect(matrix.cells[key]).toBe(price)
    }

    const matchedTier = config.tiers.find(
      (tier) => tier.label === '1080p_false'
    )
    if (!matchedTier) {
      throw new Error('Expected the 1080p text-to-video tier')
    }
    expect(
      cellKeyFromTierConditions(matrix.dimensions, matchedTier.conditions)
    ).toBe(cellKeyFromCoords(matrix.dimensions, ['1080p', 'false']))
    expect(
      cellKeyFromMatchedParams(matrix.dimensions, {
        resolution: '1080p',
        has_video: false,
        has_image: true,
      })
    ).toBe(cellKeyFromCoords(matrix.dimensions, ['1080p', 'false']))

    const regenerated = generateExprFromVisualConfigV2(
      matrixToVisualConfigV2(matrix)
    )
    expect(regenerated).toContain(`tier("${MATRIX_UNMATCHED_TIER_LABEL}", 0)`)
  })

  it('automatically estimates pre-consume tokens from requested duration', () => {
    const config = tryParseVisualConfigV2(DOUBAO_SEEDANCE_2_PRICING_EXPR)
    if (!config) {
      throw new Error('Seedance preset must parse as a v2 visual config')
    }

    expect(config.preConsumeTokensPerSecond).toBe(
      LEGACY_TASK_PRECONSUME_TOKENS_PER_SECOND
    )

    const expr = generateExprFromVisualConfigV2(config)

    expect(expr).toContain(
      '(p > 0 ? p : (seconds > 0 ? 50000 * seconds : 180000000))'
    )
    expect(tryParseVisualConfigV2(expr)?.preConsumeTokensPerSecond).toBe(
      LEGACY_TASK_PRECONSUME_TOKENS_PER_SECOND
    )
  })

  it('evaluates the v2 expression with estimator token inputs', () => {
    const estimated = evalExprLocallyV2(DOUBAO_SEEDANCE_2_PRICING_EXPR, {
      ...createDefaultEvalInputs(),
      seconds: 5,
      resolution: '1080p',
      has_video: false,
    })
    expect(estimated.error).toBeNull()
    expect(estimated.matchedTier).toBe('1080p_false')
    expect(estimated.cost).toBeCloseTo(1.925)

    const settled = evalExprLocallyV2(DOUBAO_SEEDANCE_2_PRICING_EXPR, {
      ...createDefaultEvalInputs(),
      resolution: '1080p',
      has_video: false,
      promptTokens: 1_000_000,
    })
    expect(settled.error).toBeNull()
    expect(settled.cost).toBeCloseTo(7.7)

    const unmatched = evalExprLocallyV2(DOUBAO_SEEDANCE_2_PRICING_EXPR, {
      ...createDefaultEvalInputs(),
      resolution: '8k',
      has_video: false,
    })
    expect(unmatched.error).toBeNull()
    expect(unmatched.matchedTier).toBe(MATRIX_UNMATCHED_TIER_LABEL)
    expect(unmatched.cost).toBe(0)
  })

  it('marks an empty matrix as unmatched instead of free', () => {
    const config = matrixToVisualConfigV2({
      dimensions: [],
      costUnit: 'flat',
      cells: {},
    })

    expect(config.tiers).toHaveLength(1)
    expect(config.tiers[0].label).toBe(MATRIX_UNMATCHED_TIER_LABEL)
    expect(generateExprFromVisualConfigV2(config)).toContain(
      `tier("${MATRIX_UNMATCHED_TIER_LABEL}", 0)`
    )
  })
})
