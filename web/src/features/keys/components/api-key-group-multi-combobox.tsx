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
import { useMemo, useState } from 'react'
import { Check, ChevronsUpDown, GripVertical, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { type ApiKeyGroupOption } from './api-key-group-combobox'

type ApiKeyGroupMultiComboboxProps = {
  options: ApiKeyGroupOption[]
  value: string[]
  onValueChange: (value: string[]) => void
  placeholder?: string
  disabled?: boolean
}

function formatGroupRatio(
  ratio: ApiKeyGroupOption['ratio'],
  ratioLabel: string
) {
  if (ratio === undefined || ratio === null || ratio === '') return null
  return `${ratio}x ${ratioLabel}`
}

function getRatioBadgeClassName(ratio: ApiKeyGroupOption['ratio']) {
  if (typeof ratio !== 'number') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300'
  }
  if (ratio > 5) {
    return 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-300'
  }
  if (ratio > 3) {
    return 'border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900/60 dark:bg-orange-950/40 dark:text-orange-300'
  }
  if (ratio > 1) {
    return 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900/60 dark:bg-blue-950/40 dark:text-blue-300'
  }
  return 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300'
}

function GroupRatioBadge({ ratio }: { ratio: ApiKeyGroupOption['ratio'] }) {
  const { t } = useTranslation()
  const label = formatGroupRatio(ratio, t('Ratio'))
  if (!label) return null
  return (
    <Badge
      variant='outline'
      className={cn(
        'max-w-24 shrink-0 truncate text-[10px] sm:max-w-none sm:text-xs',
        getRatioBadgeClassName(ratio)
      )}
    >
      {label}
    </Badge>
  )
}

export function ApiKeyGroupMultiCombobox({
  options,
  value,
  onValueChange,
  placeholder,
  disabled,
}: ApiKeyGroupMultiComboboxProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [searchValue, setSearchValue] = useState('')
  const [draggedValue, setDraggedValue] = useState<string | null>(null)
  const [dragOverValue, setDragOverValue] = useState<string | null>(null)

  const optionByValue = useMemo(() => {
    const map = new Map<string, ApiKeyGroupOption>()
    for (const o of options) map.set(o.value, o)
    return map
  }, [options])

  const selectedSet = useMemo(() => new Set(value), [value])
  // 已选值按用户选中顺序展示(顺序即优先级)
  const selectedOptions = useMemo(
    () =>
      value
        .map((v) => optionByValue.get(v))
        .filter((o): o is ApiKeyGroupOption => !!o),
    [value, optionByValue]
  )

  const filteredOptions = useMemo(() => {
    const search = searchValue.trim().toLowerCase()
    if (!search) return options
    return options.filter((option) => {
      const ratioText = String(option.ratio ?? '').toLowerCase()
      return (
        option.value.toLowerCase().includes(search) ||
        option.label.toLowerCase().includes(search) ||
        option.desc?.toLowerCase().includes(search) ||
        ratioText.includes(search)
      )
    })
  }, [options, searchValue])

  const toggle = (val: string) => {
    if (selectedSet.has(val)) {
      onValueChange(value.filter((v) => v !== val))
    } else {
      onValueChange([...value, val])
    }
  }

  const removeAt = (idx: number) => {
    const next = value.slice()
    next.splice(idx, 1)
    onValueChange(next)
  }

  const moveValue = (fromValue: string, toValue: string) => {
    if (fromValue === toValue) return
    const fromIndex = value.indexOf(fromValue)
    const toIndex = value.indexOf(toValue)
    if (fromIndex === -1 || toIndex === -1) return

    const next = value.slice()
    const [moved] = next.splice(fromIndex, 1)
    next.splice(toIndex, 0, moved)
    onValueChange(next)
  }

  const resetDragState = () => {
    setDraggedValue(null)
    setDragOverValue(null)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            role='combobox'
            aria-expanded={open}
            disabled={disabled}
            className='border-input bg-muted/40 hover:bg-muted/55 hover:text-foreground active:bg-background data-popup-open:border-ring data-popup-open:bg-background data-popup-open:ring-ring/20 h-auto min-h-14 w-full justify-between gap-2 rounded-lg px-3 py-2 text-start shadow-none transition-[background-color,border-color,box-shadow] duration-150 data-popup-open:ring-[3px] sm:min-h-20 sm:gap-3 sm:px-4 sm:py-3'
          />
        }
      >
        <span className='flex min-w-0 flex-1 flex-wrap items-center gap-1.5'>
          {selectedOptions.length === 0 ? (
            <span className='text-muted-foreground block truncate font-medium'>
              {placeholder || t('Select groups (priority order)')}
            </span>
          ) : (
            selectedOptions.map((option, idx) => (
              <span
                key={option.value}
                draggable={!disabled && selectedOptions.length > 1}
                onDragStart={(event) => {
                  event.stopPropagation()
                  setDraggedValue(option.value)
                  event.dataTransfer.effectAllowed = 'move'
                  event.dataTransfer.setData('text/plain', option.value)
                }}
                onDragOver={(event) => {
                  if (disabled || !draggedValue) return
                  event.preventDefault()
                  event.stopPropagation()
                  event.dataTransfer.dropEffect = 'move'
                  setDragOverValue(option.value)
                }}
                onDrop={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  const fromValue =
                    draggedValue || event.dataTransfer.getData('text/plain')
                  if (fromValue) moveValue(fromValue, option.value)
                  resetDragState()
                }}
                onDragEnd={resetDragState}
                className={cn(
                  'bg-background ring-border inline-flex max-w-[180px] items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium ring-1 ring-inset transition-[box-shadow,opacity]',
                  !disabled &&
                    selectedOptions.length > 1 &&
                    'cursor-grab active:cursor-grabbing',
                  draggedValue === option.value && 'opacity-60',
                  dragOverValue === option.value &&
                    draggedValue !== option.value &&
                    'ring-ring shadow-sm'
                )}
              >
                {!disabled && selectedOptions.length > 1 && (
                  <GripVertical
                    aria-hidden='true'
                    className='text-muted-foreground h-3 w-3 shrink-0'
                  />
                )}
                <span className='text-muted-foreground tabular-nums text-[10px]'>
                  {idx + 1}.
                </span>
                <span className='truncate'>{option.label}</span>
                {!disabled && (
                  <button
                    type='button'
                    aria-label={t('Remove')}
                    onClick={(e) => {
                      e.stopPropagation()
                      e.preventDefault()
                      removeAt(idx)
                    }}
                    className='text-muted-foreground hover:text-foreground -mr-0.5 inline-flex h-3.5 w-3.5 items-center justify-center rounded-sm'
                  >
                    <X className='h-3 w-3' />
                  </button>
                )}
              </span>
            ))
          )}
        </span>
        <ChevronsUpDown className='h-4 w-4 shrink-0 opacity-50' />
      </PopoverTrigger>
      <PopoverContent
        className='data-closed:zoom-out-100 data-open:zoom-in-100 data-[side=bottom]:slide-in-from-top-0 data-[side=left]:slide-in-from-right-0 data-[side=right]:slide-in-from-left-0 data-[side=top]:slide-in-from-bottom-0 w-[var(--anchor-width)] overflow-hidden rounded-xl p-0 shadow-lg data-closed:duration-75 data-open:duration-100'
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('Search...')}
            value={searchValue}
            onValueChange={setSearchValue}
          />
          <CommandList className='max-h-[360px]'>
            <CommandEmpty>{t('No group found.')}</CommandEmpty>
            <CommandGroup>
              {filteredOptions.map((option) => {
                const isSelected = selectedSet.has(option.value)
                return (
                  <CommandItem
                    key={option.value}
                    value={option.value}
                    onSelect={() => toggle(option.value)}
                    className='data-[selected=true]:bg-muted items-start gap-3 rounded-lg px-3 py-3 transition-colors'
                  >
                    <Check
                      className={cn(
                        'mt-0.5 h-4 w-4',
                        isSelected ? 'opacity-100' : 'opacity-0'
                      )}
                    />
                    <span className='min-w-0 flex-1'>
                      <span className='block truncate font-medium'>
                        {option.label}
                      </span>
                      {option.desc && (
                        <span className='text-muted-foreground block truncate text-xs'>
                          {option.desc}
                        </span>
                      )}
                    </span>
                    <GroupRatioBadge ratio={option.ratio} />
                  </CommandItem>
                )
              })}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
