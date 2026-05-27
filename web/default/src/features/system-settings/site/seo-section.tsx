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
import type { Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Textarea } from '@/components/ui/textarea'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const seoSchema = z.object({
  MetaDescription: z.string().optional(),
  AnalyticsScript: z.string().optional(),
})

type SeoFormValues = z.infer<typeof seoSchema>

type SeoSectionProps = {
  defaultValues: SeoFormValues
}

function normalizeValue(value: unknown): string {
  if (value === undefined || value === null) return ''
  return typeof value === 'string' ? value : String(value)
}

export function SeoSection({ defaultValues }: SeoSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const normalizedDefaults: SeoFormValues = {
    MetaDescription: normalizeValue(defaultValues.MetaDescription),
    AnalyticsScript: normalizeValue(defaultValues.AnalyticsScript),
  }

  const { form, handleSubmit, handleReset, isDirty, isSubmitting } =
    useSettingsForm<SeoFormValues>({
      resolver: zodResolver(seoSchema) as Resolver<
        SeoFormValues,
        unknown,
        SeoFormValues
      >,
      defaultValues: normalizedDefaults,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: normalizeValue(value),
          })
        }
      },
    })

  return (
    <>
      <FormNavigationGuard when={isDirty} />

      <SettingsSection title={t('SEO')}>
        <Form {...form}>
          <SettingsForm onSubmit={handleSubmit}>
            <SettingsPageFormActions
              onSave={handleSubmit}
              onReset={handleReset}
              isSaving={isSubmitting || updateOption.isPending}
              isResetDisabled={!isDirty}
            />
            <FormDirtyIndicator isDirty={isDirty} />

            <FormField
              control={form.control}
              name='MetaDescription'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Meta Description')}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t(
                        'Briefly describe your site for search engines and social previews'
                      )}
                      rows={4}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Used for the page meta description. Leave empty to keep the default browser snippet behavior.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='AnalyticsScript'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Analytics Script')}</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t(
                        'Paste your Baidu Analytics script or other site analytics snippet'
                      )}
                      rows={10}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'The script will be injected into the site as provided. Paste the full snippet from your analytics provider.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsForm>
        </Form>
      </SettingsSection>
    </>
  )
}
