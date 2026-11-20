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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { useForm, type Control, type FieldPath } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { getChannels } from '@/features/channels/api'
import {
  getDistillSetting,
  testDistill,
  testDistillAlert,
  updateDistillSetting,
} from '@/features/distill-detect/api'
import {
  describeAlertResult,
  parseCategories,
  questionLabel,
  type AlertOutcome,
} from '@/features/distill-detect/lib'
import {
  questionsSchema,
  toQuestionFormValues,
  toQuestionPayload,
} from '@/features/distill-detect/questions'
import {
  DISTILL_CLEAR_KEY,
  type DistillAlertTestResult,
  type DistillQuestion,
  type DistillSetting,
  type DistillSettingUpdate,
  type DistillTestResult,
} from '@/features/distill-detect/types'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { DistillClearCachePanel } from './distill-clear-cache-panel'
import { DistillQuestionsEditor } from './distill-questions-editor'

// 校验范围与 controller/distill_detect.go 的 UpdateDistillDetectSetting 一致：
// 后端越界直接拒绝（不钳制），前端提前拦住，避免运营以为改了实际没生效。
const distillSchema = z
  .object({
    enabled: z.boolean(),
    observe_only: z.boolean(),
    activate_rpm: z.number().int().min(0).max(100000),
    judge_interval_seconds: z.number().int().min(1).max(3600),
    confirm_hits: z.number().int().min(1).max(100),
    confirm_window_seconds: z.number().int().min(30).max(86400),
    threshold: z.number().gt(0).max(1),
    demote_seconds: z.number().int().min(60).max(2592000),
    jev_base_url: z
      .string()
      .trim()
      .refine((v) => v === '' || /^https?:\/\//.test(v), {
        message: 'Must start with http:// or https://',
      }),
    jev_api_key: z.string(),
    clear_jev_api_key: z.boolean(),
    jev_timeout_ms: z.number().int().min(200).max(60000),
    max_state_chars: z.number().int().min(500).max(200000),
    head_sample_chars: z.number().int().min(0).max(200000),
    dedup_seconds: z.number().int().min(0).max(86400),
    questions: questionsSchema,
    alert_telegram: z.boolean(),
    alert_wecom: z.boolean(),
    alert_throttle_seconds: z.number().int().min(0).max(86400),
    tg_bot_token: z.string(),
    clear_tg_bot_token: z.boolean(),
    tg_chat_id: z.string().trim(),
    wecom_webhook_url: z
      .string()
      .trim()
      .refine((v) => v === '' || v.startsWith('https://qyapi.weixin.qq.com/'), {
        message: 'Must be a qyapi.weixin.qq.com address',
      }),
    fallback_channel_id: z.number().int().min(0),
    detect_channel_ids: z.array(z.number().int().positive()),
  })
  // §16.2：兜底渠道不能同时是检测渠道，后端保存时也会拒绝。
  .superRefine((v, ctx) => {
    if (
      v.fallback_channel_id > 0 &&
      v.detect_channel_ids.includes(v.fallback_channel_id)
    ) {
      ctx.addIssue({
        code: 'custom',
        path: ['fallback_channel_id'],
        message:
          'This channel is a detection channel; it cannot be the fallback',
      })
    }
  })

type DistillFormValues = z.infer<typeof distillSchema>

// 与密钥无关、可直接比较的字段（PUT 只提交有变化的）。
const PLAIN_KEYS = [
  'enabled',
  'observe_only',
  'activate_rpm',
  'judge_interval_seconds',
  'confirm_hits',
  'confirm_window_seconds',
  'threshold',
  'demote_seconds',
  'jev_base_url',
  'jev_timeout_ms',
  'max_state_chars',
  'head_sample_chars',
  'dedup_seconds',
  'alert_telegram',
  'alert_wecom',
  'alert_throttle_seconds',
  'tg_chat_id',
  'wecom_webhook_url',
  'fallback_channel_id',
] as const satisfies ReadonlyArray<keyof DistillSetting>

function toFormValues(s: DistillSetting): DistillFormValues {
  return {
    enabled: s.enabled,
    observe_only: s.observe_only,
    activate_rpm: s.activate_rpm,
    judge_interval_seconds: s.judge_interval_seconds,
    confirm_hits: s.confirm_hits,
    confirm_window_seconds: s.confirm_window_seconds,
    threshold: s.threshold,
    demote_seconds: s.demote_seconds,
    jev_base_url: s.jev_base_url,
    jev_api_key: '',
    clear_jev_api_key: false,
    jev_timeout_ms: s.jev_timeout_ms,
    max_state_chars: s.max_state_chars,
    head_sample_chars: s.head_sample_chars,
    dedup_seconds: s.dedup_seconds,
    questions: toQuestionFormValues(s.questions),
    alert_telegram: s.alert_telegram,
    alert_wecom: s.alert_wecom,
    // 旧后端没有该字段时按 0（不节流）展示，提交时只有改动才发送。
    alert_throttle_seconds: s.alert_throttle_seconds ?? 0,
    tg_bot_token: '',
    clear_tg_bot_token: false,
    tg_chat_id: s.tg_chat_id ?? '',
    wecom_webhook_url: s.wecom_webhook_url,
    fallback_channel_id: s.fallback_channel_id,
    detect_channel_ids: s.detect_channel_ids ?? [],
  }
}

function buildUpdate(
  values: DistillFormValues,
  saved: DistillSetting
): DistillSettingUpdate {
  const update: Record<string, unknown> = {}
  for (const key of PLAIN_KEYS) {
    const next =
      typeof values[key] === 'string'
        ? (values[key] as string).trim()
        : values[key]
    if (next !== (saved[key] ?? '')) update[key] = next
  }
  // 检测渠道整组提交；比较时忽略顺序。
  const sortIds = (ids: number[]) => [...ids].sort((a, b) => a - b).join(',')
  if (
    sortIds(values.detect_channel_ids) !==
    sortIds(saved.detect_channel_ids ?? [])
  ) {
    update.detect_channel_ids = values.detect_channel_ids
  }
  // 题库全量替换：内容（含顺序）有变化才提交整组。
  const questions = toQuestionPayload(values.questions)
  if (JSON.stringify(questions) !== JSON.stringify(saved.questions ?? [])) {
    update.questions = questions
  }
  // 密钥：勾选清空优先；否则只有填了新值才提交，空串不提交（= 不修改）。
  if (values.clear_jev_api_key) {
    update.jev_api_key = DISTILL_CLEAR_KEY
  } else if (values.jev_api_key.trim() !== '') {
    update.jev_api_key = values.jev_api_key.trim()
  }
  if (values.clear_tg_bot_token) {
    update.tg_bot_token = DISTILL_CLEAR_KEY
  } else if (values.tg_bot_token.trim() !== '') {
    update.tg_bot_token = values.tg_bot_token.trim()
  }
  return update as DistillSettingUpdate
}

const DISTILL_SETTING_QUERY_KEY = ['distill-detect', 'setting'] as const

export function DistillDetectSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const settingQuery = useQuery({
    queryKey: DISTILL_SETTING_QUERY_KEY,
    queryFn: getDistillSetting,
  })
  const saved = settingQuery.data?.success ? settingQuery.data.data : undefined

  const channelsQuery = useQuery({
    queryKey: ['channels', 'for-fallback'],
    queryFn: () => getChannels({ p: 1, page_size: 1000, id_sort: true }),
    staleTime: 60_000,
  })
  const channelOptions = useMemo(() => {
    const items = channelsQuery.data?.data?.items ?? []
    return items.map((channel) => ({
      value: String(channel.id),
      label: `#${channel.id} ${channel.name || ''}`.trim(),
    }))
  }, [channelsQuery.data])

  const form = useForm<DistillFormValues>({
    resolver: zodResolver(distillSchema),
    mode: 'onChange',
  })

  useEffect(() => {
    if (saved) form.reset(toFormValues(saved))
  }, [saved, form])

  const saveMutation = useMutation({
    mutationFn: updateDistillSetting,
    onSuccess: (res) => {
      if (!res.success) return // 错误提示由 http-client 拦截器统一弹出
      queryClient.setQueryData(DISTILL_SETTING_QUERY_KEY, res)
      toast.success(t('Distillation detection saved'))
    },
  })

  const onSubmit = (values: DistillFormValues) => {
    if (!saved) return
    const update = buildUpdate(values, saved)
    if (Object.keys(update).length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    saveMutation.mutate(update)
  }

  if (settingQuery.isLoading) {
    return (
      <SettingsSection title={t('Distillation detection')}>
        <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
      </SettingsSection>
    )
  }

  if (!saved) {
    return (
      <SettingsSection title={t('Distillation detection')}>
        <div className='flex items-center gap-3'>
          <p className='text-destructive text-sm'>
            {t('Failed to load distillation detection settings')}
          </p>
          <Button
            size='sm'
            variant='outline'
            onClick={() => settingQuery.refetch()}
          >
            {t('Retry')}
          </Button>
        </div>
      </SettingsSection>
    )
  }

  const control = form.control
  const observeOnly = form.watch('observe_only')
  const clearKey = form.watch('clear_jev_api_key')
  const clearTgToken = form.watch('clear_tg_bot_token')
  const detectIds = form.watch('detect_channel_ids') ?? []
  const fallbackId = form.watch('fallback_channel_id') ?? 0
  const detectIdSet = new Set(detectIds)
  // 检测渠道：已删除的渠道仍保留在列表里，标注「已失效」，可手动移除。
  const knownIds = new Set(channelOptions.map((o) => Number(o.value)))
  const detectOptions = [
    ...channelOptions.map((o) => ({
      ...o,
      label:
        Number(o.value) === fallbackId
          ? `${o.label} (${t('fallback channel')})`
          : o.label,
      disabled: Number(o.value) === fallbackId,
    })),
    ...detectIds
      .filter((id) => !knownIds.has(id) && channelsQuery.isSuccess)
      .map((id) => ({
        value: String(id),
        label: `#${id} (${t('invalid')})`,
      })),
  ]

  return (
    <SettingsSection title={t('Distillation detection')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={saveMutation.isPending}
            saveLabel='Save distillation detection'
          />

          <div className='flex flex-wrap items-center gap-2 text-sm'>
            {observeOnly ? (
              <Badge variant='secondary'>
                {t('Observe only: record and alert, no rerouting')}
              </Badge>
            ) : (
              <Badge variant='warning'>
                {t(
                  'Rerouting enabled: confirmed users go to the fallback channel'
                )}
              </Badge>
            )}
            <Link
              to='/distill-hits'
              className='text-primary text-sm underline-offset-4 hover:underline'
            >
              {t('View distillation hits')}
            </Link>
          </div>

          <SwitchField
            control={control}
            name='enabled'
            label={t('Enable distillation detection')}
            description={t(
              'Score requests from high-volume users against the training-data & evaluation question bank. Users can override this in user edit.'
            )}
          />
          <SwitchField
            control={control}
            name='observe_only'
            label={t('Observe only')}
            description={t(
              'Only record hits and send alerts; never reroute traffic. Keep this on during the 24–72 hour observation period.'
            )}
          />

          <GroupTitle>{t('Detection channels')}</GroupTitle>
          <FormField
            control={control}
            name='detect_channel_ids'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Channels to detect')}</FormLabel>
                <FormControl>
                  <MultiSelect
                    options={detectOptions}
                    selected={(field.value ?? []).map(String)}
                    onChange={(values) =>
                      field.onChange(values.map(Number).filter((n) => n > 0))
                    }
                    placeholder={t('Select channels...')}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Only requests dispatched to these channels are counted and judged. Other channels skip detection entirely. The fallback channel cannot be selected here.'
                  )}
                </FormDescription>
                {detectIds.length === 0 ? (
                  <p className='text-warning text-sm'>
                    {t(
                      'No detection channel selected: distillation detection is effectively off.'
                    )}
                  </p>
                ) : null}
                <FormMessage />
              </FormItem>
            )}
          />

          <GroupTitle>{t('Activation & judging cadence')}</GroupTitle>
          <p className='text-muted-foreground -mt-2 text-xs'>
            {t('Applies only to the detection channels selected above.')}
          </p>
          <div className='grid gap-4 md:grid-cols-2'>
            <NumberField
              control={control}
              name='activate_rpm'
              label={t('Activation threshold')}
              unit={t('requests / minute')}
              min={0}
              max={100000}
              description={t(
                'Users below this per-minute request count are never judged.'
              )}
            />
            <NumberField
              control={control}
              name='threshold'
              label={t('Score threshold')}
              min={0.01}
              max={1}
              step={0.01}
              float
              description={t(
                'A request hits when the highest question score reaches this value (0–1].'
              )}
            />
          </div>
          <div className='grid gap-4 md:grid-cols-3'>
            <NumberField
              control={control}
              name='judge_interval_seconds'
              label={t('Judge interval')}
              unit={t('seconds')}
              min={1}
              max={3600}
              description={t(
                'Judge the same user at most once per interval. 30 s = at most 2 per minute.'
              )}
            />
            <NumberField
              control={control}
              name='confirm_hits'
              label={t('Confirmation hits')}
              unit={t('times')}
              min={1}
              max={100}
              description={t(
                'Hits needed inside the window before the user is demoted. Fewer hits are only recorded.'
              )}
            />
            <NumberField
              control={control}
              name='confirm_window_seconds'
              label={t('Confirmation window')}
              unit={t('seconds')}
              min={30}
              max={86400}
              description={t(
                'Sliding window, never reset. Default 4 hits / 300 s confirms a real distiller in about 2–3 minutes.'
              )}
            />
          </div>
          <div className='grid gap-4 md:grid-cols-2'>
            <NumberField
              control={control}
              name='demote_seconds'
              label={t('Demotion duration')}
              unit={t('seconds')}
              min={60}
              max={2592000}
              step={60}
              description={t(
                'How long a confirmed user is routed to the distillation fallback channel.'
              )}
            />
            <NumberField
              control={control}
              name='dedup_seconds'
              label={t('Dedup window')}
              unit={t('seconds')}
              min={0}
              max={86400}
              description={t(
                'Identical request text from the same user is scored only once within this window. 0 disables.'
              )}
            />
          </div>

          <GroupTitle>{t('Fallback channel')}</GroupTitle>
          <FormField
            control={control}
            name='fallback_channel_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Distillation fallback channel')}</FormLabel>
                <FormControl>
                  <Select
                    items={[
                      { value: '0', label: t('None') },
                      ...channelOptions,
                    ]}
                    value={String(field.value ?? 0)}
                    onValueChange={(value) =>
                      field.onChange(Number(value) || 0)
                    }
                  >
                    <SelectTrigger className='w-full md:w-96'>
                      <SelectValue placeholder={t('None')} />
                    </SelectTrigger>
                    <SelectContent
                      alignItemWithTrigger={false}
                      className='max-h-80'
                    >
                      <SelectGroup>
                        <SelectItem value='0'>{t('None')}</SelectItem>
                        {channelOptions.map((option) => {
                          const isDetect = detectIdSet.has(Number(option.value))
                          return (
                            <SelectItem
                              key={option.value}
                              value={option.value}
                              disabled={isDetect}
                            >
                              {isDetect
                                ? `${option.label} (${t('detection channel')})`
                                : option.label}
                            </SelectItem>
                          )
                        })}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </FormControl>
                <FormDescription>
                  {t(
                    'Global channel for confirmed distillation users, independent of the refusal-demotion fallback. When it is unset or unavailable, requests get 429 distill_fallback_unavailable.'
                  )}{' '}
                  {t('Detection channels cannot be selected as the fallback.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <GroupTitle>{t('Judge service')}</GroupTitle>
          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={control}
              name='jev_base_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Judge service URL')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder='https://'
                      autoComplete='off'
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={control}
              name='jev_api_key'
              render={({ field }) => (
                <FormItem>
                  <FormLabel className='flex items-center gap-2'>
                    {t('Judge service key')}
                    {saved.jev_api_key_set ? (
                      <Badge variant='secondary'>{t('Configured')}</Badge>
                    ) : (
                      <Badge variant='warning'>{t('Not configured')}</Badge>
                    )}
                  </FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type='password'
                      autoComplete='new-password'
                      disabled={clearKey}
                      placeholder={
                        saved.jev_api_key_set
                          ? t('Leave empty to keep the current key')
                          : t('Enter key')
                      }
                    />
                  </FormControl>
                  {saved.jev_api_key_set ? (
                    <FormField
                      control={control}
                      name='clear_jev_api_key'
                      render={({ field: clearField }) => (
                        <label className='text-muted-foreground flex items-center gap-2 text-xs'>
                          <Switch
                            checked={clearField.value}
                            onCheckedChange={clearField.onChange}
                          />
                          {t('Clear the saved key on save')}
                        </label>
                      )}
                    />
                  ) : null}
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <div className='grid gap-4 md:grid-cols-3'>
            <NumberField
              control={control}
              name='jev_timeout_ms'
              label={t('Judge timeout')}
              unit='ms'
              min={200}
              max={60000}
              step={100}
            />
            <NumberField
              control={control}
              name='max_state_chars'
              label={t('Max text sent')}
              unit={t('characters')}
              min={500}
              max={200000}
            />
            <NumberField
              control={control}
              name='head_sample_chars'
              label={t('Head sample')}
              unit={t('characters')}
              min={0}
              max={200000}
              description={t(
                'Keep this many characters from the start plus the tail; 0 keeps only the tail.'
              )}
            />
          </div>

          <GroupTitle>{t('Question bank')}</GroupTitle>
          <DistillQuestionsEditor
            control={
              control as unknown as Control<{
                questions: DistillFormValues['questions']
              }>
            }
          />

          <GroupTitle>{t('Alerts')}</GroupTitle>
          <div className='grid gap-3 md:grid-cols-2'>
            <SwitchField
              control={control}
              name='alert_telegram'
              label={t('Telegram alert')}
              description={t(
                'Uses the dedicated bot and chat below, not the site notification channel.'
              )}
            />
            <SwitchField
              control={control}
              name='alert_wecom'
              label={t('WeCom alert')}
            />
          </div>
          <NumberField
            control={control}
            name='alert_throttle_seconds'
            label={t('Alert throttle')}
            unit={t('seconds')}
            min={0}
            max={86400}
            description={t(
              'Send at most one alert per user within this window. 0 disables throttling.'
            )}
          />
          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={control}
              name='tg_bot_token'
              render={({ field }) => (
                <FormItem>
                  <FormLabel className='flex items-center gap-2'>
                    {t('Telegram bot token')}
                    {saved.tg_bot_token_set ? (
                      <Badge variant='secondary'>{t('Configured')}</Badge>
                    ) : (
                      <Badge variant='warning'>{t('Not configured')}</Badge>
                    )}
                  </FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type='password'
                      autoComplete='new-password'
                      disabled={clearTgToken}
                      placeholder={
                        saved.tg_bot_token_set
                          ? t('Leave empty to keep the current token')
                          : '123456:ABC-DEF...'
                      }
                    />
                  </FormControl>
                  {saved.tg_bot_token_set ? (
                    <FormField
                      control={control}
                      name='clear_tg_bot_token'
                      render={({ field: clearField }) => (
                        <label className='text-muted-foreground flex items-center gap-2 text-xs'>
                          <Switch
                            checked={clearField.value}
                            onCheckedChange={clearField.onChange}
                          />
                          {t('Clear the saved token on save')}
                        </label>
                      )}
                    />
                  ) : null}
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={control}
              name='tg_chat_id'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Telegram chat ID')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      autoComplete='off'
                      placeholder='-1001234567890'
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Group or channel that receives distillation alerts; the bot must be a member.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <FormField
            control={control}
            name='wecom_webhook_url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('WeCom webhook URL')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder='https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key='
                    autoComplete='off'
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <DistillAlertTestPanel />
        </SettingsForm>
      </Form>

      <DistillTestPanel questions={saved.questions ?? []} />

      <DistillClearCachePanel />
    </SettingsSection>
  )
}

function GroupTitle(props: { children: React.ReactNode }) {
  return (
    <h4 className='text-muted-foreground border-t pt-4 text-sm font-medium'>
      {props.children}
    </h4>
  )
}

type BoolKey = {
  [K in keyof DistillFormValues]: DistillFormValues[K] extends boolean
    ? K
    : never
}[keyof DistillFormValues]

type NumberKey = {
  [K in keyof DistillFormValues]: DistillFormValues[K] extends number
    ? K
    : never
}[keyof DistillFormValues]

function SwitchField(props: {
  control: Control<DistillFormValues>
  name: BoolKey & FieldPath<DistillFormValues>
  label: string
  description?: string
}) {
  return (
    <FormField
      control={props.control}
      name={props.name}
      render={({ field }) => (
        <SettingsSwitchItem>
          <SettingsSwitchContent>
            <FormLabel>{props.label}</FormLabel>
            {props.description ? (
              <FormDescription>{props.description}</FormDescription>
            ) : null}
          </SettingsSwitchContent>
          <FormControl>
            <Switch
              checked={Boolean(field.value)}
              onCheckedChange={field.onChange}
            />
          </FormControl>
        </SettingsSwitchItem>
      )}
    />
  )
}

function NumberField(props: {
  control: Control<DistillFormValues>
  name: NumberKey & FieldPath<DistillFormValues>
  label: string
  unit?: string
  min: number
  max: number
  step?: number
  float?: boolean
  description?: string
}) {
  return (
    <FormField
      control={props.control}
      name={props.name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.label}</FormLabel>
          <FormControl>
            <div className='flex items-center gap-2'>
              <Input
                type='number'
                min={props.min}
                max={props.max}
                step={props.step ?? 1}
                name={field.name}
                ref={field.ref}
                onBlur={field.onBlur}
                value={Number.isFinite(field.value) ? String(field.value) : ''}
                onChange={(e) => {
                  const raw = e.target.value
                  const parsed = props.float
                    ? Number.parseFloat(raw)
                    : Number.parseInt(raw, 10)
                  // 空/非法值保留为 NaN，让 zod 报错，而不是悄悄改成某个默认数。
                  field.onChange(Number.isNaN(parsed) ? Number.NaN : parsed)
                }}
              />
              {props.unit ? (
                <span className='text-muted-foreground shrink-0 text-sm'>
                  {props.unit}
                </span>
              ) : null}
            </div>
          </FormControl>
          {props.description ? (
            <FormDescription>{props.description}</FormDescription>
          ) : null}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function DistillAlertTestPanel() {
  const { t } = useTranslation()
  const [result, setResult] = useState<DistillAlertTestResult | null>(null)
  const alertMutation = useMutation({
    mutationFn: testDistillAlert,
    onSuccess: (res) => {
      if (res.success) setResult(res.data)
    },
  })

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex flex-wrap items-center gap-3'>
        <Button
          type='button'
          size='sm'
          variant='outline'
          disabled={alertMutation.isPending}
          onClick={() => {
            setResult(null)
            alertMutation.mutate()
          }}
        >
          {alertMutation.isPending ? t('Sending...') : t('Send test alert')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {t('Uses the saved settings; save your changes first.')}
        </span>
      </div>
      {result ? (
        <ul className='flex flex-col gap-1 text-sm'>
          <AlertResultLine
            label='Telegram'
            outcome={describeAlertResult(result.telegram)}
          />
          <AlertResultLine
            label={t('WeCom')}
            outcome={describeAlertResult(result.wecom)}
          />
        </ul>
      ) : null}
    </div>
  )
}

function AlertResultLine(props: { label: string; outcome: AlertOutcome }) {
  const { kind, detail } = props.outcome
  return (
    <li className='flex flex-wrap items-center gap-2'>
      <span className='w-20 shrink-0'>{props.label}</span>
      <AlertBadge kind={kind} />
      {detail ? (
        <span className='text-muted-foreground text-xs break-all'>
          {detail}
        </span>
      ) : null}
    </li>
  )
}

function AlertBadge(props: { kind: AlertOutcome['kind'] }) {
  const { t } = useTranslation()
  if (props.kind === 'sent') {
    return <Badge variant='secondary'>{t('Sent')}</Badge>
  }
  if (props.kind === 'disabled') {
    return <Badge variant='outline'>{t('Alert switch off')}</Badge>
  }
  if (props.kind === 'not_configured') {
    return <Badge variant='outline'>{t('Not configured')}</Badge>
  }
  return <Badge variant='destructive'>{t('Failed')}</Badge>
}

function DistillTestPanel(props: { questions: DistillQuestion[] }) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<'body' | 'text'>('body')
  const [input, setInput] = useState('')
  const [result, setResult] = useState<DistillTestResult | null>(null)

  const testMutation = useMutation({
    mutationFn: testDistill,
    onSuccess: (res) => {
      if (res.success) setResult(res.data)
    },
  })

  const run = () => {
    const value = input.trim()
    if (!value) {
      toast.error(t('Paste a request body or some text first'))
      return
    }
    setResult(null)
    testMutation.mutate(mode === 'body' ? { body: value } : { text: value })
  }

  return (
    <div className='flex flex-col gap-3 rounded-lg border p-4'>
      <div className='flex flex-col gap-1'>
        <h4 className='text-sm font-medium'>{t('Test judge')}</h4>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Uses the saved settings. Nothing is recorded, no alert is sent and no user state changes.'
          )}
        </p>
      </div>
      <div className='flex gap-2'>
        <Button
          type='button'
          size='sm'
          variant={mode === 'body' ? 'default' : 'outline'}
          onClick={() => setMode('body')}
        >
          {t('Request JSON')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant={mode === 'text' ? 'default' : 'outline'}
          onClick={() => setMode('text')}
        >
          {t('Plain text')}
        </Button>
      </div>
      <Textarea
        rows={6}
        value={input}
        onChange={(e) => setInput(e.target.value)}
        placeholder={
          mode === 'body'
            ? t(
                'Paste a full request JSON; it is sampled exactly like live traffic'
              )
            : t('Paste the text to score')
        }
        className='font-mono text-xs'
      />
      <div>
        <Button
          type='button'
          size='sm'
          onClick={run}
          disabled={testMutation.isPending}
        >
          {testMutation.isPending ? t('Judging...') : t('Run test')}
        </Button>
      </div>
      {result ? (
        <DistillTestResultView result={result} questions={props.questions} />
      ) : null}
    </div>
  )
}

function DistillTestResultView(props: {
  result: DistillTestResult
  questions: DistillQuestion[]
}) {
  const { t } = useTranslation()
  const result = props.result
  if (result.skipped) {
    return (
      <p className='text-warning text-sm'>
        {t('Not judged: {{reason}}', { reason: result.skipped })}
      </p>
    )
  }
  const scores = parseCategories(result.categories)
  return (
    <div className='flex flex-col gap-2 text-sm'>
      <div className='flex flex-wrap items-center gap-2'>
        {result.hit ? (
          <Badge variant='destructive'>{t('Hit')}</Badge>
        ) : (
          <Badge variant='secondary'>{t('No hit')}</Badge>
        )}
        <span>
          {t('Highest score')}:{' '}
          <span className='font-mono'>{result.score.toFixed(3)}</span>
        </span>
        <span className='text-muted-foreground'>
          {t('{{count}} characters judged', { count: result.doc_chars })}
        </span>
      </div>
      {scores.length > 0 ? (
        <ul className='text-muted-foreground grid gap-1 font-mono text-xs md:grid-cols-2'>
          {scores.map(([key, value]) => (
            <li key={key} title={key}>
              {questionLabel(key, props.questions)}: {value.toFixed(3)}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
