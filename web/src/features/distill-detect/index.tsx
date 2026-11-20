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
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Clipboard,
  RefreshCw,
  ShieldAlert,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { getDistillSetting, listDistillHits } from './api'
import { resolveQuestionResults } from './lib'
import type { DistillHit, DistillHitsQuery, DistillQuestion } from './types'

const PAGE_SIZE = 20

type Filters = {
  userId: string
  requestId: string
  /** datetime-local 值（浏览器本地时区），提交时转 RFC3339。 */
  from: string
  to: string
}

const EMPTY_FILTERS: Filters = { userId: '', requestId: '', from: '', to: '' }

function localToRFC3339(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

function formatTime(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

export function DistillHits() {
  const { t } = useTranslation()
  // §17：同一页面按角色分流。非管理员只看自己（后端强制），这里只决定显示哪些列。
  const isAdmin = useAuthStore(
    (state) => (state.auth.user?.role ?? 0) >= ROLE.ADMIN
  )
  const [page, setPage] = useState(1)
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS)
  const [applied, setApplied] = useState<Filters>(EMPTY_FILTERS)

  const query: DistillHitsQuery = {
    p: page,
    page_size: PAGE_SIZE,
    user_id: isAdmin ? applied.userId.trim() || undefined : undefined,
    request_id: applied.requestId.trim() || undefined,
    from: localToRFC3339(applied.from),
    to: localToRFC3339(applied.to),
  }

  const hitsQuery = useQuery({
    queryKey: ['distill-detect', 'hits', query],
    queryFn: () => listDistillHits(query),
    placeholderData: keepPreviousData,
  })
  // 只用来把 categories 里的题目 id 换成题名；拿不到就直接显示 id。
  // 设置接口仍是 AdminAuth，普通用户不调，题目直接显示 id。
  const settingQuery = useQuery({
    queryKey: ['distill-detect', 'setting'],
    queryFn: getDistillSetting,
    staleTime: 60_000,
    enabled: isAdmin,
  })
  const questions = settingQuery.data?.success
    ? (settingQuery.data.data.questions ?? [])
    : []
  const pageData = hitsQuery.data?.success ? hitsQuery.data.data : undefined
  const items = pageData?.items ?? []
  const total = pageData?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const applyFilters = () => {
    if (isAdmin && draft.userId.trim() && !/^\d+$/.test(draft.userId.trim())) {
      toast.error(t('User ID must be a number'))
      return
    }
    setPage(1)
    setApplied(draft)
  }

  const resetFilters = () => {
    setDraft(EMPTY_FILTERS)
    setApplied(EMPTY_FILTERS)
    setPage(1)
  }

  const onEnter = (event: React.KeyboardEvent) => {
    if (event.key === 'Enter') applyFilters()
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Distillation hits')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='icon'
          onClick={() => hitsQuery.refetch()}
          title={t('Refresh')}
          aria-label={t('Refresh')}
        >
          <RefreshCw className={hitsQuery.isFetching ? 'animate-spin' : ''} />
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-3'>
          <div className='border-border/70 bg-muted/20 flex shrink-0 flex-wrap items-end gap-2 rounded-md border p-3'>
            {isAdmin ? (
              <label className='flex flex-col gap-1 text-xs'>
                <span className='text-muted-foreground'>{t('User ID')}</span>
                <Input
                  className='w-32'
                  inputMode='numeric'
                  value={draft.userId}
                  onChange={(e) =>
                    setDraft({ ...draft, userId: e.target.value })
                  }
                  onKeyDown={onEnter}
                />
              </label>
            ) : null}
            <label className='flex flex-col gap-1 text-xs'>
              <span className='text-muted-foreground'>{t('Request ID')}</span>
              <Input
                className='w-64'
                placeholder={t('Partial match')}
                value={draft.requestId}
                onChange={(e) =>
                  setDraft({ ...draft, requestId: e.target.value })
                }
                onKeyDown={onEnter}
              />
            </label>
            <label className='flex flex-col gap-1 text-xs'>
              <span className='text-muted-foreground'>{t('From')}</span>
              <Input
                type='datetime-local'
                className='w-52'
                value={draft.from}
                onChange={(e) => setDraft({ ...draft, from: e.target.value })}
              />
            </label>
            <label className='flex flex-col gap-1 text-xs'>
              <span className='text-muted-foreground'>{t('To')}</span>
              <Input
                type='datetime-local'
                className='w-52'
                value={draft.to}
                onChange={(e) => setDraft({ ...draft, to: e.target.value })}
              />
            </label>
            <Button size='sm' onClick={applyFilters}>
              {t('Search')}
            </Button>
            <Button size='sm' variant='outline' onClick={resetFilters}>
              {t('Reset')}
            </Button>
          </div>

          <div className='min-h-0 flex-1 overflow-auto rounded-md border'>
            {hitsQuery.isError ||
            (hitsQuery.data && !hitsQuery.data.success) ? (
              <div className='flex items-center gap-3 p-4 text-sm'>
                <span className='text-destructive'>
                  {t('Failed to load distillation hits')}
                </span>
                <Button
                  size='sm'
                  variant='outline'
                  onClick={() => hitsQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </div>
            ) : null}
            {!hitsQuery.isLoading && pageData && items.length === 0 ? (
              <EmptyState
                icon={ShieldAlert}
                title={t('No distillation hits')}
                description={
                  isAdmin
                    ? t(
                        'Hits appear here once a high-volume user scores at or above the threshold.'
                      )
                    : t(
                        'Requests from your account that were flagged by distillation detection appear here.'
                      )
                }
              />
            ) : null}
            {items.length > 0 ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-8' />
                    <TableHead>{t('Time')}</TableHead>
                    {isAdmin ? <TableHead>{t('User')}</TableHead> : null}
                    <TableHead>{t('Request ID')}</TableHead>
                    <TableHead>{t('Model')}</TableHead>
                    <TableHead className='text-right'>{t('Score')}</TableHead>
                    <TableHead className='min-w-[14rem]'>
                      {t('Hit questions')}
                    </TableHead>
                    {isAdmin ? <TableHead>{t('Rerouted')}</TableHead> : null}
                    <TableHead>{t('Demoted until')}</TableHead>
                    {isAdmin ? <TableHead>{t('Alert')}</TableHead> : null}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((hit) => (
                    <HitRow
                      key={hit.id}
                      hit={hit}
                      questions={questions}
                      isAdmin={isAdmin}
                    />
                  ))}
                </TableBody>
              </Table>
            ) : null}
          </div>

          <div className='flex shrink-0 items-center justify-between text-sm'>
            <span className='text-muted-foreground'>
              {t('{{count}} records', { count: total })}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                size='icon'
                variant='outline'
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
                aria-label={t('Previous page')}
              >
                <ChevronLeft />
              </Button>
              <span className='tabular-nums'>
                {page} / {totalPages}
              </span>
              <Button
                size='icon'
                variant='outline'
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
                aria-label={t('Next page')}
              >
                <ChevronRight />
              </Button>
            </div>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function HitRow(props: {
  hit: DistillHit
  questions: DistillQuestion[]
  isAdmin: boolean
}) {
  const { t } = useTranslation()
  const hit = props.hit
  const [open, setOpen] = useState(false)
  const results = resolveQuestionResults(hit, props.questions)
  const hitResults = results.filter((r) => r.hit)
  const columnCount = props.isAdmin ? 10 : 7

  const copyRequestId = async () => {
    if (await copyToClipboard(hit.request_id)) {
      toast.success(t('Copied to clipboard'))
    }
  }

  return (
    <>
      <TableRow>
        <TableCell className='w-8 px-1'>
          <Button
            size='icon'
            variant='ghost'
            className='size-7'
            onClick={() => setOpen(!open)}
            disabled={results.length === 0}
            aria-expanded={open}
            title={open ? t('Collapse') : t('Show all question scores')}
            aria-label={open ? t('Collapse') : t('Show all question scores')}
          >
            {open ? (
              <ChevronDown className='size-4' />
            ) : (
              <ChevronRight className='size-4' />
            )}
          </Button>
        </TableCell>
        <TableCell className='whitespace-nowrap'>
          {formatTime(hit.sampled_at)}
        </TableCell>
        {props.isAdmin ? (
          <TableCell className='whitespace-nowrap'>
            {hit.username || '-'}
            <span className='text-muted-foreground ml-1 text-xs'>
              #{hit.user_id}
            </span>
          </TableCell>
        ) : null}
        <TableCell>
          <div className='flex items-center gap-1'>
            <code
              className='max-w-[18rem] truncate font-mono text-xs'
              title={hit.request_id}
            >
              {hit.request_id}
            </code>
            <Button
              size='icon'
              variant='ghost'
              className='size-7 shrink-0'
              onClick={copyRequestId}
              title={t('Copy request ID')}
              aria-label={t('Copy request ID')}
            >
              <Clipboard className='size-3.5' />
            </Button>
          </div>
        </TableCell>
        <TableCell className='font-mono text-xs'>{hit.model || '-'}</TableCell>
        <TableCell className='text-right font-mono tabular-nums'>
          <div className='flex flex-col items-end gap-1'>
            {hit.score.toFixed(3)}
            {hit.dedup_reused ? (
              <Badge
                variant='outline'
                className='font-sans whitespace-nowrap'
                title={t(
                  'Same request text within the dedup window: the judge service was not called again; the previous score was reused and counted as a confirmation hit.'
                )}
              >
                {t('Reused previous judgment')}
              </Badge>
            ) : null}
          </div>
        </TableCell>
        {/* §18.2：证据列，折行不截断。 */}
        <TableCell className='text-sm break-words whitespace-normal'>
          {hitResults.length > 0
            ? hitResults
                .map((r) => `${r.name || r.id} ${r.score.toFixed(2)}`)
                .join('、')
            : '—'}
        </TableCell>
        {props.isAdmin ? (
          <TableCell>
            {hit.fallback_id > 0 ? (
              <Badge variant='warning'>#{hit.fallback_id}</Badge>
            ) : (
              <span className='text-muted-foreground'>{t('No')}</span>
            )}
          </TableCell>
        ) : null}
        <TableCell className='whitespace-nowrap'>
          {hit.demote_until ? formatTime(hit.demote_until) : '-'}
        </TableCell>
        {props.isAdmin ? (
          <TableCell>
            {hit.alert_sent ? (
              <Badge variant='secondary'>{t('Sent')}</Badge>
            ) : (
              <span className='text-muted-foreground'>{t('Not sent')}</span>
            )}
          </TableCell>
        ) : null}
      </TableRow>
      {open ? (
        <TableRow className='bg-muted/30 hover:bg-muted/30'>
          <TableCell />
          <TableCell colSpan={columnCount - 1}>
            {hit.dedup_reused ? (
              <p className='text-muted-foreground pt-1 text-xs'>
                {t(
                  'Same request text within the dedup window: the judge service was not called again; the previous score was reused and counted as a confirmation hit.'
                )}
              </p>
            ) : null}
            <ul className='grid gap-1 py-1 text-sm sm:grid-cols-2 lg:grid-cols-3'>
              {results.map((r) => (
                <li
                  key={r.id}
                  className={
                    r.hit
                      ? 'flex items-baseline gap-2'
                      : 'text-muted-foreground flex items-baseline gap-2 opacity-60'
                  }
                >
                  <span className='font-mono tabular-nums'>
                    {r.score.toFixed(3)}
                  </span>
                  <span className='break-words'>{r.name || r.id}</span>
                  {r.hit ? (
                    <Badge variant='destructive'>{t('Hit')}</Badge>
                  ) : null}
                  <code className='text-muted-foreground font-mono text-xs'>
                    {r.id}
                  </code>
                </li>
              ))}
            </ul>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  )
}
