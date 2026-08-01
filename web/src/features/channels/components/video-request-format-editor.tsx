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
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { VideoRequestFormat } from '../types'

type VideoRequestFormatEditorProps = {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  modelOptions?: string[]
}

type FormatRow = {
  id: string
  model: string
  format: VideoRequestFormat
}

const INVALID_DUPLICATE_VALUE = '{"duplicate_video_request_format_model":'

export function VideoRequestFormatEditor(props: VideoRequestFormatEditorProps) {
  const { t } = useTranslation()
  const modelListId = useId()
  const nextRowID = useRef(0)
  const lastEmittedValue = useRef<string | null>(null)
  const [rows, setRows] = useState<FormatRow[]>([])
  const [duplicateModels, setDuplicateModels] = useState<string[]>([])

  const createRowID = () => {
    nextRowID.current += 1
    return `video-format-${nextRowID.current}`
  }

  useEffect(() => {
    if (props.value === lastEmittedValue.current) return
    try {
      const parsed = props.value.trim() ? JSON.parse(props.value) : {}
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
      setRows(
        Object.entries(parsed).flatMap(([model, format]) => {
          if (format !== 'openai' && format !== 'seedance_v3') return []
          return [{ id: createRowID(), model, format }]
        })
      )
      setDuplicateModels([])
    } catch {
      // The form schema reports malformed externally supplied settings.
    }
  }, [props.value])

  const syncRows = (nextRows: FormatRow[]) => {
    setRows(nextRows)
    const seen = new Set<string>()
    const duplicates = new Set<string>()
    for (const row of nextRows) {
      const model = row.model.trim()
      if (!model) continue
      if (seen.has(model)) duplicates.add(model)
      seen.add(model)
    }
    const nextDuplicates = [...duplicates]
    setDuplicateModels(nextDuplicates)

    let value = INVALID_DUPLICATE_VALUE
    if (nextDuplicates.length === 0) {
      const formats: Record<string, VideoRequestFormat> = {}
      for (const row of nextRows) {
        const model = row.model.trim()
        if (model) formats[model] = row.format
      }
      value = JSON.stringify(formats, null, 2)
    }
    lastEmittedValue.current = value
    props.onChange(value)
  }

  const updateRow = (id: string, patch: Partial<FormatRow>) => {
    syncRows(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)))
  }

  return (
    <div className='space-y-3'>
      {duplicateModels.length > 0 ? (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Duplicate model format rules: {{models}}', {
              models: duplicateModels.join(', '),
            })}
          </AlertDescription>
        </Alert>
      ) : null}

      {rows.length > 0 ? (
        <div className='space-y-2'>
          <div className='hidden grid-cols-[minmax(0,1fr)_minmax(10rem,0.6fr)_2.5rem] gap-2 text-sm font-medium sm:grid'>
            <span>{t('Original Model')}</span>
            <span>{t('Upstream request format')}</span>
            <span className='sr-only'>{t('Actions')}</span>
          </div>
          {rows.map((row) => (
            <div
              key={row.id}
              className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(10rem,0.6fr)_2.5rem]'
            >
              <Input
                value={row.model}
                onChange={(event) =>
                  updateRow(row.id, { model: event.target.value })
                }
                placeholder={t('Model name or *')}
                aria-label={t('Original Model')}
                list={modelListId}
                disabled={props.disabled}
              />
              <Select
                value={row.format}
                onValueChange={(format) =>
                  updateRow(row.id, {
                    format: format as VideoRequestFormat,
                  })
                }
                disabled={props.disabled}
              >
                <SelectTrigger aria-label={t('Upstream request format')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    <SelectItem value='seedance_v3'>
                      {t('Seedance V3 native')}
                    </SelectItem>
                    <SelectItem value='openai'>
                      {t('OpenAI compatible')}
                    </SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                onClick={() =>
                  syncRows(rows.filter((item) => item.id !== row.id))
                }
                disabled={props.disabled}
                aria-label={t('Delete format rule')}
              >
                <Trash2 aria-hidden='true' />
              </Button>
            </div>
          ))}
        </div>
      ) : (
        <div className='text-muted-foreground flex min-h-20 items-center justify-center rounded-md border border-dashed px-4 text-center text-sm'>
          {t('No model-specific request formats configured.')}
        </div>
      )}

      <Button
        type='button'
        variant='outline'
        size='sm'
        className='w-full'
        onClick={() =>
          syncRows([
            ...rows,
            {
              id: createRowID(),
              model: '',
              format: 'seedance_v3',
            },
          ])
        }
        disabled={props.disabled}
      >
        <Plus data-icon='inline-start' />
        {t('Add format rule')}
      </Button>

      <datalist id={modelListId}>
        <option value='*' />
        {props.modelOptions?.map((model) => (
          <option key={model} value={model} />
        ))}
      </datalist>
    </div>
  )
}
