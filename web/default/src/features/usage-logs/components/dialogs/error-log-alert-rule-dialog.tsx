/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
/*
  单条错误日志告警规则的编辑对话框。
  UI 结构对应原型：告警群设置 + 告警规则（频率 / 过滤内容 / 监控对象）
*/
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'

export interface LogAlertRule {
  id: number
  name: string
  enabled: boolean
  interval_minutes: number
  webhook_url: string
  filters: string
  scope_type: 'all' | 'user' | 'token' | 'channel'
  scope_values: string
  last_scan_at?: number
  created_at?: number
  updated_at?: number
}

interface FilterRow {
  code: string
  keywords: string // 原样字符串，逗号分隔
}

interface TokenGroupRow {
  user_id: number | null
  user_label: string // 展示用（回填时保存 username）
  token_ids: number[]
}

interface UserOption {
  id: number
  username: string
  display_name?: string
}

interface TokenOption {
  id: number
  name: string
}

const DEFAULT_FILTER_ROW: FilterRow = { code: '', keywords: '' }
const EMPTY_TOKEN_GROUP: TokenGroupRow = {
  user_id: null,
  user_label: '',
  token_ids: [],
}

// 推送方式 / 推送渠道 —— 保留下拉结构，方便后续扩展类型
const PUSH_METHOD_OPTIONS = [
  { value: 'webhook', labelKey: 'Webhook Push' },
]
const PUSH_CHANNEL_OPTIONS = [
  { value: 'wecom_group', labelKey: 'WeCom Group Bot' },
]

const SCOPE_OPTIONS: Array<{
  value: LogAlertRule['scope_type']
  labelKey: string
}> = [
  { value: 'channel', labelKey: 'By channel' },
  { value: 'user', labelKey: 'By user' },
  { value: 'token', labelKey: 'By token' },
  { value: 'all', labelKey: 'All errors' },
]

function parseFiltersJson(raw: string): FilterRow[] {
  if (!raw) return [{ ...DEFAULT_FILTER_ROW }]
  try {
    const parsed = JSON.parse(raw) as Array<{
      code?: string
      keywords?: string[]
    }>
    if (!Array.isArray(parsed) || parsed.length === 0) {
      return [{ ...DEFAULT_FILTER_ROW }]
    }
    return parsed.map((it) => ({
      code: it.code ?? '',
      keywords: (it.keywords ?? []).join(','),
    }))
  } catch {
    return [{ ...DEFAULT_FILTER_ROW }]
  }
}

function serializeFilters(rows: FilterRow[]): string {
  const cleaned = rows
    .map((r) => ({
      code: r.code.trim(),
      keywords: r.keywords
        .split(',')
        .map((k) => k.trim())
        .filter(Boolean),
    }))
    .filter((it) => it.code !== '' || it.keywords.length > 0)
  return JSON.stringify(cleaned)
}

/** 对 user / channel 场景：把后端 JSON 数组回填成 textarea 文本（一行一个）。 */
function scopeValuesToText(
  scope: LogAlertRule['scope_type'],
  raw: string
): string {
  if (!raw) return ''
  try {
    const arr = JSON.parse(raw)
    if (!Array.isArray(arr)) return ''
    if (scope === 'user') return (arr as string[]).join('\n')
    if (scope === 'channel') return (arr as number[]).join('\n')
    return ''
  } catch {
    return ''
  }
}

function scopeTextToValues(
  scope: LogAlertRule['scope_type'],
  text: string
): string {
  const lines = text
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
  if (scope === 'user') return JSON.stringify(lines)
  if (scope === 'channel') {
    return JSON.stringify(
      lines.map((s) => Number(s)).filter((n) => Number.isFinite(n) && n > 0)
    )
  }
  return ''
}

/** token 场景：ScopeValues JSON 结构 [{user_id, token_ids: []}] */
function parseTokenGroups(raw: string): TokenGroupRow[] {
  if (!raw) return [{ ...EMPTY_TOKEN_GROUP }]
  try {
    const arr = JSON.parse(raw)
    if (!Array.isArray(arr) || arr.length === 0)
      return [{ ...EMPTY_TOKEN_GROUP }]
    const out: TokenGroupRow[] = []
    for (const it of arr) {
      if (typeof it === 'number') {
        // 兼容旧数据：扁平 [1,2,3] → 归到一个未指定用户的组
        out.push({ user_id: null, user_label: '', token_ids: [it] })
      } else if (it && typeof it === 'object') {
        out.push({
          user_id: typeof it.user_id === 'number' ? it.user_id : null,
          user_label: typeof it.user_label === 'string' ? it.user_label : '',
          token_ids: Array.isArray(it.token_ids)
            ? it.token_ids.filter((n: unknown) => typeof n === 'number')
            : [],
        })
      }
    }
    return out.length > 0 ? out : [{ ...EMPTY_TOKEN_GROUP }]
  } catch {
    return [{ ...EMPTY_TOKEN_GROUP }]
  }
}

function serializeTokenGroups(rows: TokenGroupRow[]): string {
  const cleaned = rows
    .filter((r) => r.user_id != null && r.token_ids.length > 0)
    .map((r) => ({
      user_id: r.user_id,
      user_label: r.user_label,
      token_ids: r.token_ids,
    }))
  return JSON.stringify(cleaned)
}

export function ErrorLogAlertRuleDialog({
  rule,
  open,
  onOpenChange,
  onSaved,
}: {
  rule: LogAlertRule | null
  open: boolean
  onOpenChange: (o: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation()

  const [name, setName] = useState('')
  const [pushMethod, setPushMethod] = useState('webhook')
  const [pushChannel, setPushChannel] = useState('wecom_group')
  const [webhookUrl, setWebhookUrl] = useState('')
  const [intervalMinutes, setIntervalMinutes] = useState<number | ''>('')
  const [filters, setFilters] = useState<FilterRow[]>([{ ...DEFAULT_FILTER_ROW }])
  const [scopeType, setScopeType] =
    useState<LogAlertRule['scope_type']>('channel')
  const [scopeText, setScopeText] = useState('')
  const [tokenGroups, setTokenGroups] = useState<TokenGroupRow[]>([
    { ...EMPTY_TOKEN_GROUP },
  ])
  const [saving, setSaving] = useState(false)

  // 用户列表（token 场景使用）
  const [users, setUsers] = useState<UserOption[]>([])
  // 每个用户 id 对应的 token 列表缓存
  const [tokensByUser, setTokensByUser] = useState<Record<number, TokenOption[]>>({})

  const loadUsers = useCallback(() => {
    api
      .get('/api/error-log-alerts/lookup/users', { params: { limit: 100 } })
      .then((res) => setUsers(res.data?.data ?? []))
      .catch(() => {})
  }, [])

  const loadTokensFor = useCallback(
    (userId: number) => {
      if (tokensByUser[userId]) return
      api
        .get('/api/error-log-alerts/lookup/tokens', { params: { user_id: userId } })
        .then((res) =>
          setTokensByUser((prev) => ({ ...prev, [userId]: res.data?.data ?? [] }))
        )
        .catch(() => {})
    },
    [tokensByUser]
  )

  useEffect(() => {
    if (!open) return
    if (rule) {
      setName(rule.name ?? '')
      setWebhookUrl(rule.webhook_url ?? '')
      setIntervalMinutes(rule.interval_minutes || '')
      setFilters(parseFiltersJson(rule.filters))
      setScopeType(rule.scope_type ?? 'channel')
      if (rule.scope_type === 'token') {
        setTokenGroups(parseTokenGroups(rule.scope_values))
        setScopeText('')
      } else {
        setScopeText(scopeValuesToText(rule.scope_type, rule.scope_values))
        setTokenGroups([{ ...EMPTY_TOKEN_GROUP }])
      }
    } else {
      setName('')
      setWebhookUrl('')
      setIntervalMinutes('')
      setFilters([{ ...DEFAULT_FILTER_ROW }])
      setScopeType('channel')
      setScopeText('')
      setTokenGroups([{ ...EMPTY_TOKEN_GROUP }])
    }
    setPushMethod('webhook')
    setPushChannel('wecom_group')
  }, [rule, open])

  // 首次打开或切到 token 场景时加载用户 & 预取 tokens
  useEffect(() => {
    if (!open || scopeType !== 'token') return
    if (users.length === 0) loadUsers()
    for (const g of tokenGroups) {
      if (g.user_id != null) loadTokensFor(g.user_id)
    }
  }, [open, scopeType, tokenGroups, users.length, loadUsers, loadTokensFor])

  const scopePlaceholder = useMemo(() => {
    switch (scopeType) {
      case 'user':
        return t('Enter usernames, one per line')
      case 'channel':
        return t('Enter channel IDs, one per line')
      default:
        return ''
    }
  }, [scopeType, t])

  const save = () => {
    const url = (webhookUrl ?? '').trim()
    if (!url) {
      toast.error(t('Webhook URL is required'))
      return
    }
    const interval = Number(intervalMinutes)
    if (!Number.isFinite(interval) || interval <= 0) {
      toast.error(t('Alert frequency must be greater than 0'))
      return
    }
    let scopeValues = ''
    if (scopeType === 'token') {
      scopeValues = serializeTokenGroups(tokenGroups)
      if (scopeValues === '[]') {
        toast.error(t('Please fill monitored targets'))
        return
      }
    } else if (scopeType !== 'all') {
      if (scopeText.trim() === '') {
        toast.error(t('Please fill monitored targets'))
        return
      }
      scopeValues = scopeTextToValues(scopeType, scopeText)
    }

    setSaving(true)
    const payload = {
      name: name.trim(),
      interval_minutes: interval,
      webhook_url: url,
      filters: serializeFilters(filters),
      scope_type: scopeType,
      scope_values: scopeValues,
    }
    const req = rule
      ? api.put(`/api/error-log-alerts/rules/${rule.id}`, payload)
      : api.post('/api/error-log-alerts/rules', payload)
    req
      .then(() => {
        toast.success(t('Saved successfully'))
        onSaved()
      })
      .catch((err) =>
        toast.error(err?.response?.data?.message ?? t('Save failed'))
      )
      .finally(() => setSaving(false))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='flex max-h-[90vh] w-[95vw] max-w-6xl sm:max-w-6xl flex-col overflow-hidden'>
        <DialogHeader>
          <DialogTitle>
            {rule ? t('Edit Alert Rule') : t('New Alert Rule')}
          </DialogTitle>
        </DialogHeader>

        <div className='flex-1 space-y-5 overflow-y-auto overflow-x-hidden pe-1'>
          {/* Rule name (optional) */}
          <div>
            <Label className='mb-1 block text-sm font-medium'>
              {t('Rule Name')}{' '}
              <span className='text-xs text-muted-foreground'>
                ({t('optional')})
              </span>
            </Label>
            <Input
              placeholder={t('Leave empty to show as Rule #ID')}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          {/* 告警群设置 */}
          <div className='space-y-3'>
            <div className='text-sm font-semibold'>
              | {t('Alert Group Settings')}
            </div>
            <div className='grid grid-cols-2 gap-3'>
              <div>
                <Label className='mb-1 block text-sm'>{t('Push Method')}</Label>
                <select
                  className='h-9 w-full rounded border bg-background px-2 text-sm'
                  value={pushMethod}
                  onChange={(e) => setPushMethod(e.target.value)}
                >
                  {PUSH_METHOD_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {t(o.labelKey)}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <Label className='mb-1 block text-sm'>{t('Push Channel')}</Label>
                <select
                  className='h-9 w-full rounded border bg-background px-2 text-sm'
                  value={pushChannel}
                  onChange={(e) => setPushChannel(e.target.value)}
                >
                  {PUSH_CHANNEL_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {t(o.labelKey)}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            <div>
              <Label className='mb-1 block text-sm'>
                <span className='text-destructive'>*</span> {t('Webhook URL')}
              </Label>
              <textarea
                className='w-full rounded border bg-background p-2 text-sm'
                rows={2}
                placeholder='https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx'
                value={webhookUrl}
                onChange={(e) => setWebhookUrl(e.target.value)}
              />
            </div>
          </div>

          {/* 告警规则 */}
          <div className='space-y-3'>
            <div className='text-sm font-semibold'>| {t('Alert Rule')}</div>

            <div>
              <Label className='mb-1 block text-sm'>
                <span className='text-destructive'>*</span>{' '}
                {t('Alert Frequency (minutes)')}
              </Label>
              <Input
                type='number'
                min={1}
                placeholder={t('Please enter')}
                value={intervalMinutes}
                onChange={(e) =>
                  setIntervalMinutes(
                    e.target.value === '' ? '' : Number(e.target.value)
                  )
                }
              />
            </div>

            <div>
              <Label className='mb-1 block text-sm'>
                {t('Filter Alert Content')}
              </Label>
              <div className='mb-1 text-xs text-muted-foreground'>
                {t(
                  'Multiple rows OR; within a row error code + keywords AND; keywords are OR (comma separated); empty means no filter.'
                )}
              </div>
              <div className='space-y-2'>
                {filters.map((f, idx) => (
                  <div key={idx} className='flex items-center gap-2'>
                    <Input
                      className='w-40'
                      placeholder={t('Error code, e.g. 502')}
                      value={f.code}
                      onChange={(e) => {
                        const next = [...filters]
                        next[idx] = { ...next[idx], code: e.target.value }
                        setFilters(next)
                      }}
                    />
                    <Input
                      className='flex-1'
                      placeholder={t(
                        'Keywords, comma separated (e.g. overloaded,timeout)'
                      )}
                      value={f.keywords}
                      onChange={(e) => {
                        const next = [...filters]
                        next[idx] = { ...next[idx], keywords: e.target.value }
                        setFilters(next)
                      }}
                    />
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() =>
                        setFilters([...filters, { ...DEFAULT_FILTER_ROW }])
                      }
                    >
                      +
                    </Button>
                    <Button
                      size='sm'
                      variant='outline'
                      className='text-destructive'
                      disabled={filters.length <= 1}
                      onClick={() => {
                        const next = filters.filter((_, i) => i !== idx)
                        setFilters(
                          next.length > 0 ? next : [{ ...DEFAULT_FILTER_ROW }]
                        )
                      }}
                    >
                      -
                    </Button>
                  </div>
                ))}
              </div>
            </div>

            <div>
              <Label className='mb-1 block text-sm'>
                {t('Monitor Target Type')}
              </Label>
              <select
                className='h-9 w-full rounded border bg-background px-2 text-sm'
                value={scopeType}
                onChange={(e) => {
                  const next = e.target.value as LogAlertRule['scope_type']
                  setScopeType(next)
                  setScopeText('')
                  setTokenGroups([{ ...EMPTY_TOKEN_GROUP }])
                }}
              >
                {SCOPE_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {t(o.labelKey)}
                  </option>
                ))}
              </select>
            </div>

            {/* user / channel: 换行输入 */}
            {(scopeType === 'user' || scopeType === 'channel') && (
              <div>
                <Label className='mb-1 block text-sm'>
                  {t('Monitor Targets')}
                </Label>
                <textarea
                  className='w-full rounded border bg-background p-2 text-sm'
                  rows={4}
                  placeholder={scopePlaceholder}
                  value={scopeText}
                  onChange={(e) => setScopeText(e.target.value)}
                />
              </div>
            )}

            {/* token: 联动用户 + 密钥多选 */}
            {scopeType === 'token' && (
              <div>
                <Label className='mb-1 block text-sm'>
                  {t('Monitor Targets')}
                </Label>
                <div className='space-y-2'>
                  {tokenGroups.map((g, idx) => (
                    <div key={idx} className='flex items-start gap-2'>
                      <select
                        className='h-9 w-48 rounded border bg-background px-2 text-sm'
                        value={g.user_id ?? ''}
                        onChange={(e) => {
                          const v = e.target.value
                          const uid = v === '' ? null : Number(v)
                          const label = users.find((u) => u.id === uid)?.username ?? ''
                          const next = [...tokenGroups]
                          next[idx] = {
                            user_id: uid,
                            user_label: label,
                            token_ids: [], // 切换用户清空已选密钥
                          }
                          setTokenGroups(next)
                          if (uid != null) loadTokensFor(uid)
                        }}
                      >
                        <option value=''>{t('Please select user')}</option>
                        {users.map((u) => (
                          <option key={u.id} value={u.id}>
                            {u.username}
                            {u.display_name ? ` (${u.display_name})` : ''}
                          </option>
                        ))}
                      </select>
                      <select
                        multiple
                        disabled={g.user_id == null}
                        className='min-h-[80px] flex-1 rounded border bg-background p-2 text-sm disabled:opacity-50'
                        value={g.token_ids.map(String)}
                        onChange={(e) => {
                          const selected = Array.from(
                            e.target.selectedOptions,
                            (o) => Number(o.value)
                          )
                          const next = [...tokenGroups]
                          next[idx] = { ...next[idx], token_ids: selected }
                          setTokenGroups(next)
                        }}
                      >
                        {(g.user_id != null ? tokensByUser[g.user_id] ?? [] : []).map(
                          (tk) => (
                            <option key={tk.id} value={tk.id}>
                              {tk.name} (#{tk.id})
                            </option>
                          )
                        )}
                      </select>
                      <div className='flex flex-col gap-1'>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            setTokenGroups([
                              ...tokenGroups,
                              { ...EMPTY_TOKEN_GROUP },
                            ])
                          }
                        >
                          +
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          className='text-destructive'
                          disabled={tokenGroups.length <= 1}
                          onClick={() => {
                            const next = tokenGroups.filter((_, i) => i !== idx)
                            setTokenGroups(
                              next.length > 0 ? next : [{ ...EMPTY_TOKEN_GROUP }]
                            )
                          }}
                        >
                          -
                        </Button>
                      </div>
                    </div>
                  ))}
                </div>
                <div className='mt-1 text-xs text-muted-foreground'>
                  {t(
                    'Ctrl / Cmd + click to select multiple tokens.'
                  )}
                </div>
              </div>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={save} disabled={saving}>
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
