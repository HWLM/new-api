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
import { useForm } from 'react-hook-form'
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
import { Input } from '@/components/ui/input'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

const createOpenApiSchema = () =>
  z.object({
    OpenAPIToken: z.string(),
  })

type OpenApiFormValues = z.infer<ReturnType<typeof createOpenApiSchema>>

type OpenApiSettingsSectionProps = {
  defaultValues: OpenApiFormValues
}

export function OpenApiSettingsSection({
  defaultValues,
}: OpenApiSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const schema = createOpenApiSchema()

  const form = useForm<OpenApiFormValues>({
    resolver: zodResolver(schema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (values: OpenApiFormValues) => {
    const sanitized = values.OpenAPIToken.trim()
    const initial = defaultValues.OpenAPIToken.trim()
    if (sanitized === initial) return
    await updateOption.mutateAsync({ key: 'OpenAPIToken', value: sanitized })
  }

  return (
    <SettingsSection title={t('OpenAPI Access')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save OpenAPI settings'
          />
          <FormField
            control={form.control}
            name='OpenAPIToken'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('OpenAPI Token')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    placeholder={t('Enter new token to update')}
                    autoComplete='new-password'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Static token used to authenticate requests to /openapi/* routes. Leave blank to keep the existing value. Clear by saving an empty value (will disable /openapi/* access).'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
