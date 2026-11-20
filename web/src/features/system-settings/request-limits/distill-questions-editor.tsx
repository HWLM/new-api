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
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  Plus,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import {
  useFieldArray,
  useFormState,
  useWatch,
  type Control,
} from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { charCount } from '@/features/distill-detect/lib'
import {
  nextQuestionId,
  type QuestionFormValue,
} from '@/features/distill-detect/questions'
import { DISTILL_QUESTION_LIMITS } from '@/features/distill-detect/types'

// 只需要表单里有 questions 这一项；用宽类型避免和主表单类型互相引用。
type WithQuestions = { questions: QuestionFormValue[] }

export function DistillQuestionsEditor(props: {
  control: Control<WithQuestions>
}) {
  const { t } = useTranslation()
  const { control } = props
  const { fields, append, remove, move } = useFieldArray({
    control,
    name: 'questions',
  })
  const values = useWatch({ control, name: 'questions' }) ?? []
  const { errors } = useFormState({ control, name: 'questions' })
  // 新增的题默认展开，方便直接填写；已有题默认收起只显示摘要。
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  const add = () => {
    append({
      id: nextQuestionId(values),
      name: '',
      prompt: '',
      enabled: true,
      is_new: true,
    })
  }

  const qErrors = errors.questions as
    | { message?: string; root?: { message?: string } }
    | undefined
  const listError = qErrors?.root?.message ?? qErrors?.message
  const enabledCount = values.filter((q) => q?.enabled).length
  const full = fields.length >= DISTILL_QUESTION_LIMITS.maxCount

  return (
    <div className='flex flex-col gap-3'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Each enabled question is scored separately; the highest score is compared with the threshold. Hit records store scores by question ID.'
        )}{' '}
        {t('{{enabled}} of {{total}} enabled, up to {{max}} questions.', {
          enabled: enabledCount,
          total: fields.length,
          max: DISTILL_QUESTION_LIMITS.maxCount,
        })}
      </p>

      {fields.map((field, index) => {
        const q = values[index]
        const isOpen = expanded.has(field.id) || Boolean(q?.is_new)
        const promptLen = charCount(q?.prompt ?? '')
        return (
          <div
            key={field.id}
            className='flex flex-col gap-3 rounded-lg border p-3'
          >
            <div className='flex items-start gap-2'>
              <Button
                type='button'
                size='icon'
                variant='ghost'
                className='size-7 shrink-0'
                onClick={() => toggle(field.id)}
                aria-label={isOpen ? t('Collapse') : t('Expand')}
                title={isOpen ? t('Collapse') : t('Expand')}
              >
                {isOpen ? (
                  <ChevronDown className='size-4' />
                ) : (
                  <ChevronRight className='size-4' />
                )}
              </Button>
              <button
                type='button'
                className='flex min-w-0 flex-1 flex-col gap-1 text-left'
                onClick={() => toggle(field.id)}
              >
                <span className='flex flex-wrap items-center gap-2 text-sm font-medium'>
                  <span className='text-muted-foreground tabular-nums'>
                    {index + 1}.
                  </span>
                  {q?.name?.trim() || t('Untitled question')}
                  <code className='text-muted-foreground font-mono text-xs'>
                    {q?.id}
                  </code>
                  {q?.is_new ? (
                    <Badge variant='outline'>{t('New')}</Badge>
                  ) : null}
                  {q?.enabled ? null : (
                    <Badge variant='secondary'>{t('Disabled')}</Badge>
                  )}
                </span>
                {isOpen ? null : (
                  <span className='text-muted-foreground line-clamp-2 text-xs whitespace-pre-wrap'>
                    {q?.prompt || t('No question text yet')}
                  </span>
                )}
              </button>
              <div className='flex shrink-0 items-center gap-1'>
                <FormField
                  control={control}
                  name={`questions.${index}.enabled`}
                  render={({ field: f }) => (
                    <Switch
                      checked={Boolean(f.value)}
                      onCheckedChange={f.onChange}
                      aria-label={t('Enabled')}
                      title={t('Enabled')}
                    />
                  )}
                />
                <Button
                  type='button'
                  size='icon'
                  variant='ghost'
                  className='size-7'
                  disabled={index === 0}
                  onClick={() => move(index, index - 1)}
                  aria-label={t('Move up')}
                  title={t('Move up')}
                >
                  <ArrowUp className='size-4' />
                </Button>
                <Button
                  type='button'
                  size='icon'
                  variant='ghost'
                  className='size-7'
                  disabled={index === fields.length - 1}
                  onClick={() => move(index, index + 1)}
                  aria-label={t('Move down')}
                  title={t('Move down')}
                >
                  <ArrowDown className='size-4' />
                </Button>
                <Button
                  type='button'
                  size='icon'
                  variant='ghost'
                  className='text-destructive size-7'
                  disabled={fields.length <= 1}
                  onClick={() => remove(index)}
                  aria-label={t('Delete question')}
                  title={t('Delete question')}
                >
                  <Trash2 className='size-4' />
                </Button>
              </div>
            </div>

            {/* 收起时字段仍需挂载以显示校验错误，所以只隐藏不卸载。 */}
            <div className={isOpen ? 'flex flex-col gap-3 pl-9' : 'hidden'}>
              <div className='grid gap-3 md:grid-cols-2'>
                <FormField
                  control={control}
                  name={`questions.${index}.name`}
                  render={({ field: f }) => (
                    <FormItem>
                      <FormLabel>{t('Question name')}</FormLabel>
                      <FormControl>
                        <Input {...f} autoComplete='off' maxLength={64} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={control}
                  name={`questions.${index}.id`}
                  render={({ field: f }) => (
                    <FormItem>
                      <FormLabel>{t('Question ID')}</FormLabel>
                      <FormControl>
                        <Input
                          {...f}
                          autoComplete='off'
                          maxLength={32}
                          readOnly={!q?.is_new}
                          className='font-mono'
                        />
                      </FormControl>
                      <p className='text-muted-foreground text-xs'>
                        {q?.is_new
                          ? t(
                              'Lowercase letters, digits and _. Cannot be changed after saving.'
                            )
                          : t(
                              'Fixed after saving so past hit records keep their meaning.'
                            )}
                      </p>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
              <FormField
                control={control}
                name={`questions.${index}.prompt`}
                render={({ field: f }) => (
                  <FormItem>
                    <FormLabel className='flex items-center justify-between'>
                      {t('Question text')}
                      <span
                        className={
                          promptLen > DISTILL_QUESTION_LIMITS.promptMax
                            ? 'text-destructive text-xs tabular-nums'
                            : 'text-muted-foreground text-xs tabular-nums'
                        }
                      >
                        {promptLen} / {DISTILL_QUESTION_LIMITS.promptMax}
                      </span>
                    </FormLabel>
                    <FormControl>
                      <Textarea
                        {...f}
                        rows={10}
                        className='font-mono text-xs leading-relaxed'
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>
        )
      })}

      {listError ? (
        <p className='text-destructive text-sm'>{t(listError)}</p>
      ) : null}

      <div>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={add}
          disabled={full}
        >
          <Plus className='size-4' />
          {t('Add question')}
        </Button>
      </div>
    </div>
  )
}
