/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
/*
  错误日志告警设置 —— 外层对话框：规则列表 + 新增/编辑/删除入口。
  单条规则的编辑对话框在同目录的 error-log-alert-rule-dialog.tsx。
*/
import { useCallback, useEffect, useState } from 'react'
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
import { api } from '@/lib/api'

import {
  ErrorLogAlertRuleDialog,
  type LogAlertRule,
} from './error-log-alert-rule-dialog'

export function ErrorLogAlertDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const { t } = useTranslation()
  const [rules, setRules] = useState<LogAlertRule[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<LogAlertRule | null>(null)
  const [ruleDialogOpen, setRuleDialogOpen] = useState(false)

  const loadRules = useCallback(() => {
    setLoading(true)
    api
      .get('/api/error-log-alerts/rules')
      .then((res) => setRules(res.data?.data ?? []))
      .catch(() => toast.error(t('Failed to load alert rules')))
      .finally(() => setLoading(false))
  }, [t])

  useEffect(() => {
    if (open) loadRules()
  }, [open, loadRules])

  const handleDelete = (id: number) => {
    api
      .delete(`/api/error-log-alerts/rules/${id}`)
      .then(() => {
        toast.success(t('Deleted'))
        loadRules()
      })
      .catch(() => toast.error(t('Delete failed')))
  }

  const handleTest = (id: number) => {
    api
      .post(`/api/error-log-alerts/rules/${id}/test`)
      .then((res) => {
        if (res.data?.data?.failed_platforms?.length > 0) {
          toast.error(t('Test send failed'))
          return
        }
        toast.success(t('Test message sent'))
      })
      .catch((err) =>
        toast.error(err?.response?.data?.message ?? t('Test send failed'))
      )
  }

  // 启用/禁用只走 toggle 接口；后端启用时会把 last_scan_at 拉回 interval 分钟前，
  // 避免累积的历史错误一次性全推
  const handleToggle = (r: LogAlertRule) => {
    const next = !r.enabled
    api
      .put(`/api/error-log-alerts/rules/${r.id}/toggle`, { enabled: next })
      .then(() => {
        toast.success(next ? t('Enabled') : t('Disabled'))
        loadRules()
      })
      .catch((err) =>
        toast.error(
          err?.response?.data?.message ??
            (next ? t('Enable failed') : t('Disable failed'))
        )
      )
  }

  const describeScope = (r: LogAlertRule) => {
    switch (r.scope_type) {
      case 'user':
        return t('By user')
      case 'token':
        return t('By token')
      case 'channel':
        return t('By channel')
      default:
        return t('All errors')
    }
  }

  const displayName = (r: LogAlertRule) =>
    (r.name && r.name.trim()) || `${t('Rule')} #${r.id}`

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className='flex max-h-[85vh] w-[95vw] max-w-6xl sm:max-w-6xl flex-col overflow-hidden'>
          <DialogHeader>
            <DialogTitle>{t('Error Log Alert Settings')}</DialogTitle>
          </DialogHeader>
          <div className='flex-1 space-y-3 overflow-y-auto overflow-x-hidden pe-1'>
            <div className='flex items-center justify-between'>
              <div className='text-sm text-muted-foreground'>
                {t(
                  'Scan error logs on schedule and push to WeCom group webhook when matched.'
                )}
              </div>
              <Button
                size='sm'
                onClick={() => {
                  setEditing(null)
                  setRuleDialogOpen(true)
                }}
              >
                +{t('New Alert Rule')}
              </Button>
            </div>

            {loading && (
              <div className='text-sm text-muted-foreground'>
                {t('Loading')}…
              </div>
            )}
            {!loading && rules.length === 0 && (
              <div className='rounded border border-dashed p-6 text-center text-sm text-muted-foreground'>
                {t('No alert rules yet. Click "New Alert Rule" to create one.')}
              </div>
            )}

            <div className='space-y-2'>
              {rules.map((r) => (
                <div
                  key={r.id}
                  className={`flex items-center justify-between gap-2 rounded border px-3 py-2 text-sm ${
                    r.enabled ? 'bg-muted/30' : 'bg-muted/10 opacity-60'
                  }`}
                >
                  <div className='min-w-0 flex-1'>
                    <div className='flex items-center gap-2'>
                      <span className='truncate font-medium'>
                        {displayName(r)}
                      </span>
                      <span
                        className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] leading-none ${
                          r.enabled
                            ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                            : 'bg-muted text-muted-foreground'
                        }`}
                      >
                        {r.enabled ? t('Enabled') : t('Disabled')}
                      </span>
                    </div>
                    <div className='mt-0.5 truncate text-xs text-muted-foreground'>
                      {describeScope(r)} · {t('Every')} {r.interval_minutes}{' '}
                      {t('minutes')}
                    </div>
                  </div>
                  <div className='ms-2 flex shrink-0 gap-2'>
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => handleToggle(r)}
                    >
                      {r.enabled ? t('Disable') : t('Enable')}
                    </Button>
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => handleTest(r.id)}
                    >
                      {t('Test')}
                    </Button>
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => {
                        setEditing(r)
                        setRuleDialogOpen(true)
                      }}
                    >
                      {t('Edit')}
                    </Button>
                    <Button
                      size='sm'
                      variant='ghost'
                      className='text-destructive hover:text-destructive'
                      onClick={() => handleDelete(r.id)}
                    >
                      {t('Delete')}
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => onOpenChange(false)}>
              {t('Close')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ErrorLogAlertRuleDialog
        rule={editing}
        open={ruleDialogOpen}
        onOpenChange={setRuleDialogOpen}
        onSaved={() => {
          setRuleDialogOpen(false)
          loadRules()
        }}
      />
    </>
  )
}
