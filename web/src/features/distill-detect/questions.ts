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
import * as z from 'zod'

import { charCount } from './lib'
import { DISTILL_QUESTION_LIMITS, type DistillQuestion } from './types'

// 校验与 §14.1 一致（后端强制，这里同步）：1..20 题、id 唯一且 [a-z0-9_]{1,32}、
// name 1..64、prompt 1..8000、至少 1 题启用。
const questionSchema = z.object({
  id: z.string().trim().regex(DISTILL_QUESTION_LIMITS.idPattern, {
    message: 'ID: 1–32 chars of a-z, 0-9 or _',
  }),
  name: z
    .string()
    .trim()
    .refine((v) => charCount(v) >= 1, { message: 'Name is required' })
    .refine((v) => charCount(v) <= DISTILL_QUESTION_LIMITS.nameMax, {
      message: 'Name: at most 64 characters',
    }),
  prompt: z
    .string()
    .refine((v) => v.trim() !== '', { message: 'Question text is required' })
    .refine((v) => charCount(v) <= DISTILL_QUESTION_LIMITS.promptMax, {
      message: 'Question text: at most 8000 characters',
    }),
  enabled: z.boolean(),
  /** 仅前端：新增且未保存的题允许改 id；提交前剥掉。 */
  is_new: z.boolean(),
})

export const questionsSchema = z
  .array(questionSchema)
  .min(1, { message: 'Keep at least one question' })
  .max(DISTILL_QUESTION_LIMITS.maxCount, {
    message: 'At most 20 questions',
  })
  .superRefine((items, ctx) => {
    const seen = new Map<string, number>()
    items.forEach((q, i) => {
      const id = q.id.trim()
      if (seen.has(id)) {
        ctx.addIssue({
          code: 'custom',
          path: [i, 'id'],
          message: 'Duplicate question ID',
        })
      } else {
        seen.set(id, i)
      }
    })
    if (items.length > 0 && !items.some((q) => q.enabled)) {
      ctx.addIssue({
        code: 'custom',
        path: [],
        message: 'Enable at least one question, otherwise judging stops',
      })
    }
  })

export type QuestionFormValue = z.infer<typeof questionSchema>

export function toQuestionFormValues(
  questions: DistillQuestion[] | undefined
): QuestionFormValue[] {
  return (questions ?? []).map((q) => ({ ...q, is_new: false }))
}

/** 去掉前端标记、修剪空白，得到 PUT 用的题库数组。 */
export function toQuestionPayload(
  values: QuestionFormValue[]
): DistillQuestion[] {
  return values.map((q) => ({
    id: q.id.trim(),
    name: q.name.trim(),
    prompt: q.prompt,
    enabled: q.enabled,
  }))
}

/** 新增题目的默认 id：question_<n>，跳过已占用的。 */
export function nextQuestionId(existing: QuestionFormValue[]): string {
  const used = new Set(existing.map((q) => q.id))
  for (let n = existing.length + 1; ; n++) {
    const id = `question_${n}`
    if (!used.has(id)) return id
  }
}
