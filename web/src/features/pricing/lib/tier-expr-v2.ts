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

// Visual editor data model + generator/parser for `v2:` (task/video) billing
// expressions. Kept separate from tier-expr.ts (which is v1 chat-shaped) so
// the two visual modes can evolve independently.
//
// Body shape convention (per tier):
//     flat + per_second × seconds + per_n × n + per_mtok × p / 1_000_000
// Each factor is optional (0 means the term is omitted from the expression).
// The compiled tier looks like `tier("label", 0.30)` for a flat cost,
// `tier("label", 0.10 + 0.06 * seconds)` for a mixed cost, or
// `tier("label", 7.7 * p / 1000000)` for pure per-million-tokens billing.
//
// Per-million-tokens semantics: `per_mtok = N` means "N USD per 1M tokens".
// Because v2's quotaConversion multiplies by QuotaPerUnit only (no /1e6 like
// v1), we divide by 1e6 inside the expression body. Result: for p=1M tokens,
// quota = N × 1M / 1e6 × QuotaPerUnit = N × QuotaPerUnit, which matches the
// v1 chat convention ("ratio 1 = $2/1M tokens").

export const TASK_STRING_VARS = ['resolution', 'size', 'mode'] as const
export const TASK_BOOL_VARS = ['has_video', 'has_image'] as const
export const TASK_NUMERIC_VARS = ['seconds', 'n'] as const
export const MATRIX_UNMATCHED_TIER_LABEL = '__matrix_unmatched__'

// Matches the legacy task pre-consume path: one half of a 1M-token billing
// unit for a 5-second video, scaled linearly by requested duration.
export const LEGACY_TASK_PRECONSUME_TOKENS_PER_SECOND = 50_000
const TASK_PRECONSUME_MAX_SECONDS = 3600
const SEEDANCE_PRECONSUME_TOKEN_EXPR = `(p > 0 ? p : (seconds > 0 ? ${LEGACY_TASK_PRECONSUME_TOKENS_PER_SECOND} * seconds : ${LEGACY_TASK_PRECONSUME_TOKENS_PER_SECOND * TASK_PRECONSUME_MAX_SECONDS}))`

export const DOUBAO_SEEDANCE_2_PRICING_EXPR =
  `v2:resolution == "480p" && has_video ? tier("480p_true", 4.3 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: resolution == "480p" && !has_video ? tier("480p_false", 7.0 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: resolution == "720p" && has_video ? tier("720p_true", 4.3 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: resolution == "720p" && !has_video ? tier("720p_false", 7.0 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: resolution == "1080p" && has_video ? tier("1080p_true", 4.7 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: resolution == "1080p" && !has_video ? tier("1080p_false", 7.7 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: resolution == "4k" && has_video ? tier("4k_true", 2.4 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: resolution == "4k" && !has_video ? tier("4k_false", 4.0 * ${SEEDANCE_PRECONSUME_TOKEN_EXPR} / 1000000) ` +
  `: tier("${MATRIX_UNMATCHED_TIER_LABEL}", 0)`

export type TaskStringVar = (typeof TASK_STRING_VARS)[number]
export type TaskBoolVar = (typeof TASK_BOOL_VARS)[number]
export type TaskNumericVar = (typeof TASK_NUMERIC_VARS)[number]

export type TaskConditionInputV2 =
  | { kind: 'string_eq'; var: TaskStringVar; value: string }
  | { kind: 'bool'; var: TaskBoolVar; value: boolean }
  | {
      kind: 'numeric'
      var: TaskNumericVar
      op: '<' | '<=' | '>' | '>=' | '=='
      value: number | string
    }
  // Dynamic (custom) parameter conditions — used by the matrix builder to
  // support user-defined dimensions like param("metadata.quality").
  | { kind: 'param_string_eq'; path: string; value: string }
  | { kind: 'param_bool'; path: string; value: boolean }

export const NUMERIC_OPS: Array<'<' | '<=' | '>' | '>=' | '=='> = [
  '<',
  '<=',
  '>',
  '>=',
  '==',
]

export type VisualTierV2 = {
  label: string
  conditions: TaskConditionInputV2[]
  flat_cost: number
  per_second_cost: number
  per_n_cost: number
  // Per 1M tokens (USD). Only meaningful for adaptors that populate
  // TaskInfo.TotalTokens at settle (doubao / Byteplus / kling); at pre-consume
  // p is normally 0 unless the config supplies a fixed or per-second estimate.
  per_mtok_cost: number
  // Extra cost components beyond the four canonical ones (flat, per_second,
  // per_n, per_mtok). Each entry contributes `coef * variable` to the tier
  // body. `variable` can be a built-in numeric var (seconds, p, c, n, len) or
  // a `param("path")` / `header("name")` call. `label` is a human-readable
  // display name shown in the marketplace formula preview; the generator does
  // not embed it in the compiled expression (it lives in tier metadata only).
  // If `variable` starts with `param(` or `header(`, the coefficient is
  // multiplied by the JSON value / string length via the standard v2 semantics.
  customCosts?: CustomCostComponent[]
}

export type CustomCostComponent = {
  label: string
  coef: number
  variable: string
}

export type VisualConfigV2 = {
  tiers: VisualTierV2[]
  // Automatic task-token estimate used during pre-consume. The generated
  // expression uses `rate * seconds`, falling back to the 3600-second safety
  // ceiling when duration is absent. Settlement still uses upstream p.
  preConsumeTokensPerSecond?: number
  // Estimated prompt-token count used to size the pre-consume for per-M-token
  // tiers. When > 0, every `per_mtok_cost * p / 1e6` term in the generated
  // expression is rewritten as `per_mtok_cost * (p > 0 ? p : N) / 1e6` so:
  //   - pre-consume (backend p == 0) → the ternary evaluates to N → each tier
  //     computes its own pre-consume from its own per-M rate (auto-scaled
  //     per matched tier — no shared flat fallback).
  //   - settle (p == real tokens) → the ternary evaluates to real p → normal
  //     per-token cost → difference refunded/charged via settleTaskTieredExpr.
  //   - trace.MatchedTier records the real tier both times (not "preconsume").
  // Zero (default) disables the substitution → old pure-per-token behavior
  // where pre-consume evaluates to $0 until upstream returns tokens.
  preConsumeTokens?: number
  // LEGACY: flat $-per-call pre-consume amount from the old wrap form
  // `v2:p > 0 ? (<chain>) : tier("preconsume", N)`. Kept in the data model
  // so existing stored expressions round-trip cleanly; new configs should
  // prefer preConsumeTokens. Generator emits the legacy wrap only when
  // preConsumeTokens == 0 and preConsumeEstimate > 0.
  preConsumeEstimate?: number
}

// Label used for the synthetic pre-consume tier so trace.MatchedTier is
// meaningful on pre-consume logs. Kept exported so the marketplace breakdown
// can suppress it from the visible tier table.
export const PRECONSUME_TIER_LABEL = 'preconsume'

// ---------------------------------------------------------------------------
// Constructors / normalization
// ---------------------------------------------------------------------------

export function normalizeVisualTierV2(
  tier: Partial<VisualTierV2> = {}
): VisualTierV2 {
  return {
    label: tier.label ?? '',
    conditions: Array.isArray(tier.conditions) ? tier.conditions : [],
    flat_cost: Number(tier.flat_cost) || 0,
    per_second_cost: Number(tier.per_second_cost) || 0,
    per_n_cost: Number(tier.per_n_cost) || 0,
    per_mtok_cost: Number(tier.per_mtok_cost) || 0,
    customCosts: Array.isArray(tier.customCosts)
      ? tier.customCosts
          .map((c) => ({
            label: String(c?.label ?? ''),
            coef: Number(c?.coef) || 0,
            variable: String(c?.variable ?? '').trim(),
          }))
          .filter(
            (c) => c.variable !== '' && Number.isFinite(c.coef) && c.coef !== 0
          )
      : [],
  }
}

export function createDefaultVisualConfigV2(): VisualConfigV2 {
  return {
    tiers: [
      normalizeVisualTierV2({
        label: 'base',
        conditions: [],
        flat_cost: 0.3,
        per_second_cost: 0,
        per_n_cost: 0,
        per_mtok_cost: 0,
      }),
    ],
    preConsumeTokensPerSecond: 0,
    preConsumeTokens: 0,
    preConsumeEstimate: 0,
  }
}

export function normalizeVisualConfigV2(
  config: VisualConfigV2 | null | undefined
): VisualConfigV2 {
  if (!config || !Array.isArray(config.tiers) || config.tiers.length === 0) {
    return createDefaultVisualConfigV2()
  }
  const rawTokens = Number(config.preConsumeTokens)
  const preConsumeTokens =
    Number.isFinite(rawTokens) && rawTokens > 0 ? Math.round(rawTokens) : 0
  const rawEst = Number(config.preConsumeEstimate)
  const preConsumeEstimate = Number.isFinite(rawEst) && rawEst > 0 ? rawEst : 0
  const rawTokensPerSecond = Number(config.preConsumeTokensPerSecond)
  const preConsumeTokensPerSecond =
    Number.isFinite(rawTokensPerSecond) && rawTokensPerSecond > 0
      ? rawTokensPerSecond
      : 0
  return {
    tiers: config.tiers.map((tier) => normalizeVisualTierV2(tier)),
    preConsumeTokensPerSecond,
    preConsumeTokens,
    preConsumeEstimate,
  }
}

// ---------------------------------------------------------------------------
// Generator: VisualConfigV2 → expression string (always with `v2:` prefix)
// ---------------------------------------------------------------------------

const STRING_LIT_SAFE = /^[A-Za-z0-9_./+\- :]*$/

function quoteStringLiteral(value: string): string {
  // expr-lang uses standard double-quoted strings; escape backslash + quote.
  const escaped = value.replaceAll('\\', '\\\\').replaceAll('"', '\\"')
  return `"${escaped}"`
}

function buildConditionStr(conditions: TaskConditionInputV2[]): string {
  const parts: string[] = []
  for (const c of conditions) {
    if (c.kind === 'string_eq') {
      if (c.value === '') continue
      parts.push(`${c.var} == ${quoteStringLiteral(c.value)}`)
    } else if (c.kind === 'bool') {
      parts.push(c.value ? c.var : `!${c.var}`)
    } else if (c.kind === 'numeric') {
      if (c.value === '' || c.value == null) continue
      const num = Number(c.value)
      if (!Number.isFinite(num)) continue
      parts.push(`${c.var} ${c.op} ${num}`)
    } else if (c.kind === 'param_string_eq') {
      if (c.path === '' || c.value === '') continue
      parts.push(
        `param(${quoteStringLiteral(c.path)}) == ${quoteStringLiteral(c.value)}`
      )
    } else if (c.kind === 'param_bool') {
      if (c.path === '') continue
      parts.push(
        `param(${quoteStringLiteral(c.path)}) == ${c.value ? 'true' : 'false'}`
      )
    }
  }
  return parts.join(' && ')
}

function buildTierBodyExpr(
  tier: VisualTierV2,
  preConsumeTokens = 0,
  preConsumeTokensPerSecond = 0
): string {
  const parts: string[] = []
  const flat = Number(tier.flat_cost) || 0
  const perSec = Number(tier.per_second_cost) || 0
  const perN = Number(tier.per_n_cost) || 0
  const perMtok = Number(tier.per_mtok_cost) || 0
  if (flat !== 0) parts.push(String(flat))
  if (perSec !== 0) parts.push(`${perSec} * seconds`)
  if (perN !== 0) parts.push(`${perN} * n`)
  if (perMtok !== 0) {
    // When preConsumeTokens > 0, substitute p with (p > 0 ? p : N) so that
    // at pre-consume time (backend p == 0) each tier uses the estimated token
    // count to compute its own pre-consume amount, while at settle time the
    // real p is used. This keeps trace.MatchedTier as the real tier name.
    let pExpr = 'p'
    if (preConsumeTokensPerSecond > 0) {
      const fallbackTokens =
        preConsumeTokensPerSecond * TASK_PRECONSUME_MAX_SECONDS
      pExpr = `(p > 0 ? p : (seconds > 0 ? ${preConsumeTokensPerSecond} * seconds : ${fallbackTokens}))`
    } else if (preConsumeTokens > 0) {
      pExpr = `(p > 0 ? p : ${preConsumeTokens})`
    }
    parts.push(`${perMtok} * ${pExpr} / 1000000`)
  }
  for (const custom of tier.customCosts || []) {
    const coef = Number(custom.coef) || 0
    if (coef === 0 || !custom.variable) continue
    parts.push(`${coef} * ${custom.variable}`)
  }
  if (parts.length === 0) return '0'
  return parts.join(' + ')
}

export function generateExprFromVisualConfigV2(
  config: VisualConfigV2 | null | undefined
): string {
  if (!config || !config.tiers || config.tiers.length === 0) {
    return 'v2:tier("base", 0)'
  }
  const tiers = config.tiers
  const preConsumeTokensPerSecond =
    Number(config.preConsumeTokensPerSecond) || 0
  const preConsumeTokens = Number(config.preConsumeTokens) || 0
  const preConsumeEstimate = Number(config.preConsumeEstimate) || 0

  if (preConsumeTokensPerSecond > 0) {
    return `v2:${buildTierChainBody(tiers, 0, preConsumeTokensPerSecond)}`
  }

  // New form: inline p-substitution per tier body — each tier computes its own
  // pre-consume from its own per-M rate using the estimated token count.
  // Preferred over the legacy flat wrap when preConsumeTokens > 0.
  if (preConsumeTokens > 0) {
    return `v2:${buildTierChainBody(tiers, preConsumeTokens)}`
  }

  // Legacy flat wrap form (kept for backward compat with stored expressions).
  if (preConsumeEstimate > 0) {
    const chain = buildTierChainBody(tiers, 0)
    return `v2:p > 0 ? (${chain}) : tier("${PRECONSUME_TIER_LABEL}", ${preConsumeEstimate})`
  }

  return `v2:${buildTierChainBody(tiers, 0)}`
}

function buildTierChainBody(
  tiers: VisualTierV2[],
  preConsumeTokens = 0,
  preConsumeTokensPerSecond = 0
): string {
  if (tiers.length === 1) {
    const tier = tiers[0]
    const label = tier.label || 'base'
    const body = `tier("${label}", ${buildTierBodyExpr(tier, preConsumeTokens, preConsumeTokensPerSecond)})`
    const cond = buildConditionStr(tier.conditions)
    if (cond) return `${cond} ? ${body} : tier("default", 0)`
    return body
  }
  const parts: string[] = []
  for (let i = 0; i < tiers.length; i++) {
    const tier = tiers[i]
    const label = tier.label || `tier_${i + 1}`
    const body = `tier("${label}", ${buildTierBodyExpr(tier, preConsumeTokens, preConsumeTokensPerSecond)})`
    const cond = buildConditionStr(tier.conditions)
    if (i < tiers.length - 1 && cond) {
      parts.push(`${cond} ? ${body}`)
    } else {
      parts.push(body)
    }
  }
  return parts.join(' : ')
}

// ---------------------------------------------------------------------------
// Parser: expression string → VisualConfigV2 (round-trip safe subset only)
// ---------------------------------------------------------------------------

const STRING_VAR_SET = new Set<string>(TASK_STRING_VARS)
const BOOL_VAR_SET = new Set<string>(TASK_BOOL_VARS)
const NUMERIC_VAR_SET = new Set<string>(TASK_NUMERIC_VARS)

function parseSingleCondition(raw: string): TaskConditionInputV2 | null {
  const s = raw.trim()
  if (!s) return null

  // param("path") == "value"  (dynamic string dimension)
  const paramStrMatch = s.match(
    /^param\(\s*"([^"\\]*)"\s*\)\s*==\s*"([^"\\]*)"$/
  )
  if (paramStrMatch) {
    return {
      kind: 'param_string_eq',
      path: paramStrMatch[1],
      value: paramStrMatch[2],
    }
  }
  // param("path") == true|false  (dynamic bool dimension)
  const paramBoolMatch = s.match(
    /^param\(\s*"([^"\\]*)"\s*\)\s*==\s*(true|false)$/
  )
  if (paramBoolMatch) {
    return {
      kind: 'param_bool',
      path: paramBoolMatch[1],
      value: paramBoolMatch[2] === 'true',
    }
  }

  // Bool: `has_video`, `!has_video`, `has_image`, `!has_image`
  const bmNeg = s.match(/^!\s*([A-Za-z_][A-Za-z0-9_]*)$/)
  if (bmNeg && BOOL_VAR_SET.has(bmNeg[1])) {
    return { kind: 'bool', var: bmNeg[1] as TaskBoolVar, value: false }
  }
  if (BOOL_VAR_SET.has(s)) {
    return { kind: 'bool', var: s as TaskBoolVar, value: true }
  }

  // String equality: `var == "value"`
  const strMatch = s.match(/^([A-Za-z_][A-Za-z0-9_]*)\s*==\s*"([^"\\]*)"$/)
  if (strMatch && STRING_VAR_SET.has(strMatch[1])) {
    return {
      kind: 'string_eq',
      var: strMatch[1] as TaskStringVar,
      value: strMatch[2],
    }
  }

  // Numeric compare: `var op number`
  const numMatch = s.match(
    /^([A-Za-z_][A-Za-z0-9_]*)\s*(<=|>=|<|>|==)\s*([-+]?[\d.eE]+)$/
  )
  if (numMatch && NUMERIC_VAR_SET.has(numMatch[1])) {
    return {
      kind: 'numeric',
      var: numMatch[1] as TaskNumericVar,
      op: numMatch[2] as '<' | '<=' | '>' | '>=' | '==',
      value: Number(numMatch[3]),
    }
  }
  return null
}

function parseConditionExpr(raw: string): TaskConditionInputV2[] | null {
  const conditions: TaskConditionInputV2[] = []
  const parts = raw.split(/\s*&&\s*/)
  for (const p of parts) {
    const parsed = parseSingleCondition(p)
    if (!parsed) return null
    conditions.push(parsed)
  }
  return conditions
}

// Parse a tier body of the form
//   flat + per_sec * seconds + per_n * n + per_mtok * p / 1000000
// where each term is optional and can appear in any order. Returns null if
// the body has any unrecognized term.
function parseTierBody(raw: string): {
  flat: number
  per_second: number
  per_n: number
  per_mtok: number
  preConsumeTokensPerSecond: number
  preConsumeTokens: number
  customCosts: CustomCostComponent[]
} | null {
  const trimmed = raw.trim()
  if (trimmed === '' || trimmed === '0') {
    return {
      flat: 0,
      per_second: 0,
      per_n: 0,
      per_mtok: 0,
      preConsumeTokensPerSecond: 0,
      preConsumeTokens: 0,
      customCosts: [],
    }
  }
  const terms = splitTopLevelPlus(trimmed)
  let flat = 0
  let perSec = 0
  let perN = 0
  let perMtok = 0
  let preConsumeTokensPerSecond = 0
  let preConsumeTokens = 0
  const customCosts: CustomCostComponent[] = []
  const secOrNRe =
    /^([-+]?[\d.eE]+)\s*\*\s*(seconds|n)$|^(seconds|n)\s*\*\s*([-+]?[\d.eE]+)$/
  // New form: `coef * (p > 0 ? p : N) / 1000000`
  const mtokSubstRe =
    /^([-+]?[\d.eE]+)\s*\*\s*\(\s*p\s*>\s*0\s*\?\s*p\s*:\s*([\d.eE]+)\s*\)\s*\/\s*1000000$/
  // Automatic legacy-compatible form using requested seconds, with a fixed
  // maximum-duration fallback when the request does not specify a duration.
  const mtokAutoSubstRe =
    /^([-+]?[\d.eE]+)\s*\*\s*\(\s*p\s*>\s*0\s*\?\s*p\s*:\s*\(\s*seconds\s*>\s*0\s*\?\s*([\d.eE]+)\s*\*\s*seconds\s*:\s*([\d.eE]+)\s*\)\s*\)\s*\/\s*1000000$/
  // Legacy form: `coef * p / 1000000` or `p * coef / 1000000`
  const mtokRe =
    /^([-+]?[\d.eE]+)\s*\*\s*p\s*\/\s*1000000$|^p\s*\*\s*([-+]?[\d.eE]+)\s*\/\s*1000000$/
  const numericRe = /^[-+]?[\d.eE]+$/
  // Custom cost: `coef * <var>` where <var> is a bare identifier (p, c, len,
  // …) other than seconds/n (which we absorb into perSec/perN above), or a
  // `param("path")` / `header("name")` call.
  const customRe =
    /^([-+]?[\d.eE]+)\s*\*\s*([A-Za-z_][A-Za-z0-9_]*|param\(\s*"[^"\\]*"\s*\)|header\(\s*"[^"\\]*"\s*\))$/
  for (const term of terms) {
    const t = term.trim()
    if (numericRe.test(t)) {
      flat += Number(t)
      continue
    }
    const autoSubstM = t.match(mtokAutoSubstRe)
    if (autoSubstM) {
      const coef = Number(autoSubstM[1])
      const tokensPerSecond = Number(autoSubstM[2])
      const fallbackTokens = Number(autoSubstM[3])
      if (
        !Number.isFinite(coef) ||
        !Number.isFinite(tokensPerSecond) ||
        tokensPerSecond <= 0 ||
        fallbackTokens !== tokensPerSecond * TASK_PRECONSUME_MAX_SECONDS
      ) {
        return null
      }
      perMtok += coef
      if (preConsumeTokensPerSecond === 0) {
        preConsumeTokensPerSecond = tokensPerSecond
      }
      continue
    }
    const substM = t.match(mtokSubstRe)
    if (substM) {
      const coef = Number(substM[1])
      const est = Number(substM[2])
      if (!Number.isFinite(coef) || !Number.isFinite(est)) return null
      perMtok += coef
      if (preConsumeTokens === 0) preConsumeTokens = Math.round(est)
      continue
    }
    const mtokM = t.match(mtokRe)
    if (mtokM) {
      const coef = Number(mtokM[1] ?? mtokM[2])
      if (!Number.isFinite(coef)) return null
      perMtok += coef
      continue
    }
    const m = t.match(secOrNRe)
    if (m) {
      const coef = Number(m[1] ?? m[4])
      const varName = (m[2] ?? m[3]) as 'seconds' | 'n'
      if (!Number.isFinite(coef)) return null
      if (varName === 'seconds') perSec += coef
      else perN += coef
      continue
    }
    const cm = t.match(customRe)
    if (cm) {
      const coef = Number(cm[1])
      const variable = cm[2].replaceAll(/\s+/g, '')
      if (!Number.isFinite(coef)) return null
      customCosts.push({ label: '', coef, variable })
      continue
    }
    return null
  }
  return {
    flat,
    per_second: perSec,
    per_n: perN,
    per_mtok: perMtok,
    preConsumeTokensPerSecond,
    preConsumeTokens,
    customCosts,
  }
}

// Split a tier body on top-level `+` operators, respecting parentheses and
// string literals so nested subexpressions like `(p > 0 ? p : N)` or
// `param("a+b")` don't get chopped in the middle.
function splitTopLevelPlus(s: string): string[] {
  const out: string[] = []
  let depth = 0
  let inStr = false
  let start = 0
  for (let i = 0; i < s.length; i++) {
    const ch = s[i]
    if (inStr) {
      if (ch === '\\') {
        i++
        continue
      }
      if (ch === '"') inStr = false
      continue
    }
    if (ch === '"') {
      inStr = true
      continue
    }
    if (ch === '(') {
      depth++
      continue
    }
    if (ch === ')') {
      depth--
      continue
    }
    if (ch === '+' && depth === 0) {
      out.push(s.slice(start, i))
      start = i + 1
    }
  }
  out.push(s.slice(start))
  return out.map((x) => x.trim()).filter((x) => x !== '')
}

// Parse one `tier("label", body)` occurrence, plus its preceding condition
// group (if any). Returns null if the tier() call or body is malformed.
type ParsedTierV2 = {
  tier: VisualTierV2
  bodyEnd: number
  preConsumeTokensPerSecond: number
  preConsumeTokens: number
}

function parseTierAt(body: string, tierStart: number): ParsedTierV2 | null {
  const openParen = body.indexOf('(', tierStart)
  if (openParen < 0) return null
  // Find matching close paren respecting nested parens (defensive; the body is
  // usually flat).
  let depth = 0
  let closeParen = -1
  let inStr = false
  for (let i = openParen; i < body.length; i++) {
    const ch = body[i]
    if (inStr) {
      if (ch === '\\') {
        i++ // skip next
        continue
      }
      if (ch === '"') inStr = false
      continue
    }
    if (ch === '"') {
      inStr = true
      continue
    }
    if (ch === '(') depth++
    else if (ch === ')') {
      depth--
      if (depth === 0) {
        closeParen = i
        break
      }
    }
  }
  if (closeParen < 0) return null
  const args = body.slice(openParen + 1, closeParen)
  // Split on first top-level comma.
  let commaIdx = -1
  let sd = 0
  let sIn = false
  for (let i = 0; i < args.length; i++) {
    const ch = args[i]
    if (sIn) {
      if (ch === '\\') {
        i++
        continue
      }
      if (ch === '"') sIn = false
      continue
    }
    if (ch === '"') {
      sIn = true
      continue
    }
    if (ch === '(') sd++
    else if (ch === ')') sd--
    else if (ch === ',' && sd === 0) {
      commaIdx = i
      break
    }
  }
  if (commaIdx < 0) return null
  const labelPart = args.slice(0, commaIdx).trim()
  const bodyPart = args.slice(commaIdx + 1).trim()
  const labelMatch = labelPart.match(/^"([^"\\]*)"$/)
  if (!labelMatch) return null
  const parsedBody = parseTierBody(bodyPart)
  if (!parsedBody) return null
  return {
    tier: {
      label: labelMatch[1],
      conditions: [],
      flat_cost: parsedBody.flat,
      per_second_cost: parsedBody.per_second,
      per_n_cost: parsedBody.per_n,
      per_mtok_cost: parsedBody.per_mtok,
      customCosts: parsedBody.customCosts,
    },
    bodyEnd: closeParen + 1,
    preConsumeTokensPerSecond: parsedBody.preConsumeTokensPerSecond,
    preConsumeTokens: parsedBody.preConsumeTokens,
  }
}

// Detect the pre-consume wrap: `p > 0 ? (<inner>) : tier("preconsume", N)`.
// Returns the unwrapped inner body + the estimate value, or null when the
// input doesn't match the wrap form (in which case the caller treats the
// whole string as the tier chain body).
function extractPreConsumeWrap(
  body: string
): { inner: string; estimate: number } | null {
  const s = body.trim()
  // Must start with `p > 0 ?`
  const guard = s.match(/^p\s*>\s*0\s*\?\s*/)
  if (!guard) return null
  // Find the matching close paren for the `(` right after the guard.
  const rest = s.slice(guard[0].length).trimStart()
  if (!rest.startsWith('(')) return null
  let depth = 0
  let inStr = false
  let closeAt = -1
  for (let i = 0; i < rest.length; i++) {
    const ch = rest[i]
    if (inStr) {
      if (ch === '\\') {
        i++
        continue
      }
      if (ch === '"') inStr = false
      continue
    }
    if (ch === '"') {
      inStr = true
      continue
    }
    if (ch === '(') depth++
    else if (ch === ')') {
      depth--
      if (depth === 0) {
        closeAt = i
        break
      }
    }
  }
  if (closeAt < 0) return null
  const inner = rest.slice(1, closeAt).trim()
  const tail = rest.slice(closeAt + 1).trimStart()
  // Tail must be `: tier("preconsume", <number>)`.
  const tailMatch = tail.match(
    new RegExp(
      `^:\\s*tier\\(\\s*"${PRECONSUME_TIER_LABEL}"\\s*,\\s*([-+]?[\\d.eE]+)\\s*\\)\\s*$`
    )
  )
  if (!tailMatch) return null
  const estimate = Number(tailMatch[1])
  if (!Number.isFinite(estimate) || estimate < 0) return null
  return { inner, estimate }
}

export function tryParseVisualConfigV2(
  exprStr: string | null | undefined
): VisualConfigV2 | null {
  if (!exprStr) return null
  const trimmed = exprStr.trim()
  if (!trimmed.startsWith('v2:')) return null
  const bodyRaw = trimmed.slice(3).trim()
  if (!bodyRaw) return null

  // Strip the optional pre-consume wrap before walking the tier chain.
  const wrap = extractPreConsumeWrap(bodyRaw)
  const body = wrap ? wrap.inner : bodyRaw
  const preConsumeEstimate = wrap ? wrap.estimate : 0

  try {
    const tiers: VisualTierV2[] = []
    let cursor = 0
    let pendingCondition: TaskConditionInputV2[] | null = null
    let detectedPreConsumeTokensPerSecond = 0
    let detectedPreConsumeTokens = 0

    while (cursor < body.length) {
      const remaining = body.slice(cursor).trimStart()
      const trimOffset = body.length - cursor - remaining.length
      cursor += trimOffset

      // Look ahead for `tier(` — the next token in a well-formed v2 expression.
      const tierIdx = remaining.indexOf('tier(')
      if (tierIdx < 0) return null

      if (tierIdx > 0) {
        const preface = remaining.slice(0, tierIdx).trim()
        // Expect the preface to end with '?' meaning the preceding chunk is a
        // condition. Discard leading ':' from earlier chain.
        let condChunk = preface
        if (condChunk.startsWith(':')) condChunk = condChunk.slice(1).trim()
        if (condChunk.endsWith('?')) {
          condChunk = condChunk.slice(0, -1).trim()
        } else if (condChunk !== '') {
          // Not a recognizable pattern.
          return null
        }
        if (condChunk) {
          const parsedCond = parseConditionExpr(condChunk)
          if (!parsedCond) return null
          pendingCondition = parsedCond
        } else {
          pendingCondition = null
        }
      } else {
        pendingCondition = null
      }

      const parsed = parseTierAt(remaining, tierIdx)
      if (!parsed) return null
      const tier = parsed.tier
      if (pendingCondition) tier.conditions = pendingCondition
      tiers.push(normalizeVisualTierV2(tier))
      if (
        parsed.preConsumeTokensPerSecond > 0 &&
        detectedPreConsumeTokensPerSecond === 0
      ) {
        detectedPreConsumeTokensPerSecond = parsed.preConsumeTokensPerSecond
      }
      if (parsed.preConsumeTokens > 0 && detectedPreConsumeTokens === 0) {
        detectedPreConsumeTokens = parsed.preConsumeTokens
      }
      cursor += parsed.bodyEnd
      pendingCondition = null

      // Skip whitespace + optional ` : ` separator for the next tier.
      const sepRemaining = body.slice(cursor).trimStart()
      const sepOffset = body.length - cursor - sepRemaining.length
      cursor += sepOffset
      if (sepRemaining.startsWith(':')) {
        cursor += 1
      } else if (sepRemaining.length > 0) {
        // Unexpected trailing content.
        return null
      }
    }

    if (tiers.length === 0) return null

    // The last tier is treated as the default — if it accidentally has
    // conditions from a parser slip, drop them so re-serialization is stable.
    const lastTier = tiers.at(-1)
    if (lastTier) {
      lastTier.conditions = []
    }

    const cfg = normalizeVisualConfigV2({
      tiers,
      preConsumeTokensPerSecond: detectedPreConsumeTokensPerSecond,
      preConsumeTokens: detectedPreConsumeTokens,
      preConsumeEstimate,
    })
    // Round-trip check: regenerated expr must match the input after
    // whitespace + numeric-literal normalization. Numbers are canonicalized
    // via Number → String on both sides so that `0.30` (input) and `0.3`
    // (generator output) round-trip cleanly.
    const regenerated = generateExprFromVisualConfigV2(cfg).trim()
    const normalizedInput = canonicalizeExprText(`v2:${bodyRaw}`)
    const normalizedOut = canonicalizeExprText(regenerated)
    if (normalizedInput !== normalizedOut) {
      return null
    }
    return cfg
  } catch {
    return null
  }
}

// Normalize an expression string for round-trip comparison. Collapses
// whitespace and canonicalizes numeric literals (0.30 → 0.3, 1e6 → 1000000)
// so admin-authored expressions round-trip regardless of formatting.
function canonicalizeExprText(s: string): string {
  const parts = s.split(/("(?:\\.|[^"\\])*")/)
  return parts
    .map((part, i) => {
      if (i % 2 === 1) return part
      return part.replaceAll(
        /(?<![A-Za-z_$])([-+]?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?)/g,
        (m) => {
          const n = Number(m)
          return Number.isFinite(n) ? String(n) : m
        }
      )
    })
    .join('')
    .replaceAll(/\s+/g, ' ')
    .trim()
}

// ---------------------------------------------------------------------------
// Local evaluator (for the preview panel)
// ---------------------------------------------------------------------------

export type TaskEvalInputs = {
  seconds: number
  resolution: string
  size: string
  has_video: boolean
  has_image: boolean
  n: number
  mode: string
  promptTokens?: number
  completionTokens?: number
  cacheReadTokens?: number
  cacheCreateTokens?: number
  cacheCreate1hTokens?: number
  imageTokens?: number
  imageOutputTokens?: number
  audioInputTokens?: number
  audioOutputTokens?: number
}

export type TaskEvalResult = {
  cost: number
  matchedTier: string
  error: string | null
}

export function createDefaultEvalInputs(): TaskEvalInputs {
  return {
    seconds: 5,
    resolution: '1080p',
    size: '',
    has_video: false,
    has_image: false,
    n: 1,
    mode: '',
  }
}

export function evalExprLocallyV2(
  exprStr: string,
  inputs: TaskEvalInputs
): TaskEvalResult {
  try {
    if (!exprStr || !exprStr.trim()) {
      return { cost: 0, matchedTier: '', error: null }
    }
    let body = exprStr.trim()
    if (body.startsWith('v2:')) body = body.slice(3)
    if (body.startsWith('v1:')) body = body.slice(3)
    let matchedTier = ''
    const tierFn = (name: string, value: number) => {
      matchedTier = name
      return value
    }
    const promptTokens = Number(inputs.promptTokens) || 0
    const cacheReadTokens = Number(inputs.cacheReadTokens) || 0
    const cacheCreateTokens = Number(inputs.cacheCreateTokens) || 0
    const cacheCreate1hTokens = Number(inputs.cacheCreate1hTokens) || 0
    const env: Record<string, unknown> = {
      seconds: Number(inputs.seconds) || 0,
      resolution: String(inputs.resolution || ''),
      size: String(inputs.size || ''),
      has_video: Boolean(inputs.has_video),
      has_image: Boolean(inputs.has_image),
      n: Number(inputs.n) || 0,
      mode: String(inputs.mode || ''),
      p: promptTokens,
      c: Number(inputs.completionTokens) || 0,
      len:
        promptTokens +
        cacheReadTokens +
        cacheCreateTokens +
        cacheCreate1hTokens,
      cr: cacheReadTokens,
      cc: cacheCreateTokens,
      cc1h: cacheCreate1hTokens,
      img: Number(inputs.imageTokens) || 0,
      img_o: Number(inputs.imageOutputTokens) || 0,
      ai: Number(inputs.audioInputTokens) || 0,
      ao: Number(inputs.audioOutputTokens) || 0,
      tier: tierFn,
      max: Math.max,
      min: Math.min,
      abs: Math.abs,
      ceil: Math.ceil,
      floor: Math.floor,
    }
    // eslint-disable-next-line no-new-func
    const fn = new Function(
      ...Object.keys(env),
      `"use strict"; return (${body});`
    )
    const cost = Number(fn(...Object.values(env))) || 0
    return { cost, matchedTier, error: null }
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e)
    return { cost: 0, matchedTier: '', error: message }
  }
}

// Guard so the version check is available to editor components without them
// having to string-slice.
export function isV2Expression(exprStr: string | null | undefined): boolean {
  if (!exprStr) return false
  return exprStr.trim().startsWith('v2:')
}

// ---------------------------------------------------------------------------
// Matrix pricing model
// ---------------------------------------------------------------------------
//
// A matrix is a structured view over a tier list: N dimensions × M values each
// produce a Cartesian product of cells, one price per cell. Under the hood
// it's still a v2 tier list, so the same generator/parser/settle path applies
// with no backend changes. The matrix editor is a UX shortcut for admins who
// want to define a large 2D/3D price table (like doubao seedance's
// resolution × has_video pricing) without hand-editing 6+ tier cards.
//
// Dimensions are dynamic — users can pick from the v2 first-class variables
// (resolution / size / mode / has_video / has_image) OR add custom dimensions
// backed by param("path") for parameters not surfaced as first-class vars.

export type MatrixDimensionSource =
  | { kind: 'v2_string'; var: TaskStringVar }
  | { kind: 'v2_bool'; var: TaskBoolVar }
  | { kind: 'param_string'; path: string }
  | { kind: 'param_bool'; path: string }

export type MatrixDimension = {
  id: string // client-side stable id for React keys
  source: MatrixDimensionSource
  // Enumerated values for the dimension. Bool dimensions always store
  // ['true', 'false'] canonically; string dimensions accept any user input.
  values: string[]
}

export type MatrixCostUnit =
  | 'flat' // $ per call
  | 'per_second' // $ × seconds
  | 'per_n' // $ × n
  | 'per_mtok' // $ per 1M tokens (× p / 1e6)

export type MatrixCells = Record<string, number>

export type VisualMatrixV2 = {
  dimensions: MatrixDimension[]
  costUnit: MatrixCostUnit
  cells: MatrixCells // keyed by cellKey below
  preConsumeTokensPerSecond?: number
  preConsumeTokens?: number
  // LEGACY: kept for round-trip of old stored expressions
  preConsumeEstimate?: number
}

// Cell key: deterministic joined coord string. Dimensions ordered as given.
// e.g. "resolution=1080p|has_video=true|param:metadata.quality=hd"
export function cellKeyFromCoords(
  dimensions: MatrixDimension[],
  coords: string[]
): string {
  return dimensions
    .map((d, i) => `${dimensionCoordKey(d)}=${coords[i]}`)
    .join('|')
}

export function cellKeyFromTierConditions(
  dimensions: MatrixDimension[],
  conditions: TaskConditionInputV2[]
): string | null {
  const coords: string[] = []
  for (const dimension of dimensions) {
    const condition = conditions.find(
      (item) => conditionCoordKey(item) === dimensionCoordKey(dimension)
    )
    if (!condition) return null
    coords.push(conditionValueString(condition))
  }
  return cellKeyFromCoords(dimensions, coords)
}

export function cellKeyFromMatchedParams(
  dimensions: MatrixDimension[],
  params: Readonly<Record<string, unknown>>
): string | null {
  const coords: string[] = []
  for (const dimension of dimensions) {
    const key = dimensionCoordKey(dimension)
    let value = params[key]
    if (
      value === undefined &&
      (dimension.source.kind === 'param_string' ||
        dimension.source.kind === 'param_bool')
    ) {
      value = params[dimension.source.path]
    }
    if (value === undefined || value === null) return null

    if (
      dimension.source.kind === 'v2_bool' ||
      dimension.source.kind === 'param_bool'
    ) {
      if (typeof value === 'boolean') {
        coords.push(value ? 'true' : 'false')
        continue
      }
      if (typeof value === 'string') {
        const normalized = value.toLowerCase()
        if (normalized === 'true' || normalized === 'false') {
          coords.push(normalized)
          continue
        }
      }
      return null
    }

    if (
      typeof value !== 'string' &&
      typeof value !== 'number' &&
      typeof value !== 'boolean'
    ) {
      return null
    }
    coords.push(String(value))
  }
  return cellKeyFromCoords(dimensions, coords)
}

export function dimensionCoordKey(d: MatrixDimension): string {
  switch (d.source.kind) {
    case 'v2_string':
    case 'v2_bool':
      return d.source.var
    case 'param_string':
    case 'param_bool':
      return `param:${d.source.path}`
  }
}

export function dimensionDisplayLabel(d: MatrixDimension): string {
  switch (d.source.kind) {
    case 'v2_string':
    case 'v2_bool':
      return d.source.var
    case 'param_string':
    case 'param_bool':
      return `param(${d.source.path})`
  }
}

function safeLabelSegment(raw: string): string {
  return raw.replaceAll(/[^A-Za-z0-9]+/g, '_').replaceAll(/^_+|_+$/g, '') || 'v'
}

function conditionFromDimensionValue(
  d: MatrixDimension,
  value: string
): TaskConditionInputV2 {
  switch (d.source.kind) {
    case 'v2_string':
      return { kind: 'string_eq', var: d.source.var, value }
    case 'v2_bool':
      return { kind: 'bool', var: d.source.var, value: value === 'true' }
    case 'param_string':
      return { kind: 'param_string_eq', path: d.source.path, value }
    case 'param_bool':
      return {
        kind: 'param_bool',
        path: d.source.path,
        value: value === 'true',
      }
  }
}

function cellCoordsCartesian(dimensions: MatrixDimension[]): string[][] {
  if (dimensions.length === 0) return [[]]
  const [first, ...rest] = dimensions
  const restCombos = cellCoordsCartesian(rest)
  const out: string[][] = []
  for (const v of first.values) {
    for (const combo of restCombos) {
      out.push([v, ...combo])
    }
  }
  return out
}

function costFieldsForUnit(
  unit: MatrixCostUnit,
  cost: number
): Pick<
  VisualTierV2,
  'flat_cost' | 'per_second_cost' | 'per_n_cost' | 'per_mtok_cost'
> {
  const zeros = {
    flat_cost: 0,
    per_second_cost: 0,
    per_n_cost: 0,
    per_mtok_cost: 0,
  }
  switch (unit) {
    case 'flat':
      return { ...zeros, flat_cost: cost }
    case 'per_second':
      return { ...zeros, per_second_cost: cost }
    case 'per_n':
      return { ...zeros, per_n_cost: cost }
    case 'per_mtok':
      return { ...zeros, per_mtok_cost: cost }
  }
}

function selectCostUnitFromTier(tier: VisualTierV2): MatrixCostUnit | null {
  const set = [
    tier.flat_cost !== 0 ? 'flat' : null,
    tier.per_second_cost !== 0 ? 'per_second' : null,
    tier.per_n_cost !== 0 ? 'per_n' : null,
    tier.per_mtok_cost !== 0 ? 'per_mtok' : null,
  ].filter(Boolean) as MatrixCostUnit[]
  if (set.length === 0) return 'flat'
  if (set.length === 1) return set[0]
  return null // mixed — not representable as a single-unit matrix
}

function costValueForUnit(tier: VisualTierV2, unit: MatrixCostUnit): number {
  switch (unit) {
    case 'flat':
      return tier.flat_cost
    case 'per_second':
      return tier.per_second_cost
    case 'per_n':
      return tier.per_n_cost
    case 'per_mtok':
      return tier.per_mtok_cost
  }
}

// Build a VisualConfigV2 from a matrix. The final tier is a reserved control
// marker that makes unmatched requests fail closed in backend billing.
export function matrixToVisualConfigV2(matrix: VisualMatrixV2): VisualConfigV2 {
  const preConsumeTokensPerSecond =
    Number(matrix.preConsumeTokensPerSecond) || 0
  const preConsumeTokens = Number(matrix.preConsumeTokens) || 0
  const preConsumeEstimate = Number(matrix.preConsumeEstimate) || 0
  const validDims = matrix.dimensions.filter(
    (d) =>
      d.values.length > 0 &&
      (d.source.kind !== 'param_string' || d.source.path !== '')
  )
  if (validDims.length === 0) {
    return {
      tiers: [
        normalizeVisualTierV2({
          label: MATRIX_UNMATCHED_TIER_LABEL,
          conditions: [],
        }),
      ],
      preConsumeTokensPerSecond,
      preConsumeTokens,
      preConsumeEstimate,
    }
  }
  const coords = cellCoordsCartesian(validDims)
  const tiers: VisualTierV2[] = []
  for (const combo of coords) {
    const key = cellKeyFromCoords(validDims, combo)
    const cost = Number(matrix.cells[key]) || 0
    const conditions = validDims.map((d, i) =>
      conditionFromDimensionValue(d, combo[i])
    )
    const label = combo.map(safeLabelSegment).join('_') || 'cell'
    tiers.push(
      normalizeVisualTierV2({
        label,
        conditions,
        ...costFieldsForUnit(matrix.costUnit, cost),
      })
    )
  }
  // The final zero-cost tier is a control marker. Backend billing rejects it
  // before upstream submission and retains pre-consume if settlement reaches
  // it, so an unexpected dimension value can never become a free generation.
  tiers.push(
    normalizeVisualTierV2({
      label: MATRIX_UNMATCHED_TIER_LABEL,
      conditions: [],
      flat_cost: 0,
      per_second_cost: 0,
      per_n_cost: 0,
      per_mtok_cost: 0,
    })
  )
  return {
    tiers,
    preConsumeTokensPerSecond,
    preConsumeTokens,
    preConsumeEstimate,
  }
}

// Best-effort reverse-parse: check whether a VisualConfigV2 was authored as a
// matrix (all cells present, single cost unit). Returns null if the tier list
// doesn't fit the matrix shape.
export function tryParseMatrixFromVisualConfig(
  cfg: VisualConfigV2
): VisualMatrixV2 | null {
  const tiers = cfg.tiers.filter(
    (t) => t.conditions.length > 0 // exclude the trailing fallback tier
  )
  if (tiers.length === 0) return null

  // Every tier must have the same set of dimension keys and each condition
  // must equal exactly one value. That's the matrix contract.
  const firstDimKeys = tiers[0].conditions.map(conditionCoordKey)
  const dimValueSets: Record<string, Set<string>> = Object.fromEntries(
    firstDimKeys.map((k) => [k, new Set<string>()])
  )
  const dimSourceByKey: Record<string, MatrixDimensionSource> = {}
  for (const c of tiers[0].conditions) {
    dimSourceByKey[conditionCoordKey(c)] = conditionToDimensionSource(c)
  }

  for (const tier of tiers) {
    if (tier.conditions.length !== firstDimKeys.length) return null
    const seen = new Set<string>()
    for (const cond of tier.conditions) {
      const key = conditionCoordKey(cond)
      if (!firstDimKeys.includes(key)) return null
      if (seen.has(key)) return null
      seen.add(key)
      const src = conditionToDimensionSource(cond)
      const existing = dimSourceByKey[key]
      if (!sameDimensionSource(existing, src)) return null
      dimValueSets[key].add(conditionValueString(cond))
    }
  }

  // Determine cost unit — must be shared across all tiers.
  let unit: MatrixCostUnit | null = null
  for (const tier of tiers) {
    const u = selectCostUnitFromTier(tier)
    if (u === null) return null
    if (unit === null) unit = u
    else if (unit !== u) {
      // Allow the case where a tier has all zeros; skip refining.
      const allZero =
        tier.flat_cost === 0 &&
        tier.per_second_cost === 0 &&
        tier.per_n_cost === 0 &&
        tier.per_mtok_cost === 0
      if (!allZero) return null
    }
  }
  if (unit === null) unit = 'flat'

  const dimensions: MatrixDimension[] = firstDimKeys.map((key, idx) => ({
    id: `dim-${idx}-${key}`,
    source: dimSourceByKey[key],
    values: [...dimValueSets[key]],
  }))

  // Verify the full Cartesian product is covered exactly once.
  const expectedCombos = cellCoordsCartesian(dimensions)
  if (expectedCombos.length !== tiers.length) return null

  const cells: MatrixCells = {}
  for (const tier of tiers) {
    const coords = dimensions.map((d) => {
      const cond = tier.conditions.find(
        (c) => conditionCoordKey(c) === dimensionCoordKey(d)
      )
      return cond ? conditionValueString(cond) : ''
    })
    const key = cellKeyFromCoords(dimensions, coords)
    cells[key] = costValueForUnit(tier, unit)
  }

  return {
    dimensions,
    costUnit: unit,
    cells,
    preConsumeTokensPerSecond: Number(cfg.preConsumeTokensPerSecond) || 0,
    preConsumeTokens: Number(cfg.preConsumeTokens) || 0,
    preConsumeEstimate: Number(cfg.preConsumeEstimate) || 0,
  }
}

function conditionCoordKey(cond: TaskConditionInputV2): string {
  switch (cond.kind) {
    case 'string_eq':
    case 'bool':
    case 'numeric':
      return cond.var
    case 'param_string_eq':
    case 'param_bool':
      return `param:${cond.path}`
  }
}

function conditionToDimensionSource(
  cond: TaskConditionInputV2
): MatrixDimensionSource {
  switch (cond.kind) {
    case 'string_eq':
      return { kind: 'v2_string', var: cond.var }
    case 'bool':
      return { kind: 'v2_bool', var: cond.var }
    case 'numeric':
      // Numeric conditions can't participate in matrix (they're not enum-valued).
      // Return a placeholder so the outer parser detects and rejects.
      return { kind: 'v2_string', var: 'resolution' }
    case 'param_string_eq':
      return { kind: 'param_string', path: cond.path }
    case 'param_bool':
      return { kind: 'param_bool', path: cond.path }
  }
}

function sameDimensionSource(
  a: MatrixDimensionSource,
  b: MatrixDimensionSource
): boolean {
  if (a.kind !== b.kind) return false
  if (a.kind === 'v2_string' && b.kind === 'v2_string') return a.var === b.var
  if (a.kind === 'v2_bool' && b.kind === 'v2_bool') return a.var === b.var
  if (a.kind === 'param_string' && b.kind === 'param_string') {
    return a.path === b.path
  }
  if (a.kind === 'param_bool' && b.kind === 'param_bool') {
    return a.path === b.path
  }
  return false
}

function conditionValueString(cond: TaskConditionInputV2): string {
  switch (cond.kind) {
    case 'string_eq':
    case 'param_string_eq':
      return cond.value
    case 'bool':
    case 'param_bool':
      return cond.value ? 'true' : 'false'
    case 'numeric':
      return String(cond.value)
  }
}

export function createEmptyMatrix(): VisualMatrixV2 {
  return {
    dimensions: [],
    costUnit: 'per_mtok',
    cells: {},
    preConsumeTokensPerSecond: 0,
    preConsumeTokens: 0,
    preConsumeEstimate: 0,
  }
}

// Helper for the UI: assign a client-side unique id for new dimensions.
let dimIdCounter = 0
export function newDimensionId(): string {
  dimIdCounter += 1
  return `dim-${Date.now().toString(36)}-${dimIdCounter}`
}

// Small helper kept here (rather than in the component) so it can be shared
// between the editor and any future preview.
export function safeStringLiteralInput(value: string): string {
  // Reject characters that would break the generator's simple `"..."` quoting.
  return STRING_LIT_SAFE.test(value) ? value : value.replaceAll(/["\\]/g, '')
}

// ---------------------------------------------------------------------------
// Human-readable helpers (used by the marketplace breakdown and the editor
// tier card). These return typed data — components handle t()/currency
// formatting so this file stays UI-framework-agnostic.
// ---------------------------------------------------------------------------

// A single cost term in a tier body, in a form the UI can render naturally
// (e.g. "每次基础 $0.10" or "每秒 $0.05"). `unitKey` is an i18n key like
// "per second"/"per 1M tokens"; `variableLabel` is the variable name (empty
// for flat) used when the unit is generic (custom cost components).
export type HumanFormulaTerm = {
  amount: number
  unitKey: 'flat' | 'per second' | 'per n' | 'per 1M tokens' | 'per unit'
  // Only set for custom-cost terms. The variable expression is displayed
  // literally (e.g. `param("metadata.frames")`).
  variableExpr?: string
  variableLabel?: string
}

// Well-known task-var display names (i18n keys). Anything not in this map
// falls through to the raw identifier — for `param()`/`header()` we render
// the full expression.
const TASK_VAR_LABEL_KEY: Record<string, string> = {
  seconds: 'seconds',
  n: 'n (count)',
  p: 'input tokens',
  c: 'output tokens',
  len: 'input length',
  cr: 'cache-read tokens',
  cc: 'cache-write tokens',
  resolution: 'resolution',
  size: 'size',
  mode: 'mode',
  has_video: 'input contains video',
  has_image: 'input contains image',
}

export function taskVarLabelKey(variable: string): string {
  return TASK_VAR_LABEL_KEY[variable] ?? variable
}

// i18n key for the per-cell unit suffix in the matrix table — appended after
// each price like "0.1225 / 秒". Empty string for pure flat (no suffix).
export function matrixUnitSuffixKey(unit: MatrixCostUnit): string {
  switch (unit) {
    case 'flat':
      return '/ call'
    case 'per_second':
      return '/ sec'
    case 'per_n':
      return '/ n'
    case 'per_mtok':
      return '/ 1M tokens'
  }
}

export function tierFormulaTerms(tier: VisualTierV2): HumanFormulaTerm[] {
  const out: HumanFormulaTerm[] = []
  if (tier.flat_cost && tier.flat_cost !== 0) {
    out.push({ amount: tier.flat_cost, unitKey: 'flat' })
  }
  if (tier.per_second_cost && tier.per_second_cost !== 0) {
    out.push({ amount: tier.per_second_cost, unitKey: 'per second' })
  }
  if (tier.per_n_cost && tier.per_n_cost !== 0) {
    out.push({ amount: tier.per_n_cost, unitKey: 'per n' })
  }
  if (tier.per_mtok_cost && tier.per_mtok_cost !== 0) {
    out.push({ amount: tier.per_mtok_cost, unitKey: 'per 1M tokens' })
  }
  for (const custom of tier.customCosts || []) {
    if (!custom.variable || !custom.coef) continue
    out.push({
      amount: custom.coef,
      unitKey: 'per unit',
      variableExpr: custom.variable,
      variableLabel: custom.label || undefined,
    })
  }
  return out
}

// Typed shape for the marketplace/editor to render a condition naturally.
// `verb`/`objectKey` are i18n keys; `subjectKey` is the var-label i18n key.
// The renderer joins them as "<subject> <verb> <object>" with locale-specific
// spacing so admins can read `resolution == "1080p" && has_video` as
// "分辨率 = 1080p 且 输入含视频".
export type HumanCondition = {
  subjectKey: string
  subjectRaw?: string // for param()/header() — subjectKey is empty
  verbKey: string
  object: string // literal value shown as-is (may be "true", "1080p", "hd", …)
}

export function conditionHumanForm(cond: TaskConditionInputV2): HumanCondition {
  switch (cond.kind) {
    case 'string_eq':
      return {
        subjectKey: taskVarLabelKey(cond.var),
        verbKey: 'is',
        object: cond.value,
      }
    case 'bool':
      return {
        subjectKey: taskVarLabelKey(cond.var),
        verbKey: cond.value ? 'is present' : 'is absent',
        object: '',
      }
    case 'numeric':
      return {
        subjectKey: taskVarLabelKey(cond.var),
        verbKey: numericOpVerbKey(cond.op),
        object: String(cond.value),
      }
    case 'param_string_eq':
      return {
        subjectKey: '',
        subjectRaw: `param("${cond.path}")`,
        verbKey: 'is',
        object: cond.value,
      }
    case 'param_bool':
      return {
        subjectKey: '',
        subjectRaw: `param("${cond.path}")`,
        verbKey: cond.value ? 'is present' : 'is absent',
        object: '',
      }
  }
}

function numericOpVerbKey(op: '<' | '<=' | '>' | '>=' | '=='): string {
  switch (op) {
    case '<':
      return 'less than'
    case '<=':
      return 'at most'
    case '>':
      return 'greater than'
    case '>=':
      return 'at least'
    case '==':
      return 'equals'
  }
}
