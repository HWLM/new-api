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
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import {
  conditionHumanForm,
  tierFormulaTerms,
  type HumanFormulaTerm,
  type TaskConditionInputV2,
  type VisualTierV2,
} from '../lib/tier-expr-v2'

type HumanFormulaProps = {
  tier: VisualTierV2
  currencySymbol: string
  currencyRate: number
  officialCurrencyRate?: number
  showComparison?: boolean
  className?: string
  // When true, render a single-line dense form (for tables/cards).
  compact?: boolean
}

// Renders a tier's pricing formula as a human-readable expression, e.g.
// "每次基础 $0.30 + 每秒 $0.05 + 每 1M tokens $7.70". Falls back to "Free (no cost)"
// when the tier has no non-zero terms. Every amount is scaled by currencyRate
// and prefixed with currencySymbol so it matches the site's currency setting.
export function HumanFormula({
  tier,
  currencySymbol,
  currencyRate,
  officialCurrencyRate,
  showComparison = false,
  className,
  compact = false,
}: HumanFormulaProps) {
  const { t } = useTranslation()
  const terms = tierFormulaTerms(tier)
  const comparisonRate =
    officialCurrencyRate != null &&
    Number.isFinite(officialCurrencyRate) &&
    officialCurrencyRate > 0
      ? officialCurrencyRate
      : null

  if (terms.length === 0) {
    return (
      <span className={cn('text-muted-foreground', className)}>
        {t('Free (no cost)')}
      </span>
    )
  }

  return (
    <span
      className={cn(
        'inline-flex flex-wrap items-baseline gap-x-1 gap-y-0.5',
        className
      )}
    >
      {terms.map((term, i) => {
        const termKey = [
          term.unitKey,
          term.variableExpr || '',
          term.variableLabel || '',
          term.amount,
        ].join(':')
        return (
        <span key={termKey} className='inline-flex items-baseline gap-1'>
          {i > 0 && <span className='text-muted-foreground'>+</span>}
          <HumanTerm
            term={term}
            currencySymbol={currencySymbol}
            currencyRate={currencyRate}
            officialCurrencyRate={comparisonRate}
            showComparison={showComparison}
            t={t}
            compact={compact}
          />
        </span>
        )
      })}
    </span>
  )
}

function HumanTerm({
  term,
  currencySymbol,
  currencyRate,
  officialCurrencyRate,
  showComparison,
  t,
  compact,
}: {
  term: HumanFormulaTerm
  currencySymbol: string
  currencyRate: number
  officialCurrencyRate: number | null
  showComparison: boolean
  t: (key: string) => string
  compact: boolean
}) {
  const actualAmount = `${currencySymbol}${(term.amount * currencyRate).toFixed(4)}`
  const officialAmount =
    officialCurrencyRate != null
      ? `${currencySymbol}${(term.amount * officialCurrencyRate).toFixed(4)}`
      : null

  const amountNode =
    showComparison && officialAmount ? (
      <span className='inline-flex flex-col items-end leading-tight'>
        <span className='font-mono text-[10px] text-muted-foreground/50 line-through tabular-nums'>
          {officialAmount}
        </span>
        <span className='font-mono font-semibold tabular-nums'>{actualAmount}</span>
      </span>
    ) : (
      <span className='font-mono font-semibold tabular-nums'>{actualAmount}</span>
    )

  switch (term.unitKey) {
    case 'flat':
      return (
        <span>
          <span className='text-muted-foreground'>{t('Per call')}</span>{' '}
          {amountNode}
        </span>
      )
    case 'per second':
      return (
        <span>
          {amountNode}
          <span className='text-muted-foreground'> 脳 {t('seconds')}</span>
        </span>
      )
    case 'per n':
      return (
        <span>
          {amountNode}
          <span className='text-muted-foreground'> 脳 {t('n (count)')}</span>
        </span>
      )
    case 'per 1M tokens':
      return (
        <span>
          {amountNode}
          <span className='text-muted-foreground'>
            {' '}
            / 1M {t('tokens')}
          </span>
        </span>
      )
    case 'per unit': {
      const label = term.variableLabel || term.variableExpr || ''
      return (
        <span>
          {amountNode}
          <span className='text-muted-foreground'>
            {' '}
            脳 {compact ? label : label}
          </span>
        </span>
      )
    }
  }
}

// Renders a list of tier conditions as human-readable phrases joined by
// "and". Empty list renders nothing (caller decides whether to show
// "Always matches").
export function HumanConditions({
  conditions,
  className,
}: {
  conditions: TaskConditionInputV2[]
  className?: string
}) {
  const { t } = useTranslation()
  if (conditions.length === 0) return null
  return (
    <span className={className}>
      {conditions.map((cond, i) => {
        const h = conditionHumanForm(cond)
        const subject = h.subjectRaw ?? t(h.subjectKey)
        const verb = t(h.verbKey)
        const conditionKey = [h.subjectKey, subject, verb, h.object].join(':')
        return (
          <span key={conditionKey}>
            {i > 0 && <span className='mx-1 text-muted-foreground'>{t('and')}</span>}
            <span className='font-medium'>{subject}</span>{' '}
            <span className='text-muted-foreground'>{verb}</span>
            {h.object !== '' && (
              <>
                {' '}
                <span className='font-mono'>{h.object}</span>
              </>
            )}
          </span>
        )
      })}
    </span>
  )
}
