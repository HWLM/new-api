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
  useEffect,
  useImperativeHandle,
  useMemo,
  useState,
  type Ref,
} from 'react'
import * as z from 'zod'
import { GripVertical, Plus, Trash2 } from 'lucide-react'
import { nanoid } from 'nanoid'
import { useFieldArray, useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import {
  CUSTOM_MENU_MAX_ENABLED,
  CUSTOM_MENU_MAX_TOTAL,
  CUSTOM_MENU_NAME_MAX_LEN,
  type CustomMenuPagesConfig,
  isValidCustomMenuUrl,
  serializeCustomMenuPages,
} from './config'
import type { SectionHandle } from './header-navigation-section'

const customMenuItemSchema = z.object({
  id: z.string().min(1),
  name: z
    .string()
    .trim()
    .min(1, 'Menu name is required')
    .refine(
      (v) => Array.from(v.trim()).length <= CUSTOM_MENU_NAME_MAX_LEN,
      `Menu name must be at most ${CUSTOM_MENU_NAME_MAX_LEN} characters`
    ),
  url: z
    .string()
    .trim()
    .min(1, 'Page URL is required')
    .refine(
      isValidCustomMenuUrl,
      'URL must be http(s):// or a site-relative path starting with /'
    ),
  visibleTo: z.enum(['user', 'admin']),
  openMode: z.enum(['iframe', 'newWindow']),
  layoutMode: z.enum(['sidebar', 'fullwidth']),
  enabled: z.boolean(),
})

const formSchema = z.object({
  items: z
    .array(customMenuItemSchema)
    .max(CUSTOM_MENU_MAX_TOTAL, `Up to ${CUSTOM_MENU_MAX_TOTAL} custom menus`)
    .superRefine((items, ctx) => {
      const enabledIdx = items
        .map((it, idx) => (it.enabled ? idx : -1))
        .filter((idx) => idx >= 0)
      if (enabledIdx.length > CUSTOM_MENU_MAX_ENABLED) {
        enabledIdx.forEach((idx) => {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: [idx, 'enabled'],
            message: `Up to ${CUSTOM_MENU_MAX_ENABLED} custom menus can be enabled`,
          })
        })
      }
      const seenIds = new Set<string>()
      items.forEach((it, idx) => {
        if (seenIds.has(it.id)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            path: [idx, 'id'],
            message: 'Duplicate menu id',
          })
        }
        seenIds.add(it.id)
      })
    }),
})

type CustomMenuFormValues = z.infer<typeof formSchema>

type CustomMenuPagesSectionProps = {
  config: CustomMenuPagesConfig
  initialSerialized: string
  hideActions?: boolean
  onPendingChange?: (pending: boolean) => void
  actionsRef?: Ref<SectionHandle>
}

function generateMenuId(): string {
  return nanoid(10)
}

export function CustomMenuPagesSection({
  config,
  initialSerialized,
  hideActions,
  onPendingChange,
  actionsRef,
}: CustomMenuPagesSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const formDefaults = useMemo<CustomMenuFormValues>(
    () => ({ items: config.items }),
    [config]
  )

  const form = useForm<CustomMenuFormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: formDefaults,
    mode: 'onChange',
  })

  const { fields, append, remove, move } = useFieldArray({
    control: form.control,
    name: 'items',
    keyName: '_fieldId',
  })

  useEffect(() => {
    form.reset(formDefaults)
  }, [formDefaults, form])

  const watchedItems = form.watch('items')
  const enabledCount = watchedItems.filter((it) => it.enabled).length
  const totalCount = fields.length
  const totalAtLimit = totalCount >= CUSTOM_MENU_MAX_TOTAL

  const [draggingIdx, setDraggingIdx] = useState<number | null>(null)
  const [dragOverIdx, setDragOverIdx] = useState<number | null>(null)

  const handleDragStart = (idx: number) => (event: React.DragEvent) => {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', String(idx))
    setDraggingIdx(idx)
  }

  const handleDragOver = (idx: number) => (event: React.DragEvent) => {
    event.preventDefault()
    event.dataTransfer.dropEffect = 'move'
    if (dragOverIdx !== idx) {
      setDragOverIdx(idx)
    }
  }

  const handleDragLeave = () => {
    setDragOverIdx(null)
  }

  const handleDrop = (targetIdx: number) => (event: React.DragEvent) => {
    event.preventDefault()
    const sourceIdxStr = event.dataTransfer.getData('text/plain')
    const sourceIdx = Number(sourceIdxStr)
    if (!Number.isFinite(sourceIdx) || sourceIdx === targetIdx) {
      setDraggingIdx(null)
      setDragOverIdx(null)
      return
    }
    move(sourceIdx, targetIdx)
    setDraggingIdx(null)
    setDragOverIdx(null)
  }

  const handleDragEnd = () => {
    setDraggingIdx(null)
    setDragOverIdx(null)
  }

  const handleAdd = () => {
    if (totalAtLimit) {
      toast.warning(t('Up to {{n}} custom menus', { n: CUSTOM_MENU_MAX_TOTAL }))
      return
    }
    append({
      id: generateMenuId(),
      name: '',
      url: '',
      visibleTo: 'user',
      openMode: 'iframe',
      layoutMode: 'sidebar',
      enabled: false,
    })
  }

  const handleToggleEnabled = (
    currentValue: boolean,
    onChange: (next: boolean) => void
  ) => {
    if (!currentValue && enabledCount >= CUSTOM_MENU_MAX_ENABLED) {
      toast.warning(
        t('Up to {{n}} custom menus can be enabled', {
          n: CUSTOM_MENU_MAX_ENABLED,
        })
      )
      return
    }
    onChange(!currentValue)
  }

  const onSubmit = async (values: CustomMenuFormValues) => {
    const payload: CustomMenuPagesConfig = {
      items: values.items.map((it) => ({
        ...it,
        name: it.name.trim(),
        url: it.url.trim(),
      })),
    }
    const serialized = serializeCustomMenuPages(payload)
    if (serialized === initialSerialized) {
      return
    }
    await updateOption.mutateAsync({
      key: 'SidebarCustomMenuPages',
      value: serialized,
    })
  }

  const resetToDefault = () => {
    form.reset({ items: [] })
  }

  useEffect(() => {
    onPendingChange?.(updateOption.isPending)
  }, [updateOption.isPending, onPendingChange])

  useImperativeHandle(
    actionsRef,
    () => ({
      submit: form.handleSubmit(onSubmit),
      reset: resetToDefault,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [form, config, initialSerialized]
  )

  return (
    <SettingsSection title={t('Custom menu pages')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          {!hideActions && (
            <SettingsPageFormActions
              onSave={form.handleSubmit(onSubmit)}
              onReset={resetToDefault}
              isSaving={updateOption.isPending}
              resetLabel='Reset to default'
              saveLabel='Save custom menus'
            />
          )}

          <div className='space-y-1 text-sm'>
            <p className='text-muted-foreground'>
              {t(
                'Add custom iframe pages to the top navigation. Each page can be set as visible to regular users or administrators.'
              )}
            </p>
            {/* <p className='flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400'>
              <AlertTriangle className='mt-0.5 h-3.5 w-3.5 shrink-0' />
              <span>
                {t(
                  'Custom pages load with the highest browser privileges. Only configure URLs you trust.'
                )}
              </span>
            </p> */}
          </div>

          <div className='text-muted-foreground flex items-center justify-between text-xs'>
            <span>
              {t('Total: {{total}}/{{max}}', {
                total: totalCount,
                max: CUSTOM_MENU_MAX_TOTAL,
              })}
            </span>
            <span>
              {t('Enabled: {{enabled}}/{{max}}', {
                enabled: enabledCount,
                max: CUSTOM_MENU_MAX_ENABLED,
              })}
            </span>
          </div>

          <div className='space-y-3'>
            {fields.length === 0 ? (
              <div className='text-muted-foreground rounded-xl border border-dashed py-8 text-center text-sm'>
                {t('No custom menus yet. Click "Add menu item" to create one.')}
              </div>
            ) : (
              fields.map((field, idx) => (
                <div
                  key={field._fieldId}
                  draggable
                  onDragStart={handleDragStart(idx)}
                  onDragOver={handleDragOver(idx)}
                  onDragLeave={handleDragLeave}
                  onDrop={handleDrop(idx)}
                  onDragEnd={handleDragEnd}
                  className={cn(
                    'bg-muted/10 rounded-xl border p-4 transition-colors',
                    draggingIdx === idx && 'opacity-50',
                    dragOverIdx === idx &&
                      draggingIdx !== idx &&
                      'border-primary bg-primary/5'
                  )}
                >
                  <div className='flex items-center justify-between gap-2'>
                    <div className='flex items-center gap-2'>
                      <span className='text-muted-foreground cursor-grab active:cursor-grabbing'>
                        <GripVertical className='h-4 w-4' />
                      </span>
                      <span className='text-sm font-medium'>
                        {t('Menu item #{{n}}', { n: idx + 1 })}
                      </span>
                    </div>
                    <div className='flex items-center gap-3'>
                      <FormField
                        control={form.control}
                        name={`items.${idx}.enabled`}
                        render={({ field: switchField }) => (
                          <FormItem className='flex items-center gap-2 space-y-0'>
                            <FormControl>
                              <Switch
                                checked={switchField.value}
                                onCheckedChange={() =>
                                  handleToggleEnabled(
                                    switchField.value,
                                    switchField.onChange
                                  )
                                }
                              />
                            </FormControl>
                          </FormItem>
                        )}
                      />
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon'
                        className='text-destructive hover:text-destructive h-8 w-8'
                        onClick={() => remove(idx)}
                        aria-label={t('Delete menu item')}
                      >
                        <Trash2 className='h-4 w-4' />
                      </Button>
                    </div>
                  </div>

                  <div className='mt-3 grid gap-3 md:grid-cols-2 lg:grid-cols-4'>
                    <FormField
                      control={form.control}
                      name={`items.${idx}.name`}
                      render={({ field: nameField }) => (
                        <FormItem className='space-y-1'>
                          <Label className='text-xs'>
                            <span className='text-destructive'>* </span>
                            {t('Menu name')}
                          </Label>
                          <FormControl>
                            <Input
                              {...nameField}
                              maxLength={CUSTOM_MENU_NAME_MAX_LEN}
                              placeholder={t(
                                'Enter name, up to {{n}} characters',
                                { n: CUSTOM_MENU_NAME_MAX_LEN }
                              )}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name={`items.${idx}.visibleTo`}
                      render={({ field: roleField }) => (
                        <FormItem className='space-y-1'>
                          <Label className='text-xs'>
                            <span className='text-destructive'>* </span>
                            {t('Visible role')}
                          </Label>
                          <FormControl>
                            <Select
                              items={[
                                { value: 'user', label: t('Regular user') },
                                { value: 'admin', label: t('Administrator') },
                              ]}
                              value={roleField.value}
                              onValueChange={roleField.onChange}
                            >
                              <SelectTrigger className='w-full'>
                                <SelectValue placeholder={t('Please select')} />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value='user'>
                                  {t('Regular user')}
                                </SelectItem>
                                <SelectItem value='admin'>
                                  {t('Administrator')}
                                </SelectItem>
                              </SelectContent>
                            </Select>
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name={`items.${idx}.openMode`}
                      render={({ field: modeField }) => (
                        <FormItem className='space-y-1'>
                          <Label className='text-xs'>
                            <span className='text-destructive'>* </span>
                            {t('Open mode')}
                          </Label>
                          <FormControl>
                            <Select
                              items={[
                                {
                                  value: 'iframe',
                                  label: t('Open in current window'),
                                },
                                {
                                  value: 'newWindow',
                                  label: t('Open in new window'),
                                },
                              ]}
                              value={modeField.value}
                              onValueChange={modeField.onChange}
                            >
                              <SelectTrigger className='w-full'>
                                <SelectValue placeholder={t('Please select')} />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value='iframe'>
                                  {t('Open in current window')}
                                </SelectItem>
                                <SelectItem value='newWindow'>
                                  {t('Open in new window')}
                                </SelectItem>
                              </SelectContent>
                            </Select>
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name={`items.${idx}.layoutMode`}
                      render={({ field: layoutField }) => {
                        const isNewWindow =
                          watchedItems[idx]?.openMode === 'newWindow'
                        return (
                          <FormItem className='space-y-1'>
                            <Label
                              className={cn(
                                'text-xs',
                                isNewWindow && 'text-muted-foreground/60'
                              )}
                            >
                              <span
                                className={cn(
                                  'text-destructive',
                                  isNewWindow && 'opacity-50'
                                )}
                              >
                                *{' '}
                              </span>
                              {t('Layout mode')}
                            </Label>
                            <FormControl>
                              <Select
                                items={[
                                  {
                                    value: 'sidebar',
                                    label: t('Sidebar layout'),
                                  },
                                  {
                                    value: 'fullwidth',
                                    label: t('Full-width layout'),
                                  },
                                ]}
                                value={layoutField.value}
                                onValueChange={layoutField.onChange}
                                disabled={isNewWindow}
                              >
                                <SelectTrigger className='w-full'>
                                  <SelectValue
                                    placeholder={t('Please select')}
                                  />
                                </SelectTrigger>
                                <SelectContent>
                                  <SelectItem value='sidebar'>
                                    {t('Sidebar layout')}
                                  </SelectItem>
                                  <SelectItem value='fullwidth'>
                                    {t('Full-width layout')}
                                  </SelectItem>
                                </SelectContent>
                              </Select>
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )
                      }}
                    />

                    <FormField
                      control={form.control}
                      name={`items.${idx}.url`}
                      render={({ field: urlField }) => (
                        <FormItem className='space-y-1 md:col-span-2 lg:col-span-4'>
                          <Label className='text-xs'>
                            <span className='text-destructive'>* </span>
                            {t('Page URL')}
                          </Label>
                          <FormControl>
                            <Input
                              {...urlField}
                              placeholder='https://example.com  or  /about'
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                </div>
              ))
            )}
          </div>

          <Button
            type='button'
            variant='outline'
            onClick={handleAdd}
            disabled={totalAtLimit}
            className='w-full sm:w-auto'
          >
            <Plus className='mr-1 h-4 w-4' />
            {t('Add menu item')}
          </Button>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
