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
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const refusalDemoteSchema = z.object({
  'refusal_demote_setting.enabled': z.boolean(),
  'refusal_demote_setting.window_size': z.number().int().min(1).max(10000),
  'refusal_demote_setting.min_samples': z.number().int().min(1).max(10000),
  'refusal_demote_setting.threshold_pct': z.number().int().min(1).max(100),
  'refusal_demote_setting.demote_seconds': z.number().int().min(60).max(86400),
  'refusal_demote_setting.window_ttl_seconds': z
    .number()
    .int()
    .min(60)
    .max(604800),
})

type RefusalDemoteFormValues = z.infer<typeof refusalDemoteSchema>

type RefusalDemoteSectionProps = {
  defaultValues: RefusalDemoteFormValues
}

export function RefusalDemoteSection({
  defaultValues,
}: RefusalDemoteSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<RefusalDemoteFormValues>({
    resolver: zodResolver(refusalDemoteSchema),
    mode: 'onChange',
    defaultValues,
  })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const onSubmit = async (values: RefusalDemoteFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof RefusalDemoteFormValues]
    )
    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value: value ?? '' })
    }
  }

  return (
    <SettingsSection title={t('Refusal rate demotion')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save refusal demotion'
          />
          <FormField
            control={form.control}
            name='refusal_demote_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable refusal rate demotion')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When a user hits the refusal ratio over their recent requests, route their traffic to the per-channel fallback for the configured duration. Only Claude stop_reason=refusal responses are counted.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='refusal_demote_setting.window_size'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Window size')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        max={10000}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 1)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('requests')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t('Last N text-class requests used to compute the ratio.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='refusal_demote_setting.min_samples'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum samples')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        max={10000}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 1)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('requests')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Window must contain at least this many samples before any demotion fires.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='refusal_demote_setting.threshold_pct'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Refusal ratio threshold')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={1}
                        max={100}
                        step={1}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 1)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>%</span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Demote when refused / total in the window reaches this percentage.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='refusal_demote_setting.demote_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Demotion duration')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={60}
                        max={86400}
                        step={60}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 60)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('seconds')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t(
                      'How long a demoted user keeps hitting the fallback channel. Default 3600 (1 hour).'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='refusal_demote_setting.window_ttl_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Window TTL')}</FormLabel>
                  <FormControl>
                    <div className='flex items-center gap-2'>
                      <Input
                        type='number'
                        min={60}
                        max={604800}
                        step={60}
                        {...field}
                        onChange={(e) =>
                          field.onChange(Number.parseInt(e.target.value) || 60)
                        }
                      />
                      <span className='text-muted-foreground text-sm'>
                        {t('seconds')}
                      </span>
                    </div>
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Idle TTL on the per-user Redis window; prevents long-dormant users from piling up.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
