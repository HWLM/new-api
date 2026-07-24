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
import { Settings2 } from 'lucide-react'
import { useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import type { ChannelFormValues } from '../lib/channel-form'
import type { SeedanceV3RouteMethod } from '../types'

const METHOD_OPTIONS: SeedanceV3RouteMethod[] = ['GET', 'POST', 'PUT', 'PATCH']

type RouteID = 'asset_create' | 'task_create' | 'task_get'

type RouteDraft = {
  method: SeedanceV3RouteMethod
  target: string
  parameters: string
  responseMapping: string
}

type RouteDrafts = Record<RouteID, RouteDraft>

const ROUTES: Array<{
  id: RouteID
  label: string
  methodField:
    | 'seedance_asset_create_method'
    | 'seedance_task_create_method'
    | 'seedance_task_get_method'
  targetField:
    | 'seedance_asset_create_target'
    | 'seedance_task_create_target'
    | 'seedance_task_get_target'
  parametersField:
    | 'seedance_asset_create_parameters'
    | 'seedance_task_create_parameters'
    | 'seedance_task_get_parameters'
  responseMappingField:
    | 'seedance_asset_create_response_mapping'
    | 'seedance_task_create_response_mapping'
    | 'seedance_task_get_response_mapping'
  defaultMethod: SeedanceV3RouteMethod
  targetPlaceholder: string
  byteplusTargetPlaceholder: string
}> = [
  {
    id: 'asset_create',
    label: 'Asset creation',
    methodField: 'seedance_asset_create_method',
    targetField: 'seedance_asset_create_target',
    parametersField: 'seedance_asset_create_parameters',
    responseMappingField: 'seedance_asset_create_response_mapping',
    defaultMethod: 'POST',
    targetPlaceholder: '/v3/open/CreateAsset',
    byteplusTargetPlaceholder: '/v1/sd/assets',
  },
  {
    id: 'task_create',
    label: 'Task creation',
    methodField: 'seedance_task_create_method',
    targetField: 'seedance_task_create_target',
    parametersField: 'seedance_task_create_parameters',
    responseMappingField: 'seedance_task_create_response_mapping',
    defaultMethod: 'POST',
    targetPlaceholder: '/api/v3/contents/generations/tasks',
    byteplusTargetPlaceholder: '/v1/video/generate',
  },
  {
    id: 'task_get',
    label: 'Task query',
    methodField: 'seedance_task_get_method',
    targetField: 'seedance_task_get_target',
    parametersField: 'seedance_task_get_parameters',
    responseMappingField: 'seedance_task_get_response_mapping',
    defaultMethod: 'GET',
    targetPlaceholder: '/api/v3/contents/generations/tasks/{task_id}',
    byteplusTargetPlaceholder: '/v1/video/tasks/{task_id}',
  },
]

type SeedanceRouteFieldsProps = {
  form: UseFormReturn<ChannelFormValues>
  channelType: number
}

function getRouteDrafts(form: UseFormReturn<ChannelFormValues>): RouteDrafts {
  return Object.fromEntries(
    ROUTES.map((route) => [
      route.id,
      {
        method: form.getValues(route.methodField) || route.defaultMethod,
        target: form.getValues(route.targetField) || '',
        parameters: form.getValues(route.parametersField) || '{}',
        responseMapping: form.getValues(route.responseMappingField) || '{}',
      },
    ])
  ) as RouteDrafts
}

export function SeedanceRouteFields(props: SeedanceRouteFieldsProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [drafts, setDrafts] = useState<RouteDrafts>(() =>
    getRouteDrafts(props.form)
  )
  const [assetBaseURL, setAssetBaseURL] = useState(
    () => props.form.getValues('asset_base_url') || ''
  )
  const [parameterErrors, setParameterErrors] = useState<
    Partial<Record<RouteID, string>>
  >({})
  const [responseMappingErrors, setResponseMappingErrors] = useState<
    Partial<Record<RouteID, string>>
  >({})

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setDrafts(getRouteDrafts(props.form))
      setAssetBaseURL(props.form.getValues('asset_base_url') || '')
      setParameterErrors({})
      setResponseMappingErrors({})
    }
    setOpen(nextOpen)
  }

  const updateDraft = (routeID: RouteID, patch: Partial<RouteDraft>) => {
    setDrafts((current) => ({
      ...current,
      [routeID]: { ...current[routeID], ...patch },
    }))
    if (patch.parameters !== undefined) {
      setParameterErrors((current) => ({ ...current, [routeID]: undefined }))
    }
    if (patch.responseMapping !== undefined) {
      setResponseMappingErrors((current) => ({
        ...current,
        [routeID]: undefined,
      }))
    }
  }

  const saveRoutes = () => {
    const errors: Partial<Record<RouteID, string>> = {}
    const mappingErrors: Partial<Record<RouteID, string>> = {}
    for (const route of ROUTES) {
      try {
        const parsed = JSON.parse(drafts[route.id].parameters.trim() || '{}')
        if (
          typeof parsed !== 'object' ||
          parsed === null ||
          Array.isArray(parsed)
        ) {
          errors[route.id] = t('Parameters must be a JSON object')
        }
      } catch {
        errors[route.id] = t('Invalid JSON')
      }
      try {
        const parsed = JSON.parse(
          drafts[route.id].responseMapping.trim() || '{}'
        )
        if (
          typeof parsed !== 'object' ||
          parsed === null ||
          Array.isArray(parsed)
        ) {
          mappingErrors[route.id] = t('Response mapping must be a JSON object')
        }
      } catch {
        mappingErrors[route.id] = t('Invalid JSON')
      }
    }
    if (
      Object.keys(errors).length > 0 ||
      Object.keys(mappingErrors).length > 0
    ) {
      setParameterErrors(errors)
      setResponseMappingErrors(mappingErrors)
      return
    }

    for (const route of ROUTES) {
      const draft = drafts[route.id]
      props.form.setValue(route.methodField, draft.method, {
        shouldDirty: true,
        shouldValidate: true,
      })
      props.form.setValue(route.targetField, draft.target.trim(), {
        shouldDirty: true,
        shouldValidate: true,
      })
      props.form.setValue(
        route.parametersField,
        draft.parameters.trim() || '{}',
        { shouldDirty: true, shouldValidate: true }
      )
      props.form.setValue(
        route.responseMappingField,
        draft.responseMapping.trim() || '{}',
        { shouldDirty: true, shouldValidate: true }
      )
    }
    if (props.channelType === 81) {
      props.form.setValue('asset_base_url', assetBaseURL.trim(), {
        shouldDirty: true,
        shouldValidate: true,
      })
    }
    setOpen(false)
  }

  return (
    <>
      <Field orientation='responsive'>
        <FieldContent>
          <FieldTitle>{t('Seedance API Routes')}</FieldTitle>
          <FieldDescription>
            {t(
              'Configure each upstream method, URL, and parameter override independently.'
            )}
          </FieldDescription>
        </FieldContent>
        <Button
          type='button'
          variant='outline'
          onClick={() => handleOpenChange(true)}
        >
          <Settings2 data-icon='inline-start' />
          {t('Configure')}
        </Button>
      </Field>

      <Dialog
        open={open}
        onOpenChange={handleOpenChange}
        title={t('Seedance API Routes')}
        description={t(
          'Configure each upstream method, URL, and parameter override independently.'
        )}
        contentClassName='sm:max-w-3xl'
        contentHeight='min(68vh, 620px)'
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => setOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button type='button' onClick={saveRoutes}>
              {t('Save')}
            </Button>
          </>
        }
      >
        {props.channelType === 81 ? (
          <Field className='mb-4'>
            <FieldLabel htmlFor='seedance-asset-base-url'>
              {t('Asset Base URL')}
            </FieldLabel>
            <Input
              id='seedance-asset-base-url'
              value={assetBaseURL}
              onChange={(event) => setAssetBaseURL(event.target.value)}
            />
            <FieldDescription>
              {t(
                'Optional. Base URL for /v3/open/CreateAsset and /v3/open/GetAsset. Empty means fall back to the main Base URL (reseller scenario).'
              )}
            </FieldDescription>
          </Field>
        ) : null}

        <Tabs defaultValue='asset_create'>
          <TabsList className='grid h-auto w-full grid-cols-3'>
            {ROUTES.map((route) => (
              <TabsTrigger
                key={route.id}
                value={route.id}
                className='min-h-8 whitespace-normal'
              >
                {t(route.label)}
              </TabsTrigger>
            ))}
          </TabsList>

          {ROUTES.map((route) => {
            const draft = drafts[route.id]
            const parameterError = parameterErrors[route.id]
            const responseMappingError = responseMappingErrors[route.id]
            return (
              <TabsContent key={route.id} value={route.id} className='pt-3'>
                <FieldGroup>
                  <div className='grid gap-4 sm:grid-cols-[8rem_minmax(0,1fr)]'>
                    <Field>
                      <FieldLabel htmlFor={`${route.id}-method`}>
                        {t('HTTP Method')}
                      </FieldLabel>
                      <Select
                        value={draft.method}
                        onValueChange={(value) =>
                          updateDraft(route.id, {
                            method: value as SeedanceV3RouteMethod,
                          })
                        }
                      >
                        <SelectTrigger id={`${route.id}-method`}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {METHOD_OPTIONS.map((method) => (
                              <SelectItem key={method} value={method}>
                                {method}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </Field>

                    <Field>
                      <FieldLabel htmlFor={`${route.id}-target`}>
                        {t('Upstream URL')}
                      </FieldLabel>
                      <Input
                        id={`${route.id}-target`}
                        value={draft.target}
                        placeholder={
                          props.channelType === 81
                            ? route.byteplusTargetPlaceholder
                            : route.targetPlaceholder
                        }
                        onChange={(event) =>
                          updateDraft(route.id, { target: event.target.value })
                        }
                      />
                    </Field>
                  </div>

                  <Field data-invalid={Boolean(parameterError)}>
                    <FieldLabel htmlFor={`${route.id}-parameters`}>
                      {t('Parameters')}
                    </FieldLabel>
                    <FieldDescription>
                      {t(
                        'Current request parameters are used by default. This JSON object recursively overrides them; null removes a field.'
                      )}
                    </FieldDescription>
                    <FieldDescription>
                      {t(
                        'Use an exact {field} placeholder to copy a value from the current request, for example {url} or {content.0.text}. Objects and arrays keep their original types.'
                      )}
                    </FieldDescription>
                    <JsonCodeEditor
                      id={`${route.id}-parameters`}
                      value={draft.parameters}
                      onChange={(parameters) =>
                        updateDraft(route.id, { parameters })
                      }
                      aria-invalid={Boolean(parameterError)}
                      heightClassName='h-64 min-h-64 max-h-64'
                    />
                    {route.id === 'task_get' ? (
                      <FieldDescription>
                        {t(
                          'Use {task_id} in the URL or parameter values for the upstream task ID.'
                        )}
                      </FieldDescription>
                    ) : null}
                    {parameterError ? (
                      <p className='text-destructive text-sm'>
                        {parameterError}
                      </p>
                    ) : null}
                  </Field>

                  <Field data-invalid={Boolean(responseMappingError)}>
                    <FieldLabel htmlFor={`${route.id}-response-mapping`}>
                      {t('Response Mapping')}
                    </FieldLabel>
                    <FieldDescription>
                      {t(
                        'Map upstream response fields before existing response parsing. Use exact {field} placeholders to copy values, null removes a field, and an empty object keeps the response unchanged.'
                      )}
                    </FieldDescription>
                    <JsonCodeEditor
                      id={`${route.id}-response-mapping`}
                      value={draft.responseMapping}
                      onChange={(responseMapping) =>
                        updateDraft(route.id, { responseMapping })
                      }
                      aria-invalid={Boolean(responseMappingError)}
                      heightClassName='h-48 min-h-48 max-h-48'
                    />
                    {responseMappingError ? (
                      <p className='text-destructive text-sm'>
                        {responseMappingError}
                      </p>
                    ) : null}
                  </Field>
                </FieldGroup>
              </TabsContent>
            )
          })}
        </Tabs>
      </Dialog>
    </>
  )
}
