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
import { Tag as TagIcon } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { normalizeTierLabel } from '../lib/billing-expr'
import {
  PRECONSUME_TIER_LABEL,
  cellKeyFromCoords,
  cellKeyFromMatchedParams,
  cellKeyFromTierConditions,
  dimensionDisplayLabel,
  matrixUnitSuffixKey,
  normalizeVisualConfigV2,
  taskVarLabelKey,
  tryParseMatrixFromVisualConfig,
  tryParseVisualConfigV2,
  type MatrixCostUnit,
  type MatrixDimension,
  type VisualMatrixV2,
  type VisualTierV2,
} from '../lib/tier-expr-v2'
import { HumanConditions, HumanFormula } from './human-formula'

type V2PricingBreakdownProps = {
  billingExpr: string
  matchedTierLabel?: string | null
  matchedParams?: Readonly<Record<string, unknown>> | null
  compact?: boolean
  embedded?: boolean
  // Multiplier applied to every displayed price (matrix cell + human formula
  // term). Callers pass the viewing group's ratio so the marketplace shows
  // group-adjusted prices instead of raw base prices. Defaults to 1 (base).
  groupRatioMultiplier?: number
  currencySymbol?: string
  currencyRate?: number
  officialCurrencyRate?: number
  showComparison?: boolean
}

// Per-1M-tokens semantics: v2 quotaConversion multiplies by QuotaPerUnit, so
// a coefficient like `7.7 * p / 1000000` yields $7.70 per 1M tokens. The
// marketplace formula uses HumanFormula to render tier bodies naturally.

function costUnitLabel(unit: MatrixCostUnit, t: (k: string) => string): string {
  switch (unit) {
    case 'flat':
      return t('Flat $/call')
    case 'per_second':
      return t('$ × seconds')
    case 'per_n':
      return t('$ × n')
    case 'per_mtok':
      return t('$ / 1M tokens')
  }
}

// Translated dimension header — "resolution" → "分辨率", param path → literal.
function translatedDimensionLabel(
  d: MatrixDimension,
  t: (k: string) => string
): string {
  switch (d.source.kind) {
    case 'v2_string':
    case 'v2_bool':
      return t(taskVarLabelKey(d.source.var))
    case 'param_string':
    case 'param_bool':
      return dimensionDisplayLabel(d)
  }
}

export function V2PricingBreakdown({
  billingExpr,
  matchedTierLabel,
  matchedParams,
  compact = false,
  embedded = false,
  groupRatioMultiplier = 1,
  currencySymbol,
  currencyRate,
  officialCurrencyRate,
  showComparison = false,
}: V2PricingBreakdownProps) {
  const { t } = useTranslation()
  const currency = useSystemConfigStore((s) => s.config.currency)

  const { symbol, rate } = useMemo(() => {
    if (
      currencySymbol &&
      currencyRate != null &&
      Number.isFinite(currencyRate) &&
      currencyRate > 0
    ) {
      return { symbol: currencySymbol, rate: currencyRate }
    }
    if (currency.quotaDisplayType === 'CNY') {
      return { symbol: '¥', rate: currency.usdExchangeRate || 7 }
    }
    if (currency.quotaDisplayType === 'CUSTOM') {
      return {
        symbol: currency.customCurrencySymbol || '¤',
        rate: currency.customCurrencyExchangeRate || 1,
      }
    }
    return { symbol: '$', rate: 1 }
  }, [currency, currencyRate, currencySymbol])
  const officialRate =
    officialCurrencyRate != null &&
    Number.isFinite(officialCurrencyRate) &&
    officialCurrencyRate > 0
      ? officialCurrencyRate
      : rate

  // Fold the group ratio into the currency rate — every price shown by
  // MatrixTable / TierList / HumanFormula multiplies by this, so a single
  // multiply here reaches every leaf without threading a new prop.
  const effectiveRate =
    rate * (Number.isFinite(groupRatioMultiplier) ? groupRatioMultiplier : 1)

  const parsed = useMemo(
    () => tryParseVisualConfigV2(billingExpr),
    [billingExpr]
  )
  const matrix = useMemo(() => {
    if (!parsed) return null
    const cfg = normalizeVisualConfigV2(parsed)
    return tryParseMatrixFromVisualConfig(cfg)
  }, [parsed])
  const matchedMatrixCellKey = useMemo(() => {
    if (!parsed || !matrix) return null
    if (matchedParams) {
      const key = cellKeyFromMatchedParams(matrix.dimensions, matchedParams)
      if (key) return key
    }
    if (!matchedTierLabel) return null
    const normalizedMatched = normalizeTierLabel(matchedTierLabel)
    const matchedTier = parsed.tiers.find(
      (tier) => normalizeTierLabel(tier.label) === normalizedMatched
    )
    if (!matchedTier) return null
    return cellKeyFromTierConditions(matrix.dimensions, matchedTier.conditions)
  }, [matchedParams, matchedTierLabel, matrix, parsed])
  const matchedParamEntries = useMemo(() => {
    if (!matchedParams) return []
    const preferredOrder = [
      'resolution',
      'size',
      'has_video',
      'has_image',
      'seconds',
      'n',
      'mode',
    ]
    const order = new Map(preferredOrder.map((key, index) => [key, index]))
    return Object.entries(matchedParams)
      .filter((entry) => entry[1] !== undefined && entry[1] !== null)
      .sort((a, b) => {
        const aOrder = order.get(a[0]) ?? preferredOrder.length
        const bOrder = order.get(b[0]) ?? preferredOrder.length
        return aOrder === bOrder ? a[0].localeCompare(b[0]) : aOrder - bOrder
      })
  }, [matchedParams])

  // Fall back to a raw-expression display if neither matrix nor tier list
  // parses cleanly — matches DynamicPricingBreakdown's "special billing" UX.
  if (!parsed) {
    return (
      <section className={cn('min-w-0', !compact && 'py-4')}>
        {!compact && (
          <BreakdownHeader
            compact={compact}
            title={t('Dynamic Pricing')}
            subtitle={t('v2 expression — not visually representable')}
          />
        )}
        <div className='text-muted-foreground mb-1 text-[10px] font-medium tracking-wider uppercase'>
          {t('Raw expression')}
        </div>
        <code className='text-muted-foreground block text-xs break-all'>
          {billingExpr}
        </code>
      </section>
    )
  }

  const tiers = parsed.tiers.filter(
    (tier) =>
      // Hide the trailing zero-cost fallback and the synthetic pre-consume
      // tier — end users see them as clutter, and the pre-consume amount is
      // surfaced as a separate note below the table.
      tier.label !== PRECONSUME_TIER_LABEL &&
      !(
        tier.conditions.length === 0 &&
        tier.flat_cost === 0 &&
        tier.per_second_cost === 0 &&
        tier.per_n_cost === 0 &&
        tier.per_mtok_cost === 0
      )
  )

  return (
    <section className={cn('min-w-0', !compact && 'py-3 sm:py-4')}>
      {!compact && (
        <BreakdownHeader
          compact={compact}
          title={t('Dynamic Pricing')}
          subtitle={t('Prices vary by request parameters (video / task)')}
        />
      )}

      {matchedParamEntries.length > 0 && (
        <div className='text-muted-foreground mb-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
          <span className='font-medium'>{t('Parameters')}:</span>
          {matchedParamEntries.map(([key, value]) => (
            <code key={key} className='text-foreground font-mono text-xs'>
              {key}={String(value)}
            </code>
          ))}
        </div>
      )}

      {matrix && matrix.dimensions.length > 0 ? (
        <MatrixTable
          matrix={matrix}
          symbol={symbol}
          rate={effectiveRate}
          officialRate={officialRate}
          compact={compact}
          embedded={embedded}
          matchedTierLabel={matchedTierLabel ?? null}
          matchedCellKey={matchedMatrixCellKey}
          showComparison={showComparison}
          t={t}
        />
      ) : (
        <TierList
          tiers={tiers}
          symbol={symbol}
          officialRate={officialRate}
          compact={compact}
          embedded={embedded}
          matchedTierLabel={matchedTierLabel ?? null}
          showComparison={showComparison}
          t={t}
        />
      )}
    </section>
  )
}

function BreakdownHeader({
  title,
  subtitle,
  compact,
}: {
  title: string
  subtitle: string
  compact: boolean
}) {
  return (
    <div className={cn('mb-3 flex items-start gap-2', !compact && 'sm:mb-4')}>
      <span className='mt-0.5 inline-flex size-6 items-center justify-center rounded-lg bg-amber-100 text-amber-700 shadow-sm dark:bg-amber-500/20 dark:text-amber-300'>
        <TagIcon className='size-3.5' />
      </span>
      <div>
        <div className='text-foreground text-base font-medium'>{title}</div>
        <div className='text-muted-foreground text-xs'>{subtitle}</div>
      </div>
    </div>
  )
}

type MatrixTableProps = {
  matrix: VisualMatrixV2
  symbol: string
  rate: number
  officialRate: number
  compact: boolean
  embedded: boolean
  matchedTierLabel: string | null
  matchedCellKey: string | null
  showComparison: boolean
  t: (key: string) => string
}

function MatrixTable({
  matrix,
  symbol,
  rate,
  officialRate,
  compact,
  embedded,
  matchedTierLabel,
  matchedCellKey,
  showComparison,
  t,
}: MatrixTableProps) {
  // Enumerate the Cartesian product deterministically so cell keys match the
  // generator side.
  const combos = useMemo(() => {
    const step = (dims: MatrixDimension[]): string[][] => {
      if (dims.length === 0) return [[]]
      const [first, ...rest] = dims
      const restCombos = step(rest)
      const out: string[][] = []
      for (const v of first.values) {
        for (const combo of restCombos) {
          out.push([v, ...combo])
        }
      }
      return out
    }
    return step(matrix.dimensions)
  }, [matrix.dimensions])

  const normalizedMatched = normalizeTierLabel(matchedTierLabel ?? undefined)

  return (
    <div>
      <div className={cn('overflow-x-auto', !embedded && 'rounded-md border')}>
        <table
          className={cn(
            'w-full border-collapse',
            compact ? 'text-xs' : 'text-sm'
          )}
        >
          <thead>
            <tr className='bg-muted/50 text-muted-foreground border-b'>
              {matrix.dimensions.map((d) => (
                <th
                  key={d.id}
                  className='truncate px-3 py-2 text-left text-xs font-medium'
                >
                  {translatedDimensionLabel(d, t)}
                </th>
              ))}
              <th className='px-3 py-2 text-left text-xs font-medium'>
                {t('Price')}{' '}
                <span className='text-muted-foreground/70 font-normal'>
                  ({costUnitLabel(matrix.costUnit, t)})
                </span>
              </th>
            </tr>
          </thead>
          <tbody>
            {combos.map((combo) => {
              const key = cellKeyFromCoords(matrix.dimensions, combo)
              const value = matrix.cells[key] ?? 0
              const label = combo.join('_')
              const isMatched = matchedCellKey
                ? key === matchedCellKey
                : normalizedMatched !== '' &&
                  normalizeTierLabel(label) === normalizedMatched
              const unitSuffix = t(matrixUnitSuffixKey(matrix.costUnit))
              return (
                <tr
                  key={key}
                  className={cn(
                    'border-b last:border-b-0',
                    isMatched && 'bg-emerald-50/70 dark:bg-emerald-500/10'
                  )}
                >
                  {combo.map((v, i) => (
                    <td
                      key={matrix.dimensions[i]?.id ?? v}
                      className='truncate px-3 py-2 text-xs'
                    >
                      {v}
                    </td>
                  ))}
                  <td className='px-3 py-2 text-left'>
                    <div className='flex flex-wrap items-center gap-1.5'>
                      {(() => {
                        if (value <= 0) {
                          return <span>-</span>
                        }
                        if (showComparison) {
                          return (
                            <span className='inline-flex flex-col items-end'>
                              <span className='text-muted-foreground/50 font-mono text-[10px] line-through'>
                                {`${symbol}${(value * officialRate).toFixed(4)}`}
                              </span>
                              <span className='font-mono font-semibold text-amber-600 dark:text-amber-400'>
                                {`${symbol}${(value * rate).toFixed(4)}`}
                              </span>
                            </span>
                          )
                        }
                        return (
                          <span className='font-mono font-semibold text-amber-600 dark:text-amber-400'>
                            {`${symbol}${(value * rate).toFixed(4)}`}
                          </span>
                        )
                      })()}
                      {value > 0 && (
                        <span className='text-muted-foreground text-xs'>
                          {unitSuffix}
                        </span>
                      )}
                      {isMatched && (
                        <Badge
                          variant='secondary'
                          className='bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300'
                        >
                          {t('Matched')}
                        </Badge>
                      )}
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

type TierListProps = {
  tiers: VisualTierV2[]
  symbol: string
  officialRate: number
  compact: boolean
  embedded: boolean
  matchedTierLabel: string | null
  showComparison: boolean
  t: (key: string) => string
}

function TierList({
  tiers,
  symbol,
  officialRate,
  compact,
  embedded,
  matchedTierLabel,
  showComparison,
  t,
}: TierListProps) {
  const normalizedMatched = normalizeTierLabel(matchedTierLabel ?? undefined)

  if (tiers.length === 0) return null

  return (
    <div>
      {!embedded && (
        <div
          className={
            compact
              ? 'text-muted-foreground mb-1.5 text-xs font-medium'
              : 'text-foreground mb-2 text-sm font-semibold'
          }
        >
          {t('Tiered price table')}
        </div>
      )}
      {/* Mobile: card layout */}
      <div className={cn('space-y-1.5 sm:hidden', embedded && 'hidden')}>
        {tiers.map((tier) => {
          const isMatched =
            normalizedMatched !== '' &&
            normalizeTierLabel(tier.label) === normalizedMatched
          return (
            <div
              key={`v2-tier-mobile-${tier.label || JSON.stringify(tier.conditions)}`}
              className={cn(
                'rounded-md border p-2',
                isMatched && 'border-emerald-500/40 bg-emerald-500/10'
              )}
            >
              <div className='mb-1.5 flex flex-wrap items-center gap-1.5'>
                <Badge
                  variant='secondary'
                  className='bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-300'
                >
                  {tier.label || t('Default')}
                </Badge>
                {isMatched && (
                  <Badge
                    variant='secondary'
                    className='bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300'
                  >
                    {t('Matched')}
                  </Badge>
                )}
              </div>
              {tier.conditions.length > 0 && (
                <div className='text-muted-foreground mb-1.5 text-xs'>
                  <HumanConditions conditions={tier.conditions} />
                </div>
              )}
              <div className='text-xs'>
          <HumanFormula
            tier={tier}
            currencySymbol={symbol}
            currencyRate={effectiveRate}
            officialCurrencyRate={officialRate}
            showComparison={showComparison}
                  compact
                />
              </div>
            </div>
          )
        })}
      </div>
      {/* Desktop: table layout */}
      <StaticDataTable
        className={cn('rounded-none border-0', !embedded && 'hidden sm:block')}
        tableClassName={
          compact
            ? '[&_td]:text-xs [&_td_*]:text-xs [&_th]:text-xs [&_th_*]:text-xs'
            : 'text-sm'
        }
        headerRowClassName='hover:bg-transparent'
        data={tiers}
        getRowKey={(_tier, index) => `v2-tier-${index}`}
        getRowClassName={(tier) => {
          const isMatched =
            normalizedMatched !== '' &&
            normalizeTierLabel(tier.label) === normalizedMatched
          return cn(
            isMatched &&
              'bg-emerald-50/70 hover:bg-emerald-50/70 dark:bg-emerald-500/10 dark:hover:bg-emerald-500/10'
          )
        }}
        columns={[
          {
            id: 'tier',
            header: t('Tier'),
            className: cn(
              'text-muted-foreground py-2 font-medium',
              compact && 'h-8'
            ),
            cellClassName: cn('align-top', compact ? 'py-2' : 'py-2.5'),
            cell: (tier) => {
              const isMatched =
                normalizedMatched !== '' &&
                normalizeTierLabel(tier.label) === normalizedMatched
              return (
                <div className='flex flex-wrap items-center gap-1.5'>
                  <Badge
                    variant='secondary'
                    className='bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-300'
                  >
                    {tier.label || t('Default')}
                  </Badge>
                  {isMatched && (
                    <Badge
                      variant='secondary'
                      className='bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300'
                    >
                      {t('Matched')}
                    </Badge>
                  )}
                </div>
              )
            },
          },
          {
            id: 'tier-conditions',
            header: t('Tier conditions'),
            className: cn(
              'text-muted-foreground py-2 font-medium',
              compact && 'h-8'
            ),
            cellClassName: cn('align-top', compact ? 'py-2' : 'py-2.5'),
            cell: (tier) =>
              tier.conditions.length > 0 ? (
                <HumanConditions conditions={tier.conditions} />
              ) : (
                '-'
              ),
          },
          {
            id: 'human-formula',
            header: t('Pricing'),
            className: cn(
              'text-muted-foreground py-2 font-medium',
              compact && 'h-8'
            ),
            cellClassName: cn('align-top', compact ? 'py-2' : 'py-2.5'),
            cell: (tier: VisualTierV2) => (
              <HumanFormula
                tier={tier}
                currencySymbol={symbol}
                currencyRate={effectiveRate}
                officialCurrencyRate={officialRate}
                showComparison={showComparison}
                compact={compact}
              />
            ),
          },
        ]}
      />
    </div>
  )
}
