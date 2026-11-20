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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { clearDistillCache } from '@/features/distill-detect/api'
import type {
  DistillClearCacheRequest,
  DistillClearCacheResult,
} from '@/features/distill-detect/types'

/**
 * §20 一键清除蒸馏检测运行态：降级标记、确认计数、时间桶、去重窗口、告警节流。
 * 后端 Redis 与内存兜底两份都清；命中记录（distill_detect_hits）不删。
 */
export function DistillClearCachePanel() {
  const { t } = useTranslation()
  const [userId, setUserId] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [result, setResult] = useState<{
    request: DistillClearCacheRequest
    data: DistillClearCacheResult
  } | null>(null)

  const trimmed = userId.trim()
  const userIdValid = trimmed === '' || /^[1-9]\d*$/.test(trimmed)
  const request: DistillClearCacheRequest =
    trimmed === ''
      ? { scope: 'all' }
      : { scope: 'user', user_id: Number(trimmed) }

  const clearMutation = useMutation({
    mutationFn: clearDistillCache,
    onSuccess: (res, variables) => {
      setConfirmOpen(false)
      if (!res.success) return // 错误由 http-client 拦截器统一弹出
      setResult({ request: variables, data: res.data })
      toast.success(
        t('Cleared {{cleared}} keys, released {{users}} demoted users', {
          cleared: res.data.cleared,
          users: res.data.upgraded_users,
        })
      )
    },
  })

  const scopeLabel = (r: DistillClearCacheRequest) =>
    r.scope === 'all' ? t('all users') : t('user #{{id}}', { id: r.user_id })

  return (
    <div className='border-destructive/30 flex flex-col gap-3 rounded-lg border p-4'>
      <div className='flex flex-col gap-1'>
        <h4 className='text-sm font-medium'>{t('Clear detection state')}</h4>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Use this when users were misjudged because of a configuration problem. Clears demotion flags, confirmation counts, time buckets, dedup windows and alert throttling in both Redis and the in-memory fallback. Hit records are NOT deleted.'
          )}
        </p>
      </div>
      <div className='flex flex-wrap items-end gap-2'>
        <label className='flex flex-col gap-1 text-xs'>
          <span className='text-muted-foreground'>
            {t('User ID (leave empty for all users)')}
          </span>
          <Input
            className='w-56'
            inputMode='numeric'
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            placeholder={t('All users')}
          />
        </label>
        <Button
          type='button'
          size='sm'
          variant='destructive'
          disabled={!userIdValid || clearMutation.isPending}
          onClick={() => setConfirmOpen(true)}
        >
          {trimmed === ''
            ? t('Clear state for all users')
            : t('Clear state for this user')}
        </Button>
      </div>
      {userIdValid ? null : (
        <p className='text-destructive text-xs'>
          {t('User ID must be a number')}
        </p>
      )}
      {result ? (
        <p className='text-sm'>
          {t(
            'Last clear ({{scope}}): {{cleared}} keys removed, {{users}} demoted users released, backend {{backend}}.',
            {
              scope: scopeLabel(result.request),
              cleared: result.data.cleared,
              users: result.data.upgraded_users,
              backend: result.data.backend,
            }
          )}
        </p>
      ) : null}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        destructive
        isLoading={clearMutation.isPending}
        title={t('Clear distillation detection state?')}
        desc={t(
          'This clears the demotion, confirmation-count, dedup and alert-throttle state for {{scope}}. Demoted users go back to their normal channels immediately. Hit records are not deleted.',
          { scope: scopeLabel(request) }
        )}
        confirmText={t('Clear')}
        handleConfirm={() => clearMutation.mutate(request)}
      />
    </div>
  )
}
