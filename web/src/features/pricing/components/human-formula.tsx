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
  className?: string
  // When true, render a single-line dense form (for tables/cards).
  compact?: boolean
}

// Renders a tier's pricing formula as a human-readable expression, e.g.
// "每次基础 $0.30 + 每秒 $0.05 + 每 1M tokens $7.70". Falls back to "免费"
// when the tier has no non-zero terms. Every amount is scaled by currencyRate
// and prefixed with currencySymbol so it matches the site's currency setting.
export function HumanFormula({
  tier,
  currencySymbol,
  currencyRate,
  className,
  compact = false,
}: HumanFormulaProps) {
  const { t } = useTranslation()
  const terms = tierFormulaTerms(tier)

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
      {terms.map((term, i) => (
        <span key={i} className='inline-flex items-baseline gap-1'>
          {i > 0 && <span className='text-muted-foreground'>+</span>}
          <HumanTerm
            term={term}
            currencySymbol={currencySymbol}
            currencyRate={currencyRate}
            t={t}
            compact={compact}
          />
        </span>
      ))}
    </span>
  )
}

function HumanTerm({
  term,
  currencySymbol,
  currencyRate,
  t,
  compact,
}: {
  term: HumanFormulaTerm
  currencySymbol: string
  currencyRate: number
  t: (key: string) => string
  compact: boolean
}) {
  const amount = `${currencySymbol}${(term.amount * currencyRate).toFixed(4)}`
  switch (term.unitKey) {
    case 'flat':
      return (
        <span>
          <span className='text-muted-foreground'>{t('Per call')}</span>{' '}
          <span className='font-mono font-semibold'>{amount}</span>
        </span>
      )
    case 'per second':
      return (
        <span>
          <span className='font-mono font-semibold'>{amount}</span>
          <span className='text-muted-foreground'> × {t('seconds')}</span>
        </span>
      )
    case 'per n':
      return (
        <span>
          <span className='font-mono font-semibold'>{amount}</span>
          <span className='text-muted-foreground'> × {t('n (count)')}</span>
        </span>
      )
    case 'per 1M tokens':
      return (
        <span>
          <span className='font-mono font-semibold'>{amount}</span>
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
          <span className='font-mono font-semibold'>{amount}</span>
          <span className='text-muted-foreground'>
            {' '}
            × {compact ? label : label}
          </span>
        </span>
      )
    }
  }
}

// Renders a list of tier conditions as human-readable phrases joined by
// "且"/"and". Empty list renders nothing (caller decides whether to show
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
        return (
          <span key={i}>
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
