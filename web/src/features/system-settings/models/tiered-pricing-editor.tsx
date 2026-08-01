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
import { ChevronDown, Copy, Plus, Trash2 } from 'lucide-react'
import {
  memo,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type FocusEvent,
  type InputHTMLAttributes,
  type MouseEvent as ReactMouseEvent,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  HumanConditions,
  HumanFormula,
} from '@/features/pricing/components/human-formula'
import {
  BILLING_EXTRA_VARS,
  COMMON_TIMEZONES,
  MATCH_CONTAINS,
  MATCH_EQ,
  MATCH_EXISTS,
  MATCH_GT,
  MATCH_GTE,
  MATCH_LT,
  MATCH_LTE,
  MATCH_RANGE,
  SOURCE_HEADER,
  SOURCE_PARAM,
  SOURCE_TIME,
  TIME_FUNCS,
  buildRequestRuleExpr,
  combineBillingExpr,
  createEmptyCondition,
  createEmptyRuleGroup,
  createEmptyTimeCondition,
  getRequestRuleMatchOptions,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type ParamHeaderCondition,
  type RequestCondition,
  type RequestRuleGroup,
  type TimeCondition,
  type TimeFunc,
} from '@/features/pricing/lib/billing-expr'
import {
  CACHE_MODE_GENERIC,
  CACHE_MODE_TIMED,
  type CacheMode,
  type ExtraTokenValues,
  type TierConditionInput,
  type VisualConfig,
  type VisualTier,
  createDefaultVisualConfig,
  evalExprLocally,
  exprUsesExtraVars,
  generateExprFromVisualConfig,
  getTierCacheMode,
  normalizeVisualConfig,
  normalizeVisualTier,
  tryParseVisualConfig,
} from '@/features/pricing/lib/tier-expr'
import {
  DOUBAO_SEEDANCE_2_PRICING_EXPR,
  NUMERIC_OPS as V2_NUMERIC_OPS,
  TASK_BOOL_VARS,
  TASK_NUMERIC_VARS,
  TASK_STRING_VARS,
  type MatrixCostUnit,
  type MatrixDimension,
  type MatrixDimensionSource,
  type TaskConditionInputV2,
  type CustomCostComponent,
  type VisualConfigV2,
  type VisualMatrixV2,
  type VisualTierV2,
  cellKeyFromCoords,
  createDefaultEvalInputs,
  createDefaultVisualConfigV2,
  createEmptyMatrix,
  dimensionDisplayLabel,
  evalExprLocallyV2,
  generateExprFromVisualConfigV2,
  isV2Expression,
  matrixToVisualConfigV2,
  newDimensionId,
  normalizeVisualConfigV2,
  normalizeVisualTierV2,
  tryParseMatrixFromVisualConfig,
  tryParseVisualConfigV2,
} from '@/features/pricing/lib/tier-expr-v2'
import { cn } from '@/lib/utils'

const PRICE_SUFFIX = '$/1M tokens'
const CACHE_PRICE_VARS = BILLING_EXTRA_VARS.filter(
  (variable) => variable.group === 'cache'
)
const MEDIA_PRICE_VARS = BILLING_EXTRA_VARS.filter(
  (variable) => variable.group === 'media'
)

const renderKeyMap = new WeakMap<object, string>()
let renderKeyCounter = 0

function getStableRenderKey(value: object): string {
  const existing = renderKeyMap.get(value)
  if (existing) return existing
  renderKeyCounter += 1
  const key = `pricing-row-${renderKeyCounter}`
  renderKeyMap.set(value, key)
  return key
}

const CONDITION_INPUT_OPTIONS: {
  value: TierConditionInput['var']
  labelKey: string
}[] = [
  { value: 'len', labelKey: 'Full input length' },
  { value: 'p', labelKey: 'Billable input tokens' },
  { value: 'c', labelKey: 'Billable output tokens' },
]
const OPS: TierConditionInput['op'][] = ['<', '<=', '>', '>=']

type Preset = {
  key: string
  label: string
  expr: string
  requestRules?: RequestRuleGroup[]
}

type PresetGroup = {
  group: string
  presets: Preset[]
}

const PRESET_GROUPS: PresetGroup[] = [
  {
    group: 'Fixed price',
    presets: [
      { key: 'flat', label: 'Flat', expr: 'tier("base", p * 2 + c * 4)' },
      {
        key: 'claude-opus',
        label: 'Claude Opus 4.6',
        expr: 'tier("base", p * 5 + c * 25 + cr * 0.5 + cc * 6.25 + cc1h * 10)',
      },
      {
        key: 'gpt-5.4',
        label: 'GPT-5.4',
        expr: 'len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.25) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5)',
      },
    ],
  },
  {
    group: 'Tiered',
    presets: [
      {
        key: 'claude-sonnet',
        label: 'Claude Sonnet 4.5',
        expr: 'len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)',
      },
      {
        key: 'qwen3-max',
        label: 'Qwen3 Max',
        expr: 'len <= 32000 ? tier("short", p * 1.2 + c * 6 + cr * 0.24 + cc * 1.5) : len <= 128000 ? tier("mid", p * 2.4 + c * 12 + cr * 0.48 + cc * 3) : tier("long", p * 3 + c * 15 + cr * 0.6 + cc * 3.75)',
      },
      {
        key: 'glm-4.5-air',
        label: 'GLM-4.5 Air',
        expr: 'len < 32000 && c < 200 ? tier("short_output", p * 0.8 + c * 2 + cr * 0.16) : len < 32000 && c >= 200 ? tier("long_output", p * 0.8 + c * 6 + cr * 0.16) : tier("mid_context", p * 1.2 + c * 8 + cr * 0.24)',
      },
      {
        key: 'doubao-seed-1.8',
        label: 'Doubao Seed 1.8',
        expr: 'len <= 32000 && c <= 200 ? tier("discount", p * 0.8 + c * 2 + cr * 0.16 + cc * 0.17) : len <= 32000 ? tier("short", p * 0.8 + c * 8 + cr * 0.16 + cc * 0.17) : len <= 128000 ? tier("mid", p * 1.2 + c * 16 + cr * 0.16 + cc * 0.17) : tier("long", p * 2.4 + c * 24 + cr * 0.16 + cc * 0.17)',
      },
    ],
  },
  {
    group: 'Multimodal',
    presets: [
      {
        key: 'gpt-image-1-mini',
        label: 'GPT Image 1 Mini',
        expr: 'tier("base", p * 2 + c * 8 + img * 2.5)',
      },
      {
        key: 'gemini-2.5-flash',
        label: 'Gemini 2.5 Flash',
        expr: 'tier("base", p * 0.3 + c * 2.5 + cr * 0.03 + ai * 1.0)',
      },
      {
        key: 'gemini-3-pro-image',
        label: 'Gemini 3 Pro Image',
        expr: 'tier("base", p * 2 + c * 12 + img_o * 120)',
      },
      {
        key: 'qwen3-omni-flash',
        label: 'Qwen3 Omni Flash',
        expr: 'tier("base", p * 0.43 + c * 3.06 + img * 0.78 + ai * 3.81 + ao * 15.11)',
      },
    ],
  },
  {
    group: 'Request rule',
    presets: [
      {
        key: 'claude-opus-fast',
        label: 'Claude Opus 4.6 Fast',
        expr: 'tier("base", p * 5 + c * 25 + cr * 0.5 + cc * 6.25 + cc1h * 10)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_HEADER as 'header',
                path: 'anthropic-beta',
                mode: MATCH_CONTAINS,
                value: 'fast-mode-2026-02-01',
              },
            ],
            multiplier: '6',
          },
        ],
      },
      {
        key: 'gpt-5.4-tiers',
        label: 'GPT-5.4 Priority/Flex',
        expr: 'len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.25) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_PARAM as 'param',
                path: 'service_tier',
                mode: MATCH_EQ,
                value: 'priority',
              },
            ],
            multiplier: '2',
          },
          {
            conditions: [
              {
                source: SOURCE_PARAM as 'param',
                path: 'service_tier',
                mode: MATCH_EQ,
                value: 'flex',
              },
            ],
            multiplier: '0.5',
          },
        ],
      },
    ],
  },
  {
    group: 'Video / Task (v2)',
    presets: [
      {
        key: 'video-flat-per-call',
        label: 'Video flat $0.30/call',
        expr: 'v2:tier("flat", 0.30)',
      },
      {
        key: 'video-by-resolution',
        label: 'Video by resolution × seconds',
        expr: 'v2:resolution == "4k" ? tier("4k", 0.15 * seconds) : resolution == "1080p" ? tier("1080p", 0.06 * seconds) : resolution == "720p" ? tier("720p", 0.04 * seconds) : tier("480p", 0.025 * seconds)',
      },
      {
        key: 'video-i2v-surcharge',
        label: 'Video i2v surcharge',
        expr: 'v2:has_video ? tier("i2v_" + resolution, 0.10 + 0.06 * seconds) : resolution == "1080p" ? tier("t2v_1080p", 0.06 * seconds) : tier("t2v_default", 0.04 * seconds)',
      },
      {
        key: 'image-per-count',
        label: 'Image $0.05 × n',
        expr: 'v2:tier("img_" + resolution, 0.05 * n)',
      },
      {
        key: 'doubao-seedance-2-0',
        label: 'Doubao Seedance 2.0',
        // Explicit Cartesian tiers keep this preset editable in matrix view.
        // Seedance requests with an omitted/adaptive resolution are normalized
        // to 720p during pre-consume; settlement overlays the actual resolution.
        expr: DOUBAO_SEEDANCE_2_PRICING_EXPR,
      },
    ],
  },
  {
    group: 'Time-based',
    presets: [
      {
        key: 'night-discount',
        label: 'Night discount (50%)',
        expr: 'tier("base", p * 3 + c * 15)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_TIME as 'time',
                timeFunc: 'hour',
                timezone: 'Asia/Shanghai',
                mode: MATCH_RANGE,
                value: '',
                rangeStart: '21',
                rangeEnd: '6',
              },
            ],
            multiplier: '0.5',
          },
        ],
      },
      {
        key: 'weekend-discount',
        label: 'Weekend discount (80%)',
        expr: 'tier("base", p * 3 + c * 15)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_TIME as 'time',
                timeFunc: 'weekday',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '0',
                rangeStart: '',
                rangeEnd: '',
              },
            ],
            multiplier: '0.8',
          },
          {
            conditions: [
              {
                source: SOURCE_TIME as 'time',
                timeFunc: 'weekday',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '6',
                rangeStart: '',
                rangeEnd: '',
              },
            ],
            multiplier: '0.8',
          },
        ],
      },
    ],
  },
]

function unitCostToPrice(uc: number | string): number {
  return Number(uc) || 0
}

function priceToUnitCost(price: number | string): number {
  return Number(price) || 0
}

function formatTokenHint(n: number | string | null | undefined): string {
  if (n == null || n === '' || Number.isNaN(Number(n))) return ''
  const v = Number(n)
  if (v === 0) return '= 0'
  if (v >= 1_000_000) return `= ${(v / 1_000_000).toLocaleString()}M tokens`
  if (v >= 1_000) return `= ${(v / 1_000).toLocaleString()}K tokens`
  return `= ${v.toLocaleString()} tokens`
}

function formatNumberDraft(value: number | string): string {
  if (value === '') return ''
  if (typeof value === 'number') {
    return Number.isFinite(value) ? String(value) : '0'
  }
  return value
}

function parseNumberDraft(value: string): number {
  if (value.trim() === '') return 0
  const next = Number(value)
  return Number.isFinite(next) ? next : 0
}

function isZeroDraft(value: string): boolean {
  return value.trim() !== '' && parseNumberDraft(value) === 0
}

type DraftNumberInputProps = Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'type' | 'value' | 'onChange'
> & {
  value: number | string
  onValueChange: (next: number) => void
  selectZeroOnFocus?: boolean
}

function DraftNumberInput({
  value,
  onValueChange,
  selectZeroOnFocus = true,
  onBlur,
  onFocus,
  onMouseUp,
  ...props
}: DraftNumberInputProps) {
  const [draft, setDraft] = useState(() => formatNumberDraft(value))
  const [focused, setFocused] = useState(false)

  useEffect(() => {
    if (!focused) {
      setDraft(formatNumberDraft(value))
    }
  }, [focused, value])

  const handleChange = (event: ChangeEvent<HTMLInputElement>) => {
    const nextDraft = event.target.value
    setDraft(nextDraft)
    onValueChange(parseNumberDraft(nextDraft))
  }

  const handleFocus = (event: FocusEvent<HTMLInputElement>) => {
    setFocused(true)
    onFocus?.(event)
    if (selectZeroOnFocus && isZeroDraft(event.currentTarget.value)) {
      event.currentTarget.select()
    }
  }

  const handleMouseUp = (event: ReactMouseEvent<HTMLInputElement>) => {
    onMouseUp?.(event)
    if (selectZeroOnFocus && isZeroDraft(event.currentTarget.value)) {
      event.preventDefault()
      event.currentTarget.select()
    }
  }

  const handleBlur = (event: FocusEvent<HTMLInputElement>) => {
    const normalized = parseNumberDraft(event.currentTarget.value)
    setFocused(false)
    setDraft(String(normalized))
    onValueChange(normalized)
    onBlur?.(event)
  }

  return (
    <Input
      {...props}
      type='number'
      value={draft}
      onChange={handleChange}
      onFocus={handleFocus}
      onMouseUp={handleMouseUp}
      onBlur={handleBlur}
    />
  )
}

// ---------------------------------------------------------------------------
// Tier condition row
// ---------------------------------------------------------------------------

type ConditionRowProps = {
  condition: TierConditionInput
  onChange: (next: TierConditionInput) => void
  onRemove: () => void
}

function ConditionRow({ condition, onChange, onRemove }: ConditionRowProps) {
  const { t } = useTranslation()
  const currentInputOption = CONDITION_INPUT_OPTIONS.find(
    (option) => option.value === condition.var
  )

  return (
    <div className='flex items-center gap-2'>
      <Select
        items={CONDITION_INPUT_OPTIONS.map((option) => ({
          value: option.value,
          label: t(option.labelKey),
        }))}
        value={condition.var}
        onValueChange={(value) =>
          onChange({ ...condition, var: value as TierConditionInput['var'] })
        }
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>
            {currentInputOption
              ? t(currentInputOption.labelKey)
              : condition.var}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {CONDITION_INPUT_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.labelKey)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={OPS.map((op) => ({ value: op, label: op }))}
        value={condition.op}
        onValueChange={(value) =>
          onChange({ ...condition, op: value as TierConditionInput['op'] })
        }
      >
        <SelectTrigger className='w-20' size='sm'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {OPS.map((op) => (
              <SelectItem key={op} value={op}>
                {op}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <DraftNumberInput
        min={0}
        value={condition.value}
        onValueChange={(value) => onChange({ ...condition, value })}
        placeholder='tokens'
        className='w-32'
      />
      <span className='text-muted-foreground text-xs'>
        {formatTokenHint(condition.value)}
      </span>
      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
        aria-label='remove'
        className='ml-auto'
      >
        <Trash2 className='text-destructive h-4 w-4' />
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Price input field
// ---------------------------------------------------------------------------

type PriceFieldProps = {
  label: string
  hint?: string
  value: number
  onChange: (next: number) => void
}

function PriceField({ label, hint, value, onChange }: PriceFieldProps) {
  return (
    <div className='w-36 space-y-0.5'>
      <Label className='text-muted-foreground text-xs'>{label}</Label>
      <DraftNumberInput
        min={0}
        step={0.000001}
        value={Number.isFinite(value) ? value : 0}
        onValueChange={onChange}
        className='h-8 w-full'
      />
      {hint && <p className='text-muted-foreground text-xs'>{hint}</p>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Single tier card (visual editor)
// ---------------------------------------------------------------------------

type VisualTierCardProps = {
  tier: VisualTier
  index: number
  total: number
  onChange: (next: VisualTier) => void
  onRemove: () => void
  onAddCondition: () => void
}

function VisualTierCard({
  tier,
  index,
  total,
  onChange,
  onRemove,
  onAddCondition,
}: VisualTierCardProps) {
  const { t } = useTranslation()
  const cacheMode = getTierCacheMode(tier)

  const handleConditionChange = (
    conditionIndex: number,
    next: TierConditionInput
  ) => {
    const conditions = [...tier.conditions]
    conditions[conditionIndex] = next
    onChange({ ...tier, conditions })
  }

  const handleConditionRemove = (conditionIndex: number) => {
    onChange({
      ...tier,
      conditions: tier.conditions.filter((_, i) => i !== conditionIndex),
    })
  }

  const handlePriceChange = (field: keyof VisualTier, value: number) => {
    onChange({ ...tier, [field]: value })
  }

  const handleCacheModeChange = (mode: CacheMode) => {
    onChange({
      ...tier,
      cache_mode: mode,
      cache_create_1h_unit_cost:
        mode === CACHE_MODE_TIMED ? (tier.cache_create_1h_unit_cost ?? 0) : 0,
    })
  }

  const inputUnitPrice = unitCostToPrice(tier.input_unit_cost)
  const outputUnitPrice = unitCostToPrice(tier.output_unit_cost)
  const hasMediaPricing = MEDIA_PRICE_VARS.some((variable) => {
    const fieldKey = variable.tierField as keyof VisualTier
    return unitCostToPrice((tier[fieldKey] as number | undefined) ?? 0) > 0
  })
  const [mediaOpen, setMediaOpen] = useState(hasMediaPricing)

  useEffect(() => {
    if (hasMediaPricing) setMediaOpen(true)
  }, [hasMediaPricing])

  const renderPriceVariable = (
    variable: (typeof BILLING_EXTRA_VARS)[number]
  ) => {
    const fieldKey = variable.tierField as keyof VisualTier
    const value = unitCostToPrice((tier[fieldKey] as number | undefined) ?? 0)

    return (
      <PriceField
        key={variable.key}
        label={t(variable.label)}
        value={value}
        onChange={(next) => handlePriceChange(fieldKey, priceToUnitCost(next))}
      />
    )
  }

  return (
    <div className='space-y-3 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Badge variant='outline'>
            {t('Tier')} {index + 1} / {total}
          </Badge>
          {tier.conditions.length === 0 && (
            <Badge variant='secondary'>{t('Fallback tier')}</Badge>
          )}
          <Input
            value={tier.label}
            onChange={(event) =>
              onChange({ ...tier, label: event.target.value })
            }
            placeholder={t('Tier name')}
            className='h-7 w-36'
          />
        </div>
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          disabled={total <= 1}
          aria-label={t('Remove tier')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      {/* Conditions */}
      <div className='space-y-1.5'>
        <div className='flex h-7 items-center justify-between'>
          <Label className='text-xs font-medium'>{t('Tier conditions')}</Label>
          <Button
            variant='ghost'
            size='sm'
            onClick={onAddCondition}
            disabled={tier.conditions.length >= 2}
            className='h-7 px-2 text-xs'
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add condition')}
          </Button>
        </div>
        {tier.conditions.length === 0 ? (
          <p className='text-muted-foreground text-xs'>
            {t('Always matches (default tier).')}
          </p>
        ) : (
          tier.conditions.map((condition, conditionIndex) => (
            <ConditionRow
              key={getStableRenderKey(condition)}
              condition={condition}
              onChange={(next) => handleConditionChange(conditionIndex, next)}
              onRemove={() => handleConditionRemove(conditionIndex)}
            />
          ))
        )}
      </div>

      <div className='space-y-2'>
        <div className='flex items-center justify-between gap-3'>
          <Label className='text-sm font-semibold'>{t('Token prices')}</Label>
          <span className='bg-muted text-muted-foreground rounded-md px-2 py-1 text-xs'>
            {PRICE_SUFFIX}
          </span>
        </div>

        <div className='space-y-3'>
          <div className='flex flex-wrap gap-x-4 gap-y-2'>
            <PriceField
              label={t('Input price')}
              value={inputUnitPrice}
              onChange={(value) =>
                handlePriceChange('input_unit_cost', priceToUnitCost(value))
              }
            />
            <PriceField
              label={t('Output price')}
              value={outputUnitPrice}
              onChange={(value) =>
                handlePriceChange('output_unit_cost', priceToUnitCost(value))
              }
            />
          </div>

          <div className='space-y-2'>
            <div className='flex h-7 items-center'>
              <Tabs
                value={cacheMode}
                onValueChange={(value) =>
                  value !== null && handleCacheModeChange(value as CacheMode)
                }
              >
                <TabsList className='h-8'>
                  <TabsTrigger
                    value={CACHE_MODE_GENERIC}
                    className='px-2 text-xs'
                  >
                    {t('Generic cache')}
                  </TabsTrigger>
                  <TabsTrigger
                    value={CACHE_MODE_TIMED}
                    className='px-2 text-xs'
                  >
                    {t('Time-sliced cache (Claude)')}
                  </TabsTrigger>
                </TabsList>
              </Tabs>
            </div>
            <div className='flex flex-wrap gap-x-4 gap-y-2'>
              {CACHE_PRICE_VARS.map((variable) => {
                if (variable.key === 'cc1h' && cacheMode !== CACHE_MODE_TIMED) {
                  return null
                }
                return renderPriceVariable(variable)
              })}
            </div>
          </div>
        </div>
      </div>

      {/* Media prices */}
      <div className='space-y-1.5'>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='h-7 px-2 text-xs'
          onClick={() => setMediaOpen((prev) => !prev)}
        >
          <ChevronDown
            className={cn(
              'mr-1 h-3 w-3 transition-transform',
              mediaOpen && 'rotate-180'
            )}
          />
          {t('Media pricing')}
        </Button>
        {mediaOpen && (
          <div className='flex flex-wrap gap-x-4 gap-y-2'>
            {MEDIA_PRICE_VARS.map(renderPriceVariable)}
          </div>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Visual editor (list of tiers)
// ---------------------------------------------------------------------------

type VisualEditorProps = {
  visualConfig: VisualConfig | null
  onChange: (next: VisualConfig) => void
}

function VisualEditor({ visualConfig, onChange }: VisualEditorProps) {
  const { t } = useTranslation()
  const config = useMemo(
    () => normalizeVisualConfig(visualConfig),
    [visualConfig]
  )

  const handleTierChange = (index: number, next: VisualTier) => {
    const tiers = [...config.tiers]
    tiers[index] = normalizeVisualTier(next)
    onChange({ ...config, tiers })
  }

  const handleAddTier = () => {
    const tiers = [...config.tiers]
    const lastIndex = tiers.length - 1
    // When adding a new fallback, give the previous catch-all tier a default
    // upper-bound condition so the expression compiles into a sane two-tier
    // shape. Mirrors the classic editor's UX for adding tiers.
    if (lastIndex >= 0 && tiers[lastIndex].conditions.length === 0) {
      tiers[lastIndex] = normalizeVisualTier({
        ...tiers[lastIndex],
        conditions: [{ var: 'len', op: '<', value: 200000 }],
      })
    }
    tiers.push(
      normalizeVisualTier({
        label: `tier_${tiers.length + 1}`,
        conditions: [],
        input_unit_cost: 0,
        output_unit_cost: 0,
      })
    )
    onChange({ ...config, tiers })
  }

  const handleRemoveTier = (index: number) => {
    const tiers = config.tiers.filter((_, i) => i !== index)
    onChange({ ...config, tiers: tiers.length > 0 ? tiers : config.tiers })
  }

  const handleAddCondition = (index: number) => {
    const tier = config.tiers[index]
    if (tier.conditions.length >= 2) return
    // Prefer `len` (input length) over `p`/`c` for tier conditions because
    // `p` is subject to auto-exclusion when sub-categories like `cr` are
    // priced separately, which can misroute long-input requests into shorter
    // tiers when cache-hits reduce the effective `p`.
    const usedVars = new Set(tier.conditions.map((c) => c.var))
    const nextVar: TierConditionInput['var'] = usedVars.has('len') ? 'c' : 'len'
    onChange({
      ...config,
      tiers: config.tiers.map((current, i) =>
        i === index
          ? {
              ...current,
              conditions: [
                ...tier.conditions,
                { var: nextVar, op: '<', value: 200000 },
              ],
            }
          : current
      ),
    })
  }

  return (
    <div className='space-y-2'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Each tier supports up to 2 conditions. The last tier without conditions is the fallback.'
        )}
      </p>
      {config.tiers.map((tier, index) => (
        <VisualTierCard
          key={getStableRenderKey(tier)}
          tier={tier}
          index={index}
          total={config.tiers.length}
          onChange={(next) => handleTierChange(index, next)}
          onRemove={() => handleRemoveTier(index)}
          onAddCondition={() => handleAddCondition(index)}
        />
      ))}
      <Button
        variant='outline'
        size='sm'
        className='h-9 w-36 justify-center'
        onClick={handleAddTier}
      >
        <Plus className='mr-2 h-4 w-4' />
        {t('Add tier')}
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Task (v2) visual editor — tier list for video / image / audio task models.
// Body shape per tier: flat + per_second * seconds + per_n * n.
// Conditions support: resolution/size/mode (string ==), has_video/has_image
// (bool), seconds/n (numeric compare). See tier-expr-v2.ts.
// ---------------------------------------------------------------------------

type TaskConditionKind = TaskConditionInputV2['kind']

const TASK_CONDITION_OPTIONS: {
  value: string
  kind: TaskConditionKind
  varName: string
  labelKey: string
}[] = [
  ...TASK_STRING_VARS.map((v) => ({
    value: `string:${v}`,
    kind: 'string_eq' as const,
    varName: v,
    labelKey: `Task var: ${v} (string)`,
  })),
  ...TASK_BOOL_VARS.map((v) => ({
    value: `bool:${v}`,
    kind: 'bool' as const,
    varName: v,
    labelKey: `Task var: ${v} (yes/no)`,
  })),
  ...TASK_NUMERIC_VARS.map((v) => ({
    value: `num:${v}`,
    kind: 'numeric' as const,
    varName: v,
    labelKey: `Task var: ${v} (number)`,
  })),
]

const RESOLUTION_SUGGESTIONS = ['480p', '720p', '1080p', '4k']

function makeDefaultCondition(
  kind: TaskConditionKind,
  varName: string
): TaskConditionInputV2 {
  if (kind === 'string_eq') {
    return {
      kind: 'string_eq',
      var: varName as TaskConditionInputV2 extends {
        kind: 'string_eq'
        var: infer V
      }
        ? V
        : never,
      value: varName === 'resolution' ? '1080p' : '',
    }
  }
  if (kind === 'bool') {
    return {
      kind: 'bool',
      var: varName as TaskConditionInputV2 extends {
        kind: 'bool'
        var: infer V
      }
        ? V
        : never,
      value: true,
    }
  }
  return {
    kind: 'numeric',
    var: varName as TaskConditionInputV2 extends {
      kind: 'numeric'
      var: infer V
    }
      ? V
      : never,
    op: '>=',
    value: 5,
  }
}

type TaskConditionRowProps = {
  condition: TaskConditionInputV2
  onChange: (next: TaskConditionInputV2) => void
  onRemove: () => void
}

function TaskConditionRow({
  condition,
  onChange,
  onRemove,
}: TaskConditionRowProps) {
  const { t } = useTranslation()
  // param_string_eq / param_bool conditions are only produced by the matrix
  // editor; the list-view condition picker only handles the v2 first-class
  // vars, so we defensively fall through those variants without a var lookup.
  let selectedKey = ''
  if (condition.kind === 'string_eq') {
    selectedKey = `string:${condition.var}`
  } else if (condition.kind === 'bool') {
    selectedKey = `bool:${condition.var}`
  } else if (condition.kind === 'numeric') {
    selectedKey = `num:${condition.var}`
  }

  const handleVarChange = (nextKey: string | null) => {
    if (!nextKey) return
    const option = TASK_CONDITION_OPTIONS.find((o) => o.value === nextKey)
    if (!option) return
    onChange(makeDefaultCondition(option.kind, option.varName))
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Select
        items={TASK_CONDITION_OPTIONS.map((o) => ({
          value: o.value,
          label: t(o.labelKey),
        }))}
        value={selectedKey}
        onValueChange={handleVarChange}
      >
        <SelectTrigger className='w-56' size='sm'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {TASK_CONDITION_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {t(o.labelKey)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>

      {condition.kind === 'string_eq' && (
        <>
          <span className='text-muted-foreground text-xs'>==</span>
          <Input
            list={
              condition.var === 'resolution'
                ? 'v2-resolution-suggestions'
                : undefined
            }
            value={condition.value}
            onChange={(event) =>
              onChange({ ...condition, value: event.target.value })
            }
            placeholder={condition.var === 'resolution' ? '1080p' : t('value')}
            className='h-8 w-32'
          />
          {condition.var === 'resolution' && (
            <datalist id='v2-resolution-suggestions'>
              {RESOLUTION_SUGGESTIONS.map((r) => (
                <option key={r} value={r} />
              ))}
            </datalist>
          )}
        </>
      )}

      {condition.kind === 'bool' && (
        <Select
          items={[
            { value: 'true', label: t('is true') },
            { value: 'false', label: t('is false') },
          ]}
          value={condition.value ? 'true' : 'false'}
          onValueChange={(v) => onChange({ ...condition, value: v === 'true' })}
        >
          <SelectTrigger className='w-24' size='sm'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='true'>{t('is true')}</SelectItem>
              <SelectItem value='false'>{t('is false')}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      )}

      {condition.kind === 'numeric' && (
        <>
          <Select
            items={V2_NUMERIC_OPS.map((op) => ({ value: op, label: op }))}
            value={condition.op}
            onValueChange={(v) =>
              onChange({
                ...condition,
                op: v as TaskConditionInputV2 extends {
                  kind: 'numeric'
                  op: infer O
                }
                  ? O
                  : never,
              })
            }
          >
            <SelectTrigger className='w-20' size='sm'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {V2_NUMERIC_OPS.map((op) => (
                  <SelectItem key={op} value={op}>
                    {op}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <DraftNumberInput
            min={0}
            value={condition.value}
            onValueChange={(value) => onChange({ ...condition, value })}
            className='h-8 w-24'
          />
        </>
      )}

      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
        aria-label={t('Remove')}
        className='ml-auto'
      >
        <Trash2 className='text-destructive h-4 w-4' />
      </Button>
    </div>
  )
}

type TaskTierCardProps = {
  tier: VisualTierV2
  index: number
  total: number
  hasPreConsumeEstimate: boolean
  onChange: (next: VisualTierV2) => void
  onRemove: () => void
  onAddCondition: () => void
}

function TaskTierCard({
  tier,
  index,
  total,
  hasPreConsumeEstimate,
  onChange,
  onRemove,
  onAddCondition,
}: TaskTierCardProps) {
  const { t } = useTranslation()

  const handlePrice = (field: keyof VisualTierV2, value: number) => {
    onChange({ ...tier, [field]: value })
  }

  return (
    <div className='space-y-3 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Badge variant='outline'>
            {t('Tier')} {index + 1} / {total}
          </Badge>
          {tier.conditions.length === 0 && (
            <Badge variant='secondary'>{t('Fallback tier')}</Badge>
          )}
          <Input
            value={tier.label}
            onChange={(event) =>
              onChange({ ...tier, label: event.target.value })
            }
            placeholder={t('Tier name')}
            className='h-7 w-36'
          />
        </div>
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          disabled={total <= 1}
          aria-label={t('Remove tier')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      {/* Conditions */}
      <div className='space-y-1.5'>
        <div className='flex h-7 items-center justify-between'>
          <Label className='text-xs font-medium'>{t('Tier conditions')}</Label>
          <Button
            variant='ghost'
            size='sm'
            onClick={onAddCondition}
            disabled={tier.conditions.length >= 3}
            className='h-7 px-2 text-xs'
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add condition')}
          </Button>
        </div>
        {tier.conditions.length === 0 ? (
          <p className='text-muted-foreground text-xs'>
            {t('Always matches (default tier).')}
          </p>
        ) : (
          tier.conditions.map((condition, conditionIndex) => (
            <TaskConditionRow
              key={getStableRenderKey(condition)}
              condition={condition}
              onChange={(next) => {
                const conditions = [...tier.conditions]
                conditions[conditionIndex] = next
                onChange({ ...tier, conditions })
              }}
              onRemove={() =>
                onChange({
                  ...tier,
                  conditions: tier.conditions.filter(
                    (_, i) => i !== conditionIndex
                  ),
                })
              }
            />
          ))
        )}
      </div>

      {/* Costs */}
      <div className='space-y-2'>
        <div className='flex items-center justify-between gap-3'>
          <Label className='text-sm font-semibold'>{t('Per-call costs')}</Label>
          <span className='bg-muted text-muted-foreground rounded-md px-2 py-1 text-xs'>
            USD / call
          </span>
        </div>
        <div className='flex flex-wrap gap-x-4 gap-y-2'>
          <PriceField
            label={t('Flat $/call')}
            value={tier.flat_cost}
            onChange={(v) => handlePrice('flat_cost', v)}
          />
          <PriceField
            label={t('$ × seconds')}
            value={tier.per_second_cost}
            onChange={(v) => handlePrice('per_second_cost', v)}
          />
          <PriceField
            label={t('$ × n')}
            value={tier.per_n_cost}
            onChange={(v) => handlePrice('per_n_cost', v)}
          />
          <PriceField
            label={t('$ / 1M tokens')}
            value={tier.per_mtok_cost}
            onChange={(v) => handlePrice('per_mtok_cost', v)}
          />
        </div>
        <CustomCostRows
          tier={tier}
          onChange={(customCosts) => onChange({ ...tier, customCosts })}
        />
        <div className='rounded-md border border-dashed p-2 text-xs'>
          <div className='text-muted-foreground mb-1 font-medium'>
            {t('Preview')}
          </div>
          <HumanFormula tier={tier} currencySymbol='$' currencyRate={1} />
          {tier.conditions.length > 0 && (
            <div className='text-muted-foreground mt-1'>
              {t('Condition')}: <HumanConditions conditions={tier.conditions} />
            </div>
          )}
          <div className='text-muted-foreground/70 mt-1 font-mono text-[10px]'>
            {`tier("${tier.label || 'base'}", ${previewTaskBody(tier)})`}
          </div>
        </div>
        {tier.per_mtok_cost > 0 &&
          !hasPreConsumeEstimate &&
          tier.flat_cost === 0 &&
          tier.per_second_cost === 0 &&
          tier.per_n_cost === 0 &&
          (tier.customCosts || []).length === 0 && (
            <p className='text-xs text-amber-600 dark:text-amber-500'>
              {t(
                'This tier only charges per output token, so upstream failures (timeout / no tokens returned) will bill $0. Add a fixed per-call amount, per-second charge, or a custom cost component above to guarantee some minimum charge.'
              )}
            </p>
          )}
      </div>
    </div>
  )
}

function previewTaskBody(tier: VisualTierV2): string {
  const parts: string[] = []
  if (tier.flat_cost) parts.push(String(tier.flat_cost))
  if (tier.per_second_cost) parts.push(`${tier.per_second_cost} * seconds`)
  if (tier.per_n_cost) parts.push(`${tier.per_n_cost} * n`)
  if (tier.per_mtok_cost) parts.push(`${tier.per_mtok_cost} * p / 1000000`)
  for (const c of tier.customCosts || []) {
    if (c.coef && c.variable) parts.push(`${c.coef} * ${c.variable}`)
  }
  return parts.length > 0 ? parts.join(' + ') : '0'
}

// Custom cost editor: admins add extra (coefficient × variable) terms per tier,
// e.g. `0.001 * param("metadata.frames")` or `0.02 * len`. Variable can be a
// built-in identifier from BUILTIN_CUSTOM_VARS or a param()/header() call
// typed freely.
const BUILTIN_CUSTOM_VARS: { value: string; labelKey: string }[] = [
  { value: 'p', labelKey: 'input tokens' },
  { value: 'c', labelKey: 'output tokens' },
  { value: 'len', labelKey: 'input length' },
  { value: 'cr', labelKey: 'cache-read tokens' },
  { value: 'cc', labelKey: 'cache-write tokens' },
]

function CustomCostRows({
  tier,
  onChange,
}: {
  tier: VisualTierV2
  onChange: (next: CustomCostComponent[]) => void
}) {
  const { t } = useTranslation()
  const rows = tier.customCosts || []

  const updateRow = (i: number, next: Partial<CustomCostComponent>) => {
    const copy = rows.map((r) => ({ ...r }))
    copy[i] = { ...copy[i], ...next }
    onChange(copy)
  }
  const addRow = () => {
    onChange([...rows, { label: '', coef: 0.01, variable: 'p' }])
  }
  const removeRow = (i: number) => {
    onChange(rows.filter((_, idx) => idx !== i))
  }

  return (
    <div className='space-y-1.5'>
      <div className='flex items-center justify-between'>
        <Label className='text-xs font-medium'>
          {t('Custom cost components')}
        </Label>
        <Button
          variant='ghost'
          size='sm'
          onClick={addRow}
          className='h-7 px-2 text-xs'
        >
          <Plus className='mr-1 h-3 w-3' />
          {t('Add component')}
        </Button>
      </div>
      {rows.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Optional. Add "coefficient × variable" terms (e.g. 0.001 × param("metadata.frames")).'
          )}
        </p>
      ) : (
        rows.map((row, i) => (
          <CustomCostRow
            key={getStableRenderKey(row)}
            row={row}
            onChange={(next) => updateRow(i, next)}
            onRemove={() => removeRow(i)}
          />
        ))
      )}
    </div>
  )
}

function CustomCostRow({
  row,
  onChange,
  onRemove,
}: {
  row: CustomCostComponent
  onChange: (next: Partial<CustomCostComponent>) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const isBuiltin = BUILTIN_CUSTOM_VARS.some((v) => v.value === row.variable)
  return (
    <div className='flex flex-wrap items-center gap-1.5 rounded-md border p-1.5'>
      <span className='text-muted-foreground text-xs'>$</span>
      <DraftNumberInput
        min={0}
        step={0.001}
        value={row.coef}
        onValueChange={(next) => onChange({ coef: next })}
        className='h-7 w-20'
      />
      <span className='text-muted-foreground text-xs'>×</span>
      <Select
        value={isBuiltin ? row.variable : '__custom__'}
        onValueChange={(v) => {
          if (v === '__custom__') {
            if (isBuiltin) onChange({ variable: '' })
          } else if (v !== null) {
            onChange({ variable: v })
          }
        }}
      >
        <SelectTrigger className='h-7 w-32 text-xs'>
          <SelectValue placeholder={t('Variable')} />
        </SelectTrigger>
        <SelectContent>
          {BUILTIN_CUSTOM_VARS.map((v) => (
            <SelectItem key={v.value} value={v.value} className='text-xs'>
              {t(v.labelKey)}{' '}
              <span className='text-muted-foreground'>({v.value})</span>
            </SelectItem>
          ))}
          <SelectItem value='__custom__' className='text-xs'>
            {t('Custom expression')}…
          </SelectItem>
        </SelectContent>
      </Select>
      {!isBuiltin && (
        <Input
          value={row.variable}
          onChange={(e) => onChange({ variable: e.target.value })}
          placeholder='param("metadata.frames")'
          className='h-7 w-56 font-mono text-xs'
        />
      )}
      <Input
        value={row.label}
        onChange={(e) => onChange({ label: e.target.value })}
        placeholder={t('Display name (optional)')}
        className='h-7 w-28 text-xs'
      />
      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
        className='h-7 w-7'
        aria-label={t('Remove component')}
      >
        <Trash2 className='text-destructive h-3.5 w-3.5' />
      </Button>
    </div>
  )
}

type TaskVisualEditorProps = {
  visualConfigV2: VisualConfigV2 | null
  onChange: (next: VisualConfigV2) => void
}

// ---------------------------------------------------------------------------
// Task visual editor — switches between List view (tier cards) and Matrix
// view (dimensions + cell price table). The underlying source of truth is
// always VisualConfigV2 (the tier list); matrix view reverse-parses on mount
// and forwards edits back through matrixToVisualConfigV2.
// ---------------------------------------------------------------------------

type TaskVisualViewMode = 'list' | 'matrix'

function TaskVisualEditor({ visualConfigV2, onChange }: TaskVisualEditorProps) {
  const { t } = useTranslation()
  const config = useMemo(
    () => normalizeVisualConfigV2(visualConfigV2),
    [visualConfigV2]
  )
  const usesPerMillionTokenPricing = config.tiers.some(
    (tier) => tier.per_mtok_cost > 0
  )
  const usesAutomaticTokenEstimate = (config.preConsumeTokensPerSecond ?? 0) > 0
  const [viewMode, setViewMode] = useState<TaskVisualViewMode>(() => {
    // Default: matrix view if the current tiers already look like a matrix.
    const cfg = normalizeVisualConfigV2(visualConfigV2)
    return tryParseMatrixFromVisualConfig(cfg) ? 'matrix' : 'list'
  })

  return (
    <div className='space-y-3'>
      {usesPerMillionTokenPricing && (
        <Field className='gap-2'>
          <FieldLabel>{t('Pre-consume estimate')}</FieldLabel>
          {usesAutomaticTokenEstimate ? (
            <Input
              readOnly
              value={`${config.preConsumeTokensPerSecond} × seconds`}
              className='max-w-56 font-mono'
            />
          ) : (
            <DraftNumberInput
              min={0}
              step={1}
              inputMode='numeric'
              value={config.preConsumeTokens ?? 0}
              onValueChange={(value) =>
                onChange({
                  ...config,
                  preConsumeTokens: Math.max(0, Math.round(value)),
                  preConsumeEstimate: 0,
                })
              }
              className='max-w-56'
            />
          )}
          <p className='text-muted-foreground text-xs leading-5'>
            {t(
              'Used before actual usage is available. Final billing is recalculated with the upstream token count.'
            )}
          </p>
          {!usesAutomaticTokenEstimate &&
            (config.preConsumeTokens ?? 0) <= 0 && (
              <Alert variant='destructive'>
                <AlertDescription className='text-xs'>
                  {t(
                    'Enter a positive token estimate to avoid a zero pre-consume for per-1M-token pricing.'
                  )}
                </AlertDescription>
              </Alert>
            )}
        </Field>
      )}

      <div className='flex items-center gap-2'>
        <Label className='text-muted-foreground text-xs'>{t('View')}</Label>
        <Tabs
          value={viewMode}
          onValueChange={(v) =>
            v !== null && setViewMode(v as TaskVisualViewMode)
          }
        >
          <TabsList className='h-8'>
            <TabsTrigger value='list' className='px-3 text-xs'>
              {t('List')}
            </TabsTrigger>
            <TabsTrigger value='matrix' className='px-3 text-xs'>
              {t('Matrix')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {viewMode === 'list' ? (
        <TaskListVisualEditor
          visualConfigV2={visualConfigV2}
          onChange={onChange}
        />
      ) : (
        <TaskMatrixVisualEditor
          visualConfigV2={visualConfigV2}
          onChange={onChange}
        />
      )}
    </div>
  )
}

function TaskListVisualEditor({
  visualConfigV2,
  onChange,
}: TaskVisualEditorProps) {
  const { t } = useTranslation()
  const config = useMemo(
    () => normalizeVisualConfigV2(visualConfigV2),
    [visualConfigV2]
  )

  const handleTierChange = (index: number, next: VisualTierV2) => {
    const tiers = [...config.tiers]
    tiers[index] = normalizeVisualTierV2(next)
    onChange({ ...config, tiers })
  }

  const handleAddTier = () => {
    const tiers = [...config.tiers]
    const lastIndex = tiers.length - 1
    // If the current fallback tier has no conditions, give it a default
    // resolution guard so the chain still ends with a real fallback.
    if (lastIndex >= 0 && tiers[lastIndex].conditions.length === 0) {
      tiers[lastIndex] = normalizeVisualTierV2({
        ...tiers[lastIndex],
        conditions: [{ kind: 'string_eq', var: 'resolution', value: '1080p' }],
      })
    }
    tiers.push(
      normalizeVisualTierV2({
        label: `tier_${tiers.length + 1}`,
        conditions: [],
        flat_cost: 0.1,
        per_second_cost: 0,
        per_n_cost: 0,
        per_mtok_cost: 0,
      })
    )
    onChange({ ...config, tiers })
  }

  const handleRemoveTier = (index: number) => {
    const tiers = config.tiers.filter((_, i) => i !== index)
    onChange({ ...config, tiers: tiers.length > 0 ? tiers : config.tiers })
  }

  const handleAddCondition = (index: number) => {
    const tier = config.tiers[index]
    if (tier.conditions.length >= 3) return
    onChange({
      ...config,
      tiers: config.tiers.map((current, i) =>
        i === index
          ? {
              ...current,
              conditions: [
                ...tier.conditions,
                makeDefaultCondition('string_eq', 'resolution'),
              ],
            }
          : current
      ),
    })
  }

  return (
    <div className='space-y-2'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Video / task pricing: each tier bills a flat $/call plus optional per-second and per-count components. Conditions branch on resolution / seconds / has_video / n. Last unconditioned tier is the fallback. Output expression is prefixed with v2:.'
        )}
      </p>
      {config.tiers.map((tier, index) => (
        <TaskTierCard
          key={getStableRenderKey(tier)}
          tier={tier}
          index={index}
          total={config.tiers.length}
          hasPreConsumeEstimate={
            (config.preConsumeTokensPerSecond ?? 0) > 0 ||
            (config.preConsumeTokens ?? 0) > 0 ||
            (config.preConsumeEstimate ?? 0) > 0
          }
          onChange={(next) => handleTierChange(index, next)}
          onRemove={() => handleRemoveTier(index)}
          onAddCondition={() => handleAddCondition(index)}
        />
      ))}
      <Button
        variant='outline'
        size='sm'
        className='h-9 w-36 justify-center'
        onClick={handleAddTier}
      >
        <Plus className='mr-2 h-4 w-4' />
        {t('Add tier')}
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Task matrix visual editor — dimension chips + cell price table. The matrix
// state is local; every edit regenerates the underlying VisualConfigV2 via
// matrixToVisualConfigV2 and forwards it up so the expression stays in sync.
// ---------------------------------------------------------------------------

const V2_DIM_SOURCE_OPTIONS: {
  value: string
  labelKey: string
  build: (path?: string) => MatrixDimensionSource
  defaultValues: string[]
}[] = [
  ...TASK_STRING_VARS.map((v) => ({
    value: `v2_string:${v}`,
    labelKey: `${v} (string)`,
    build: () => ({ kind: 'v2_string' as const, var: v }),
    defaultValues: v === 'resolution' ? ['480p', '720p', '1080p', '4k'] : [],
  })),
  ...TASK_BOOL_VARS.map((v) => ({
    value: `v2_bool:${v}`,
    labelKey: `${v} (yes/no)`,
    build: () => ({ kind: 'v2_bool' as const, var: v }),
    defaultValues: ['true', 'false'],
  })),
  {
    value: 'param_string',
    labelKey: 'Custom param (string)',
    build: (path?: string) => ({
      kind: 'param_string' as const,
      path: path ?? '',
    }),
    defaultValues: [],
  },
  {
    value: 'param_bool',
    labelKey: 'Custom param (yes/no)',
    build: (path?: string) => ({
      kind: 'param_bool' as const,
      path: path ?? '',
    }),
    defaultValues: ['true', 'false'],
  },
]

function dimSourceKey(source: MatrixDimensionSource): string {
  switch (source.kind) {
    case 'v2_string':
      return `v2_string:${source.var}`
    case 'v2_bool':
      return `v2_bool:${source.var}`
    case 'param_string':
      return 'param_string'
    case 'param_bool':
      return 'param_bool'
  }
}

function TaskMatrixVisualEditor({
  visualConfigV2,
  onChange,
}: TaskVisualEditorProps) {
  const { t } = useTranslation()

  // Matrix state is local to this component. On mount we try to reverse-parse
  // the current tier list; if the tiers form a valid matrix (Cartesian
  // product across the same dimensions), we adopt it. Otherwise we start
  // empty and let the user configure dimensions from scratch.
  const [matrix, setMatrix] = useState<VisualMatrixV2>(() => {
    const cfg = normalizeVisualConfigV2(visualConfigV2)
    const parsed = tryParseMatrixFromVisualConfig(cfg)
    if (parsed) {
      return {
        ...parsed,
        dimensions: parsed.dimensions.map((d) => ({
          ...d,
          id: d.id || newDimensionId(),
        })),
      }
    }
    return createEmptyMatrix()
  })

  // Forward every change up to the parent as a regenerated VisualConfigV2.
  // Always preserve the incoming preConsumeEstimate from the parent so the
  // shared PreConsumeEstimateRow (rendered above this editor) survives matrix
  // edits — the matrix state itself doesn't track it.
  const applyMatrix = (next: VisualMatrixV2) => {
    setMatrix(next)
    const parentCfg = normalizeVisualConfigV2(visualConfigV2)
    const generated = matrixToVisualConfigV2(next)
    onChange({
      ...generated,
      preConsumeTokensPerSecond: parentCfg.preConsumeTokensPerSecond,
      preConsumeTokens: parentCfg.preConsumeTokens,
      preConsumeEstimate: parentCfg.preConsumeEstimate,
    })
  }

  const handleAddDimension = () => {
    // Pick a source that's not already used, defaulting to resolution.
    const usedKeys = new Set(
      matrix.dimensions.map((d) => dimSourceKey(d.source))
    )
    const nextOption =
      V2_DIM_SOURCE_OPTIONS.find((o) => !usedKeys.has(o.value)) ??
      V2_DIM_SOURCE_OPTIONS[0]
    applyMatrix({
      ...matrix,
      dimensions: [
        ...matrix.dimensions,
        {
          id: newDimensionId(),
          source: nextOption.build(),
          values: [...nextOption.defaultValues],
        },
      ],
    })
  }

  const handleRemoveDimension = (id: string) => {
    applyMatrix({
      ...matrix,
      dimensions: matrix.dimensions.filter((d) => d.id !== id),
      cells: {}, // clear cells so stale keys don't leak in
    })
  }

  const handleDimensionSourceChange = (id: string, nextKey: string) => {
    const option = V2_DIM_SOURCE_OPTIONS.find((o) => o.value === nextKey)
    if (!option) return
    applyMatrix({
      ...matrix,
      dimensions: matrix.dimensions.map((d) =>
        d.id === id
          ? {
              ...d,
              source: option.build(),
              values:
                option.defaultValues.length > 0
                  ? [...option.defaultValues]
                  : d.values,
            }
          : d
      ),
      cells: {},
    })
  }

  const handleParamPathChange = (id: string, path: string) => {
    applyMatrix({
      ...matrix,
      dimensions: matrix.dimensions.map((d) => {
        if (d.id !== id) return d
        if (
          d.source.kind === 'param_string' ||
          d.source.kind === 'param_bool'
        ) {
          return { ...d, source: { ...d.source, path } }
        }
        return d
      }),
      cells: {},
    })
  }

  const handleAddDimensionValue = (id: string, value: string) => {
    const trimmed = value.trim()
    if (!trimmed) return
    applyMatrix({
      ...matrix,
      dimensions: matrix.dimensions.map((d) =>
        d.id === id && !d.values.includes(trimmed)
          ? { ...d, values: [...d.values, trimmed] }
          : d
      ),
    })
  }

  const handleRemoveDimensionValue = (id: string, value: string) => {
    applyMatrix({
      ...matrix,
      dimensions: matrix.dimensions.map((d) =>
        d.id === id ? { ...d, values: d.values.filter((v) => v !== value) } : d
      ),
    })
  }

  const handleCostUnitChange = (unit: MatrixCostUnit) => {
    // Preserve numeric values across unit change — semantics differ but the
    // admin is explicitly picking so we don't lose their input.
    applyMatrix({ ...matrix, costUnit: unit })
  }

  const handleCellChange = (key: string, value: number) => {
    applyMatrix({
      ...matrix,
      cells: { ...matrix.cells, [key]: value },
    })
  }

  const validDimensions = matrix.dimensions.filter((d) => {
    if (d.values.length === 0) return false
    if (
      (d.source.kind === 'param_string' || d.source.kind === 'param_bool') &&
      d.source.path.trim() === ''
    ) {
      return false
    }
    return true
  })

  return (
    <div className='space-y-4'>
      <div className='space-y-2'>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Matrix view generates one tier per (dim × dim × …) combination. Add up to 4 dimensions; each dimension has an enumerated value list. Fill the cell prices below to complete the matrix. Every edit regenerates the underlying v2 expression.'
          )}
        </p>
      </div>

      {/* Dimensions */}
      <div className='space-y-2 rounded-lg border p-3'>
        <div className='flex items-center justify-between'>
          <Label className='text-sm font-semibold'>{t('Dimensions')}</Label>
          <Button
            variant='outline'
            size='sm'
            className='h-8 text-xs'
            onClick={handleAddDimension}
            disabled={matrix.dimensions.length >= 4}
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add dimension')}
          </Button>
        </div>
        {matrix.dimensions.length === 0 ? (
          <p className='text-muted-foreground text-xs'>
            {t('No dimensions yet. Add one to build a price matrix.')}
          </p>
        ) : (
          matrix.dimensions.map((dim) => (
            <MatrixDimensionRow
              key={dim.id}
              dimension={dim}
              onSourceChange={(nextKey) =>
                handleDimensionSourceChange(dim.id, nextKey)
              }
              onPathChange={(path) => handleParamPathChange(dim.id, path)}
              onAddValue={(v) => handleAddDimensionValue(dim.id, v)}
              onRemoveValue={(v) => handleRemoveDimensionValue(dim.id, v)}
              onRemove={() => handleRemoveDimension(dim.id)}
            />
          ))
        )}
      </div>

      {/* Cost unit */}
      <div className='flex items-center gap-3'>
        <Label className='text-sm font-medium'>{t('Cost unit')}</Label>
        <Select
          items={[
            { value: 'flat', label: t('Flat $/call') },
            { value: 'per_second', label: t('$ × seconds') },
            { value: 'per_n', label: t('$ × n') },
            { value: 'per_mtok', label: t('$ / 1M tokens') },
          ]}
          value={matrix.costUnit}
          onValueChange={(v) =>
            v !== null && handleCostUnitChange(v as MatrixCostUnit)
          }
        >
          <SelectTrigger className='w-48' size='sm'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='flat'>{t('Flat $/call')}</SelectItem>
              <SelectItem value='per_second'>{t('$ × seconds')}</SelectItem>
              <SelectItem value='per_n'>{t('$ × n')}</SelectItem>
              <SelectItem value='per_mtok'>{t('$ / 1M tokens')}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      {/* Cell price table */}
      {validDimensions.length === 0 ? (
        <Alert>
          <AlertDescription className='text-xs'>
            {t(
              'Add at least one dimension with values to see the price cells below.'
            )}
          </AlertDescription>
        </Alert>
      ) : (
        <MatrixCellTable
          dimensions={validDimensions}
          cells={matrix.cells}
          onCellChange={handleCellChange}
        />
      )}
    </div>
  )
}

type MatrixDimensionRowProps = {
  dimension: MatrixDimension
  onSourceChange: (nextKey: string) => void
  onPathChange: (path: string) => void
  onAddValue: (value: string) => void
  onRemoveValue: (value: string) => void
  onRemove: () => void
}

function MatrixDimensionRow({
  dimension,
  onSourceChange,
  onPathChange,
  onAddValue,
  onRemoveValue,
  onRemove,
}: MatrixDimensionRowProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState('')
  const isParam =
    dimension.source.kind === 'param_string' ||
    dimension.source.kind === 'param_bool'
  const isBool =
    dimension.source.kind === 'v2_bool' ||
    dimension.source.kind === 'param_bool'

  const handleAddDraft = () => {
    if (!draft.trim()) return
    onAddValue(draft.trim())
    setDraft('')
  }

  return (
    <div className='space-y-2 rounded-md border p-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <Badge variant='outline' className='shrink-0'>
          {dimensionDisplayLabel(dimension)}
        </Badge>
        <Select
          items={V2_DIM_SOURCE_OPTIONS.map((o) => ({
            value: o.value,
            label: t(o.labelKey),
          }))}
          value={dimSourceKey(dimension.source)}
          onValueChange={(v) => v !== null && onSourceChange(v)}
        >
          <SelectTrigger className='w-48' size='sm'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {V2_DIM_SOURCE_OPTIONS.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {t(o.labelKey)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        {isParam && (
          <Input
            value={(dimension.source as { path: string }).path || ''}
            onChange={(event) => onPathChange(event.target.value)}
            placeholder='metadata.quality'
            className='h-8 w-48 font-mono text-xs'
          />
        )}
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          aria-label={t('Remove dimension')}
          className='ml-auto'
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      <div className='flex flex-wrap items-center gap-1.5'>
        {dimension.values.map((v) => (
          <Badge
            key={v}
            variant='secondary'
            className='gap-1 pr-1 pl-2 text-xs'
          >
            {v}
            {!isBool && (
              <button
                type='button'
                onClick={() => onRemoveValue(v)}
                aria-label={t('Remove value')}
                className='text-muted-foreground hover:text-destructive'
              >
                <Trash2 className='h-3 w-3' />
              </button>
            )}
          </Badge>
        ))}
        {!isBool && (
          <div className='flex items-center gap-1'>
            <Input
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  handleAddDraft()
                }
              }}
              placeholder={t('Add value…')}
              className='h-7 w-32 text-xs'
            />
            <Button
              variant='ghost'
              size='sm'
              onClick={handleAddDraft}
              className='h-7 px-2 text-xs'
              disabled={!draft.trim()}
            >
              {t('Add')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

type MatrixCellTableProps = {
  dimensions: MatrixDimension[]
  cells: Record<string, number>
  onCellChange: (key: string, value: number) => void
}

function MatrixCellTable({
  dimensions,
  cells,
  onCellChange,
}: MatrixCellTableProps) {
  const { t } = useTranslation()
  // Cartesian product enumeration for rendering — mirrors the generator so
  // the visual table matches the produced tier list 1:1.
  const combos: string[][] = useMemo(() => {
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
    return step(dimensions)
  }, [dimensions])

  return (
    <div className='rounded-lg border'>
      <div
        className='bg-muted/50 grid gap-2 border-b p-2 text-xs font-medium'
        style={{
          gridTemplateColumns: `repeat(${dimensions.length}, minmax(90px, 1fr)) minmax(140px, 200px)`,
        }}
      >
        {dimensions.map((d) => (
          <div key={d.id} className='truncate'>
            {dimensionDisplayLabel(d)}
          </div>
        ))}
        <div className='text-right'>{t('Price')}</div>
      </div>
      <div className='divide-y'>
        {combos.map((combo) => {
          const key = cellKeyFromCoords(dimensions, combo)
          const value = cells[key] ?? 0
          return (
            <div
              key={key}
              className='grid items-center gap-2 p-2'
              style={{
                gridTemplateColumns: `repeat(${dimensions.length}, minmax(90px, 1fr)) minmax(140px, 200px)`,
              }}
            >
              {combo.map((v, i) => (
                <span key={dimensions[i].id} className='truncate text-xs'>
                  {v}
                </span>
              ))}
              <div className='flex justify-end'>
                <DraftNumberInput
                  min={0}
                  step={0.0001}
                  value={value}
                  onValueChange={(next) => onCellChange(key, next)}
                  className='h-8 w-32 text-right'
                />
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Raw expression editor
// ---------------------------------------------------------------------------

type RawExprEditorProps = {
  exprString: string
  onChange: (value: string) => void
}

function RawExprEditor({ exprString, onChange }: RawExprEditorProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-3'>
      <Alert>
        <AlertDescription className='space-y-1 text-xs'>
          <div>
            {t('Variables')}: <code>len</code>, <code>p</code>, <code>c</code>,{' '}
            <code>cr</code>, <code>cc</code>, <code>cc1h</code>,{' '}
            <code>img</code>, <code>img_o</code>, <code>ai</code>,{' '}
            <code>ao</code>
          </div>
          <div>
            {t('Functions')}: <code>tier(name, value)</code>, <code>max</code>,{' '}
            <code>min</code>, <code>ceil</code>, <code>floor</code>,{' '}
            <code>abs</code>, <code>header(name)</code>,{' '}
            <code>param(path)</code>, <code>has(source, text)</code>
          </div>
        </AlertDescription>
      </Alert>
      <Textarea
        value={exprString}
        onChange={(event) => onChange(event.target.value)}
        placeholder='tier("base", p * 3 + c * 15)'
        rows={6}
        className='font-mono text-xs'
        spellCheck={false}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Request rule condition row
// ---------------------------------------------------------------------------

type RuleConditionRowProps = {
  condition: RequestCondition
  onChange: (next: RequestCondition) => void
  onRemove: () => void
}

function RuleConditionRow({
  condition,
  onChange,
  onRemove,
}: RuleConditionRowProps) {
  const { t } = useTranslation()
  const matchOptions = getRequestRuleMatchOptions(condition.source)
  const getMatchLabel = (mode: string) => {
    switch (mode) {
      case MATCH_EQ:
        return t('Equals')
      case MATCH_CONTAINS:
        return t('Contains')
      case MATCH_EXISTS:
        return t('Exists')
      case MATCH_GT:
        return t('Greater than')
      case MATCH_GTE:
        return t('Greater than or equal')
      case MATCH_LT:
        return t('Less than')
      case MATCH_LTE:
        return t('Less than or equal')
      case MATCH_RANGE:
        return t('Overnight range')
      default:
        return mode
    }
  }
  const getTimeFuncLabel = (timeFunc: TimeFunc) => {
    switch (timeFunc) {
      case 'hour':
        return t('Hour of day')
      case 'minute':
        return t('Minute')
      case 'weekday':
        return t('Weekday')
      case 'month':
        return t('Month number')
      case 'day':
        return t('Day of month')
      default:
        return timeFunc
    }
  }
  let sourceLabel = t('Time')
  if (condition.source === SOURCE_PARAM) {
    sourceLabel = t('Body param')
  } else if (condition.source === SOURCE_HEADER) {
    sourceLabel = t('Header')
  }

  const handleSourceChange = (source: string) => {
    if (source === SOURCE_TIME) {
      onChange(createEmptyTimeCondition())
    } else if (source === SOURCE_HEADER || source === SOURCE_PARAM) {
      onChange({
        ...createEmptyCondition(),
        source: source as 'param' | 'header',
      })
    }
  }

  const handleModeChange = (mode: string) => {
    onChange({ ...condition, mode } as RequestCondition)
  }

  const renderTimeCondition = (timeCond: TimeCondition) => (
    <>
      <Select
        items={TIME_FUNCS.map((fn) => ({
          value: fn,
          label: getTimeFuncLabel(fn),
        }))}
        value={timeCond.timeFunc}
        onValueChange={(value) =>
          onChange({ ...timeCond, timeFunc: value as TimeFunc })
        }
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getTimeFuncLabel(timeCond.timeFunc)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {TIME_FUNCS.map((fn) => (
              <SelectItem key={fn} value={fn}>
                {getTimeFuncLabel(fn)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={COMMON_TIMEZONES.map((tz) => ({
          value: tz.value,
          label: tz.label,
        }))}
        value={timeCond.timezone}
        onValueChange={(value) =>
          value !== null && onChange({ ...timeCond, timezone: value })
        }
      >
        <SelectTrigger className='w-56' size='sm'>
          <SelectValue>
            {COMMON_TIMEZONES.find((tz) => tz.value === timeCond.timezone)
              ?.label ?? timeCond.timezone}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {COMMON_TIMEZONES.map((tz) => (
              <SelectItem key={tz.value} value={tz.value}>
                {tz.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={matchOptions.map((option) => ({
          value: option.value,
          label: getMatchLabel(option.value),
        }))}
        value={timeCond.mode}
        onValueChange={(v) => v !== null && handleModeChange(v)}
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getMatchLabel(timeCond.mode)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {matchOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {getMatchLabel(option.value)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      {timeCond.mode === MATCH_RANGE ? (
        <>
          <DraftNumberInput
            value={timeCond.rangeStart}
            onValueChange={(value) =>
              onChange({ ...timeCond, rangeStart: String(value) })
            }
            placeholder={t('Start')}
            className='w-20'
          />
          <span className='text-muted-foreground text-xs'>~</span>
          <DraftNumberInput
            value={timeCond.rangeEnd}
            onValueChange={(value) =>
              onChange({ ...timeCond, rangeEnd: String(value) })
            }
            placeholder={t('End')}
            className='w-20'
          />
        </>
      ) : (
        <DraftNumberInput
          value={timeCond.value}
          onValueChange={(value) =>
            onChange({ ...timeCond, value: String(value) })
          }
          placeholder={t('Value')}
          className='w-24'
        />
      )}
    </>
  )

  const renderParamHeaderCondition = (phCond: ParamHeaderCondition) => (
    <>
      <Input
        value={phCond.path}
        onChange={(event) => onChange({ ...phCond, path: event.target.value })}
        placeholder={
          phCond.source === SOURCE_HEADER ? 'X-Header-Name' : 'service_tier'
        }
        className='w-44'
      />
      <Select
        items={matchOptions.map((option) => ({
          value: option.value,
          label: getMatchLabel(option.value),
        }))}
        value={phCond.mode}
        onValueChange={(v) => v !== null && handleModeChange(v)}
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getMatchLabel(phCond.mode)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {matchOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {getMatchLabel(option.value)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      {phCond.mode !== MATCH_EXISTS && (
        <Input
          value={phCond.value}
          onChange={(event) =>
            onChange({ ...phCond, value: event.target.value })
          }
          placeholder={t('Value')}
          className='w-44'
        />
      )}
    </>
  )

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Select
        items={[
          { value: SOURCE_PARAM, label: t('Body param') },
          { value: SOURCE_HEADER, label: t('Header') },
          { value: SOURCE_TIME, label: t('Time') },
        ]}
        value={condition.source}
        onValueChange={(v) => v !== null && handleSourceChange(v)}
      >
        <SelectTrigger className='w-28' size='sm'>
          <SelectValue>{sourceLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            <SelectItem value={SOURCE_PARAM}>{t('Body param')}</SelectItem>
            <SelectItem value={SOURCE_HEADER}>{t('Header')}</SelectItem>
            <SelectItem value={SOURCE_TIME}>{t('Time')}</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      {condition.source === SOURCE_TIME
        ? renderTimeCondition(condition as TimeCondition)
        : renderParamHeaderCondition(condition as ParamHeaderCondition)}
      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
        aria-label={t('Remove condition')}
        className='ml-auto'
      >
        <Trash2 className='text-destructive h-4 w-4' />
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Request rule group card
// ---------------------------------------------------------------------------

type RuleGroupCardProps = {
  group: RequestRuleGroup
  index: number
  onChange: (next: RequestRuleGroup) => void
  onRemove: () => void
}

function RuleGroupCard({
  group,
  index,
  onChange,
  onRemove,
}: RuleGroupCardProps) {
  const { t } = useTranslation()

  const handleConditionChange = (
    conditionIndex: number,
    next: RequestCondition
  ) => {
    const conditions = [...group.conditions]
    conditions[conditionIndex] = next
    onChange({ ...group, conditions })
  }

  const handleAddCondition = (timeMode: boolean) => {
    onChange({
      ...group,
      conditions: [
        ...group.conditions,
        timeMode ? createEmptyTimeCondition() : createEmptyCondition(),
      ],
    })
  }

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='flex items-center justify-between gap-2'>
        <Badge variant='outline'>
          {t('Rule group')} #{index + 1}
        </Badge>
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          aria-label={t('Remove rule group')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      <div className='space-y-2'>
        {group.conditions.map((condition, conditionIndex) => (
          <RuleConditionRow
            key={getStableRenderKey(condition)}
            condition={condition}
            onChange={(next) => handleConditionChange(conditionIndex, next)}
            onRemove={() =>
              onChange({
                ...group,
                conditions: group.conditions.filter(
                  (_, i) => i !== conditionIndex
                ),
              })
            }
          />
        ))}
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => handleAddCondition(false)}
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add param/header')}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => handleAddCondition(true)}
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add time condition')}
          </Button>
        </div>
      </div>

      <div className='flex items-center gap-2'>
        <Label className='text-xs'>{t('Multiplier')}</Label>
        <DraftNumberInput
          min={0}
          step={0.000001}
          value={group.multiplier}
          onValueChange={(value) =>
            onChange({ ...group, multiplier: String(value) })
          }
          className='w-32'
          placeholder='1.0'
        />
        <span className='text-muted-foreground text-xs'>
          {t('Final cost = base × multiplier when conditions match')}
        </span>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Preset section
// ---------------------------------------------------------------------------

type PresetSectionProps = {
  applyPreset: (preset: Preset) => void
}

function PresetSection({ applyPreset }: PresetSectionProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const visible = expanded ? PRESET_GROUPS : PRESET_GROUPS.slice(0, 2)
  const hasMore = PRESET_GROUPS.length > 2

  return (
    <div className='space-y-2'>
      <div className='flex items-center gap-2'>
        <span className='text-sm font-medium'>{t('Preset templates')}</span>
        {hasMore && (
          <Button
            variant='ghost'
            size='sm'
            className='h-6 px-2 text-xs'
            onClick={() => setExpanded((prev) => !prev)}
          >
            {expanded ? t('Collapse') : t('More templates...')}
          </Button>
        )}
      </div>
      <div className='space-y-1'>
        {visible.map((presetGroup) => (
          <div
            key={presetGroup.group}
            className='flex flex-wrap items-center gap-2'
          >
            <Badge variant='secondary' className='min-w-[60px] justify-center'>
              {t(presetGroup.group)}
            </Badge>
            {presetGroup.presets.map((preset) => (
              <Button
                key={preset.key}
                variant='outline'
                size='sm'
                className='h-7 text-xs'
                onClick={() => applyPreset(preset)}
              >
                {preset.label}
              </Button>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Cost estimator
// ---------------------------------------------------------------------------

type EstimatorProps = {
  effectiveExpr: string
}

function CostEstimator({ effectiveExpr }: EstimatorProps) {
  const { t } = useTranslation()
  const [promptTokens, setPromptTokens] = useState(0)
  const [completionTokens, setCompletionTokens] = useState(0)
  const [extras, setExtras] = useState<ExtraTokenValues>({
    cacheReadTokens: 0,
    cacheCreateTokens: 0,
    cacheCreate1hTokens: 0,
    imageTokens: 0,
    imageOutputTokens: 0,
    audioInputTokens: 0,
    audioOutputTokens: 0,
  })

  const usesExtras = useMemo(
    () => exprUsesExtraVars(effectiveExpr),
    [effectiveExpr]
  )
  const isTaskExpr = isV2Expression(effectiveExpr)

  const result = useMemo(() => {
    if (isTaskExpr) {
      return evalExprLocallyV2(effectiveExpr, {
        ...createDefaultEvalInputs(),
        promptTokens,
        completionTokens,
        ...extras,
      })
    }
    return evalExprLocally(
      effectiveExpr,
      promptTokens,
      completionTokens,
      extras
    )
  }, [effectiveExpr, promptTokens, completionTokens, extras, isTaskExpr])

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='space-y-1'>
        <h4 className='text-sm font-medium'>{t('Token estimator')}</h4>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Enter token counts to preview the estimated cost (excluding group multipliers).'
          )}
        </p>
      </div>
      <div className='grid grid-cols-2 gap-3'>
        <div className='space-y-1'>
          <Label className='text-xs'>{t('Input tokens')}</Label>
          <DraftNumberInput
            min={0}
            value={promptTokens}
            onValueChange={setPromptTokens}
          />
        </div>
        <div className='space-y-1'>
          <Label className='text-xs'>{t('Output tokens')}</Label>
          <DraftNumberInput
            min={0}
            value={completionTokens}
            onValueChange={setCompletionTokens}
          />
        </div>
      </div>
      {usesExtras && (
        <div className='grid grid-cols-2 gap-3'>
          {BILLING_EXTRA_VARS.map((variable) => {
            // BILLING_EXTRA_VARS only contains pricing variables; they are
            // guaranteed to have a non-null `field` (the `len` condition-only
            // variable is filtered out). Narrow the type here for safety.
            if (!variable.field) return null
            const stateKey = variable.field.replace(
              'Price',
              'Tokens'
            ) as keyof ExtraTokenValues
            return (
              <div key={variable.key} className='space-y-1'>
                <Label className='text-xs'>{t(variable.shortLabel)}</Label>
                <DraftNumberInput
                  min={0}
                  value={extras[stateKey]}
                  onValueChange={(value) =>
                    setExtras((prev) => ({
                      ...prev,
                      [stateKey]: value,
                    }))
                  }
                />
              </div>
            )
          })}
        </div>
      )}
      <div
        className={cn(
          'rounded-md border p-3 text-sm',
          result.error
            ? 'border-destructive/50 bg-destructive/10 text-destructive'
            : 'border-primary/50 bg-primary/10'
        )}
      >
        {result.error ? (
          <span>
            {t('Expression error')}: {result.error}
          </span>
        ) : (
          <div className='flex items-center gap-2'>
            <span className='font-medium'>
              {t('Estimated quota cost')}: {result.cost.toLocaleString()}
            </span>
            {result.matchedTier && (
              <Badge variant='outline' className='text-xs'>
                {t('Hit tier')}: {result.matchedTier}
              </Badge>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// LLM prompt helper
// ---------------------------------------------------------------------------

const LLM_PROMPT_TEMPLATE = `You are an AI API billing expression design assistant. The user needs help designing a billing expression for an AI API gateway.

## Expression Language

Expressions are based on standard arithmetic with ternary operators.

### Token Variables

Input side:
- p — input token count (for pricing). Automatically excludes sub-categories priced separately (e.g., if cr is used, cache tokens are deducted from p)
- len — total input context length (for condition checks). Not affected by auto-exclusion; always reflects the full input length. Use in tier conditions
- cr — cache-hit (read) token count
- cc — cache-create token count (5-min TTL)
- cc1h — cache-create token count (1-hour TTL, Claude-specific)
- img — image input token count
- ai — audio input token count

Output side:
- c — output token count. Also auto-excludes sub-categories priced separately
- img_o — image output token count
- ao — audio output token count

### p/c Auto-exclusion

p and c are fallback variables representing all tokens not separately priced in the expression. If the expression uses a sub-category variable (e.g., cr), those tokens are deducted from p to avoid double-billing. Unused sub-category tokens remain in p/c at base price.

Important: len is NOT affected by auto-exclusion. Tier conditions should use len instead of p to prevent cache hits from lowering p and misidentifying the tier.

### Built-in Functions

- tier(name, value) — labels the billing tier; must wrap the cost expression
- max(a, b), min(a, b) — maximum/minimum
- ceil(x), floor(x), abs(x) — ceiling, floor, absolute value
- header(name) — reads a request header
- param(path) — reads a request body JSON path (gjson syntax)
- has(source, substr) — substring check
- hour(tz), minute(tz), weekday(tz), month(tz), day(tz) — time functions, tz is a timezone like "Asia/Shanghai"

### Price Coefficients

Numbers in the expression are $/1M tokens prices. For example, p * 2.5 means input $2.50/1M tokens.

## Expression Examples

Simple pricing:
tier("base", p * 2.5 + c * 15)

With cache:
tier("base", p * 2.5 + c * 15 + cr * 0.25)

Multi-tier (use len for conditions):
len <= 200000
  ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)
  : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)

Image model:
tier("base", p * 2 + c * 8 + img * 2.5)

Multimodal with audio:
tier("base", p * 0.43 + c * 3.06 + img * 0.78 + ai * 3.81 + ao * 15.11)

Three-tier example:
len <= 128000
  ? tier("standard", p * 1.1 + c * 4.4)
  : (len <= 1000000
    ? tier("medium", p * 2.2 + c * 8.8)
    : tier("long", p * 4.4 + c * 17.6))

## Rules

1. Every leaf branch must be wrapped in tier("name", cost_expr)
2. Use English tier names, e.g. "base", "standard", "long_context"
3. Use len for tier conditions (not p), supports <, <=, >, >=
4. Multi-tier uses nested ternary: cond1 ? tier(...) : (cond2 ? tier(...) : tier(...))
5. Price coefficients are the provider's official $/1M tokens prices
6. If cache/image/audio don't need separate pricing, omit those variables; their tokens are included in p/c automatically

Please generate a billing expression based on the model information and pricing requirements provided.`

type LlmPromptHelperProps = {
  modelName?: string
}

function LlmPromptHelper({ modelName }: LlmPromptHelperProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  const prompt = useMemo(() => {
    if (modelName) {
      return `${LLM_PROMPT_TEMPLATE}\n\nCurrent model: ${modelName}`
    }
    return LLM_PROMPT_TEMPLATE
  }, [modelName])

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(prompt)
      toast.success(t('Copied to clipboard'))
    } catch {
      toast.error(t('Failed to copy'))
    }
  }, [prompt, t])

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger
        render={
          <Button variant='ghost' size='sm' className='h-7 px-2 text-xs' />
        }
      >
        <Copy className='mr-1.5 h-3 w-3' />
        {t('LLM prompt helper')}
      </CollapsibleTrigger>
      <CollapsibleContent className='mt-2'>
        <div className='bg-muted/30 rounded-md border p-3'>
          <div className='mb-2 flex items-center justify-between'>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Copy this prompt and send it to an LLM (e.g. ChatGPT / Claude) to help design your billing expression.'
              )}
            </p>
            <Button
              variant='outline'
              size='sm'
              className='ml-3 shrink-0'
              onClick={handleCopy}
            >
              <Copy className='mr-1.5 h-3 w-3' />
              {t('Copy prompt')}
            </Button>
          </div>
          <Textarea
            value={prompt}
            readOnly
            rows={8}
            className='font-mono text-xs'
            spellCheck={false}
          />
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

// ---------------------------------------------------------------------------
// Main editor
// ---------------------------------------------------------------------------

export type TieredPricingEditorProps = {
  modelName?: string
  billingExpr: string
  requestRuleExpr: string
  onBillingExprChange: (next: string) => void
  onRequestRuleExprChange: (next: string) => void
  onValidationChange?: (isValid: boolean) => void
}

type EditorMode = 'visual-chat' | 'visual-task' | 'raw'

export const TieredPricingEditor = memo(function TieredPricingEditor({
  modelName,
  billingExpr: currentExpr,
  requestRuleExpr: currentRequestRuleExpr,
  onBillingExprChange,
  onRequestRuleExprChange,
  onValidationChange,
}: TieredPricingEditorProps) {
  const { t } = useTranslation()
  const [editorMode, setEditorMode] = useState<EditorMode>(() => {
    if (tryParseVisualConfigV2(currentExpr)) return 'visual-task'
    if (
      currentExpr &&
      (isV2Expression(currentExpr) || !tryParseVisualConfig(currentExpr))
    ) {
      return 'raw'
    }
    return 'visual-chat'
  })
  const [visualConfig, setVisualConfig] = useState<VisualConfig | null>(() =>
    tryParseVisualConfig(currentExpr)
  )
  const [visualConfigV2, setVisualConfigV2] = useState<VisualConfigV2 | null>(
    () => tryParseVisualConfigV2(currentExpr)
  )
  const [rawExpr, setRawExpr] = useState(() =>
    combineBillingExpr(currentExpr || '', currentRequestRuleExpr || '')
  )
  const [requestRuleGroups, setRequestRuleGroups] = useState<
    RequestRuleGroup[]
  >(() => tryParseRequestRuleExpr(currentRequestRuleExpr) || [])
  const initRef = useRef(false)
  const skipNextExprSyncRef = useRef(false)

  useEffect(() => {
    if (initRef.current) return
    // Only mark init as done when we have a meaningful expression. Without
    // this guard, a first-render pass where the parent's billingExpr state
    // hasn't propagated yet (empty string) would burn the initRef and any
    // later expression update would be short-circuited — the editor would
    // stay empty even though data was eventually available.
    if (!currentExpr && !currentRequestRuleExpr) return
    initRef.current = true
    // State updates below take effect on the next render. Until then,
    // effectiveExpr still reflects the previous/default editor mode.
    skipNextExprSyncRef.current = true
    const parsedV2 = tryParseVisualConfigV2(currentExpr)
    if (parsedV2) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setVisualConfigV2(parsedV2)
      setEditorMode('visual-task')
    } else if (isV2Expression(currentExpr)) {
      // v2 prefix but visual round-trip failed — fall back to raw.
      setVisualConfigV2(null)
      setEditorMode('raw')
    } else {
      const parsedConfig = tryParseVisualConfig(currentExpr)
      if (parsedConfig) {
        setVisualConfig(parsedConfig)
        setEditorMode('visual-chat')
      } else if (currentExpr) {
        setVisualConfig(null)
        setEditorMode('raw')
      } else {
        setVisualConfig(createDefaultVisualConfig())
      }
    }
    setRawExpr(
      combineBillingExpr(currentExpr || '', currentRequestRuleExpr || '')
    )
    setRequestRuleGroups(tryParseRequestRuleExpr(currentRequestRuleExpr) || [])
  }, [currentExpr, currentRequestRuleExpr])

  const canUseVisualRules = useMemo(() => {
    if (!currentRequestRuleExpr) return true
    return tryParseRequestRuleExpr(currentRequestRuleExpr) !== null
  }, [currentRequestRuleExpr])

  const effectiveExpr = useMemo(() => {
    if (editorMode === 'visual-chat') {
      return generateExprFromVisualConfig(visualConfig)
    }
    if (editorMode === 'visual-task') {
      return generateExprFromVisualConfigV2(visualConfigV2)
    }
    const { billingExpr } = splitBillingExprAndRequestRules(rawExpr)
    return billingExpr
  }, [editorMode, visualConfig, visualConfigV2, rawExpr])

  const isPreConsumeValid = useMemo(() => {
    const config = tryParseVisualConfigV2(effectiveExpr)
    if (!config) return true
    const usesPerMillionTokenPricing = config.tiers.some(
      (tier) => tier.per_mtok_cost > 0
    )
    return (
      !usesPerMillionTokenPricing ||
      (config.preConsumeTokensPerSecond ?? 0) > 0 ||
      (config.preConsumeTokens ?? 0) > 0 ||
      (config.preConsumeEstimate ?? 0) > 0
    )
  }, [effectiveExpr])

  useEffect(() => {
    onValidationChange?.(isPreConsumeValid)
  }, [isPreConsumeValid, onValidationChange])

  useEffect(() => {
    if (skipNextExprSyncRef.current) {
      skipNextExprSyncRef.current = false
      return
    }
    if (effectiveExpr !== currentExpr) {
      onBillingExprChange(effectiveExpr)
    }
  }, [effectiveExpr, currentExpr, onBillingExprChange])

  useEffect(() => {
    // Request-rule multiplier only applies in visual modes (both variants).
    if (editorMode === 'raw') return
    const ruleExpr = buildRequestRuleExpr(requestRuleGroups)
    if (ruleExpr !== currentRequestRuleExpr) {
      onRequestRuleExprChange(ruleExpr)
    }
  }, [
    editorMode,
    requestRuleGroups,
    currentRequestRuleExpr,
    onRequestRuleExprChange,
  ])

  const handleVisualChange = useCallback((next: VisualConfig) => {
    setVisualConfig(next)
  }, [])

  const handleVisualV2Change = useCallback((next: VisualConfigV2) => {
    setVisualConfigV2(next)
  }, [])

  const handleRawChange = useCallback(
    (value: string) => {
      setRawExpr(value)
      const { requestRuleExpr: ruleStr } =
        splitBillingExprAndRequestRules(value)
      onRequestRuleExprChange(ruleStr)
    },
    [onRequestRuleExprChange]
  )

  const handleModeChange = useCallback(
    (next: EditorMode) => {
      if (next === 'visual-chat') {
        const { billingExpr, requestRuleExpr: ruleStr } =
          splitBillingExprAndRequestRules(rawExpr)
        const parsed = tryParseVisualConfig(billingExpr)
        if (parsed) {
          setVisualConfig(parsed)
        } else {
          setVisualConfig(createDefaultVisualConfig())
        }
        const parsedGroups = tryParseRequestRuleExpr(ruleStr)
        setRequestRuleGroups(parsedGroups || [])
        onRequestRuleExprChange(ruleStr)
      } else if (next === 'visual-task') {
        const { billingExpr, requestRuleExpr: ruleStr } =
          splitBillingExprAndRequestRules(rawExpr)
        const parsedV2 = tryParseVisualConfigV2(billingExpr)
        if (parsedV2) {
          setVisualConfigV2(parsedV2)
        } else {
          setVisualConfigV2(createDefaultVisualConfigV2())
        }
        const parsedGroups = tryParseRequestRuleExpr(ruleStr)
        setRequestRuleGroups(parsedGroups || [])
        onRequestRuleExprChange(ruleStr)
      } else {
        // Switching to raw: serialize whichever visual mode was active.
        let expr = ''
        if (editorMode === 'visual-task') {
          expr = generateExprFromVisualConfigV2(visualConfigV2)
        } else {
          expr = generateExprFromVisualConfig(visualConfig)
        }
        const ruleExpr = buildRequestRuleExpr(requestRuleGroups)
        setRawExpr(combineBillingExpr(expr, ruleExpr) || expr)
      }
      setEditorMode(next)
    },
    [
      rawExpr,
      visualConfig,
      visualConfigV2,
      requestRuleGroups,
      editorMode,
      onRequestRuleExprChange,
    ]
  )

  const applyPreset = useCallback(
    (preset: Preset) => {
      const presetGroups = preset.requestRules || []
      const ruleExpr = buildRequestRuleExpr(presetGroups)
      const combined = combineBillingExpr(preset.expr, ruleExpr) || preset.expr
      setRawExpr(combined)
      const parsedV2 = tryParseVisualConfigV2(preset.expr)
      if (parsedV2) {
        setVisualConfigV2(parsedV2)
        setEditorMode('visual-task')
      } else if (isV2Expression(preset.expr)) {
        // v2 preset but not visually representable → raw
        setVisualConfigV2(null)
        setEditorMode('raw')
      } else {
        const parsed = tryParseVisualConfig(preset.expr)
        if (parsed) {
          setVisualConfig(parsed)
          setEditorMode('visual-chat')
        } else {
          setEditorMode('raw')
          setVisualConfig(null)
        }
      }
      setRequestRuleGroups(presetGroups)
      onRequestRuleExprChange(ruleExpr)
    },
    [onRequestRuleExprChange]
  )

  const handleRuleGroupsChange = useCallback((next: RequestRuleGroup[]) => {
    setRequestRuleGroups(next)
  }, [])

  return (
    <div className='space-y-5'>
      <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end'>
        <Field className='gap-2'>
          <FieldLabel>{t('Editor mode')}</FieldLabel>
          <Select
            items={[
              { value: 'visual-chat', label: t('Visual editor (chat)') },
              {
                value: 'visual-task',
                label: t('Visual editor (video / task)'),
              },
              { value: 'raw', label: t('Expression editor') },
            ]}
            value={editorMode}
            onValueChange={(value) => handleModeChange(value as EditorMode)}
          >
            <SelectTrigger className='w-full sm:w-64' size='sm'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='visual-chat'>
                  {t('Visual editor (chat)')}
                </SelectItem>
                <SelectItem value='visual-task'>
                  {t('Visual editor (video / task)')}
                </SelectItem>
                <SelectItem value='raw'>{t('Expression editor')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        {editorMode === 'raw' && (
          <div className='sm:pb-0.5'>
            <LlmPromptHelper modelName={modelName} />
          </div>
        )}
      </div>

      <PresetSection applyPreset={applyPreset} />

      <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
        {editorMode === 'visual-chat' && (
          <VisualEditor
            visualConfig={visualConfig}
            onChange={handleVisualChange}
          />
        )}
        {editorMode === 'visual-task' && (
          <TaskVisualEditor
            visualConfigV2={visualConfigV2}
            onChange={handleVisualV2Change}
          />
        )}
        {editorMode === 'raw' && (
          <RawExprEditor exprString={rawExpr} onChange={handleRawChange} />
        )}

        {editorMode !== 'raw' && (
          <div className='space-y-3 border-t pt-3'>
            <div className='space-y-1'>
              <h4 className='text-sm font-medium'>
                {t('Request rule pricing')}
              </h4>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'When conditions match, the final price is multiplied by X. Multiple matches multiply together; values < 1 act as discounts.'
                )}
              </p>
            </div>

            {currentRequestRuleExpr && !canUseVisualRules ? (
              <Alert>
                <AlertDescription className='text-xs'>
                  {t(
                    'This expression is too complex for the visual editor. Please switch to expression mode to edit.'
                  )}
                </AlertDescription>
              </Alert>
            ) : (
              <>
                {requestRuleGroups.map((group, groupIndex) => (
                  <RuleGroupCard
                    key={getStableRenderKey(group)}
                    group={group}
                    index={groupIndex}
                    onChange={(next) => {
                      const updated = [...requestRuleGroups]
                      updated[groupIndex] = next
                      handleRuleGroupsChange(updated)
                    }}
                    onRemove={() =>
                      handleRuleGroupsChange(
                        requestRuleGroups.filter((_, i) => i !== groupIndex)
                      )
                    }
                  />
                ))}
                <Button
                  variant='outline'
                  size='sm'
                  className='h-9 w-36 justify-center'
                  onClick={() =>
                    handleRuleGroupsChange([
                      ...requestRuleGroups,
                      createEmptyRuleGroup(),
                    ])
                  }
                >
                  <Plus className='mr-2 h-4 w-4' />
                  {t('Add rule group')}
                </Button>
              </>
            )}
          </div>
        )}
      </div>

      <CostEstimator effectiveExpr={effectiveExpr} />
    </div>
  )
})
