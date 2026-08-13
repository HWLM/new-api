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
import { getRouteApi } from '@tanstack/react-router'
import { Download, Loader2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'
import { getDefaultTimeRange } from '../lib/utils'
import {
  getCurrentUsageLogExportTask,
  getUsageLogExportTask,
  startUsageLogExportTask,
} from '../api'
import type {
  UsageLogExportTask,
  UsageLogExportTaskPayload,
} from '../types'
import { useLogsViewScope } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

function isActiveUsageLogExportTask(task: UsageLogExportTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

function toExportTimestamp(value: unknown, fallback: number) {
  const timestamp = typeof value === 'number' && Number.isFinite(value) ? value : fallback
  return timestamp > 0 ? timestamp : fallback
}

function buildExportPayload(search: ReturnType<typeof route.useSearch>): UsageLogExportTaskPayload {
  const { start, end } = getDefaultTimeRange()
  const startTimestamp = toExportTimestamp(search.startTime, start.getTime())
  const endTimestamp = toExportTimestamp(search.endTime, end.getTime())

  return {
    start_timestamp: Math.floor(startTimestamp / 1000),
    end_timestamp: Math.ceil(endTimestamp / 1000),
    ...(search.channel ? { channel: Number(search.channel) || 0 } : {}),
    ...(search.model ? { model_name: search.model } : {}),
    ...(search.token ? { token_name: search.token } : {}),
    ...(search.group ? { group: search.group } : {}),
    ...(search.username ? { username: search.username } : {}),
    ...(search.requestId ? { request_id: search.requestId } : {}),
    ...(search.upstreamRequestId
      ? { upstream_request_id: search.upstreamRequestId }
      : {}),
  }
}

export function CommonLogsExportButton() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const { isAdminView } = useLogsViewScope()
  const isRoot = useAuthStore(
    (state) => (state.auth.user?.role ?? 0) >= ROLE.SUPER_ADMIN
  )
  const [task, setTask] = useState<UsageLogExportTask | null>(null)
  const [isStarting, setIsStarting] = useState(false)
  const downloadUrlRef = useRef<string | null>(null)

  useEffect(() => {
    if (!isRoot) {
      setTask(null)
      downloadUrlRef.current = null
      return
    }

    let cancelled = false
    async function loadCurrentTask() {
      try {
        const res = await getCurrentUsageLogExportTask()
        if (cancelled || !res.success || !res.data) return
        setTask(res.data)
      } catch {
        /* ignore */
      }
    }

    void loadCurrentTask()
    return () => {
      cancelled = true
    }
  }, [isRoot])

  useEffect(() => {
    if (!task || !isActiveUsageLogExportTask(task)) {
      return
    }

    let cancelled = false
    const taskId = task.task_id

    const poll = async () => {
      try {
        const res = await getUsageLogExportTask(taskId)
        if (cancelled || !res.success || !res.data) return
        setTask(res.data)
        if (!isActiveUsageLogExportTask(res.data)) {
          if (res.data.status === 'succeeded') {
            downloadUrlRef.current = res.data.result?.download_url ?? null
            toast.success(t('Usage log export completed.'))
          } else {
            toast.error(res.data.error || t('Export failed'))
          }
        }
      } catch {
        /* keep polling */
      }
    }

    void poll()
    const interval = window.setInterval(() => {
      void poll()
    }, 1500)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [t, task])

  const active = isActiveUsageLogExportTask(task)
  const exportButtonLabel = isStarting || active ? t('Exporting...') : t('Export')
  const downloadUrl = downloadUrlRef.current

  const handleStartExport = useCallback(async () => {
    if (!isRoot || isStarting || active) {
      return
    }

    setIsStarting(true)
    downloadUrlRef.current = null

    try {
      const res = await startUsageLogExportTask(buildExportPayload(search))
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Export failed'))
      }
      setTask(res.data)
      toast.success(t('Usage log export started.'))
    } catch (error) {
      const message = error instanceof Error ? error.message : t('Export failed')
      toast.error(message)
    } finally {
      setIsStarting(false)
    }
  }, [active, isRoot, isStarting, search, t])

  const downloadButton = useMemo(() => {
    if (!downloadUrl || active || isStarting) {
      return null
    }

    return (
      <Button
        variant='secondary'
        size='sm'
        render={<a href={downloadUrl} target='_blank' rel='noreferrer' />}
      >
        <Download className='h-4 w-4' />
        {t('Download')}
      </Button>
    )
  }, [active, downloadUrl, isStarting, t])

  if (!isAdminView || !isRoot) {
    return null
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Button
        variant='outline'
        size='sm'
        onClick={() => void handleStartExport()}
        disabled={isStarting || active}
      >
        {isStarting || active ? (
          <Loader2 className='h-4 w-4 animate-spin' />
        ) : (
          <Download className='h-4 w-4' />
        )}
        {exportButtonLabel}
      </Button>
      {downloadButton}
    </div>
  )
}
