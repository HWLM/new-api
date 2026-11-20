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

import type {
  DistillAlertChannelResult,
  DistillHit,
  DistillQuestion,
  DistillQuestionResult,
} from './types'

export type AlertOutcome = {
  kind: 'sent' | 'disabled' | 'not_configured' | 'failed'
  detail: string
}

/** 单通道测试结果 → 展示用状态；未知 status 按失败处理并把原值带出来。 */
export function describeAlertResult(
  raw: DistillAlertChannelResult | undefined
): AlertOutcome {
  const status = raw?.status ?? ''
  if (status === 'sent') return { kind: 'sent', detail: '' }
  if (status === 'disabled') return { kind: 'disabled', detail: '' }
  if (status === 'not_configured') {
    return { kind: 'not_configured', detail: '' }
  }
  return { kind: 'failed', detail: raw?.error || status }
}

/**
 * 一行命中记录的逐题结果，按分数降序。
 * 优先用 question_results 快照；为空时（老记录且后端没兜底）从 categories
 * 拼：题名取当前题库，「命中」只能推断为最高分那题（该行之所以存在，是最高分过了阈值）。
 */
export function resolveQuestionResults(
  hit: DistillHit,
  questions: DistillQuestion[] | undefined
): DistillQuestionResult[] {
  if (hit.question_results && hit.question_results.length > 0) {
    return [...hit.question_results].sort((a, b) => b.score - a.score)
  }
  const scores = parseCategories(hit.categories)
  const top = scores[0]?.[1]
  return scores.map(([id, score]) => ({
    id,
    name: questionLabel(id, questions),
    score,
    hit: top !== undefined && score === top,
  }))
}

/** 题目 id → 展示名；题库里找不到（已删除的题）就显示 id 本身。 */
export function questionLabel(
  id: string,
  questions: DistillQuestion[] | undefined
): string {
  const found = questions?.find((q) => q.id === id)
  return found?.name ? found.name : id
}

/** 按 Unicode 字符数计长度，与后端 utf8.RuneCountInString 对齐。 */
export function charCount(value: string): number {
  return [...value].length
}

/** categories 是逐题分数的 JSON 文本；解析失败返回空数组，不影响主分数展示。按分数降序。 */
export function parseCategories(raw: string): Array<[string, number]> {
  if (!raw) {
    return []
  }
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return []
    }
    return Object.entries(parsed as Record<string, unknown>)
      .filter(
        (entry): entry is [string, number] => typeof entry[1] === 'number'
      )
      .sort((a, b) => b[1] - a[1])
  } catch {
    return []
  }
}
