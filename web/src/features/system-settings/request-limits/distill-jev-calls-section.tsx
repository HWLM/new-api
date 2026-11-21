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
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { listDistillJevCalls } from '@/features/distill-detect/api'
import {
  DISTILL_JEV_OUTCOMES,
  type DistillJevCall,
  type DistillJevCallsQuery,
  type DistillJevCallsSummary,
} from '@/features/distill-detect/types'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { SettingsSection } from '../components/settings-section'

const PAGE_SIZE = 20
const ALL = '__all__'
const COLUMN_COUNT = 9

type Filters = {
  userId: string
  outcome: string
  /** datetime-local（浏览器本地时区），提交时转 RFC3339 */
  from: string
  to: string
}

const EMPTY_FILTERS: Filters = { userId: '', outcome: '', from: '', to: '' }

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

/** outcome → 文案键；未知值原样显示。 */
const OUTCOME_LABEL: Record<string, string> = {
  success: 'Success',
  http_error: 'HTTP error',
  timeout: 'Timeout',
  network_error: 'Network error',
  parse_error: 'Parse error',
}

export function DistillJevCallsSection() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS)
  const [applied, setApplied] = useState<Filters>(EMPTY_FILTERS)

  const query: DistillJevCallsQuery = {
    p: page,
    page_size: PAGE_SIZE,
    user_id: applied.userId.trim() || undefined,
    outcome: applied.outcome || undefined,
    from: localToRFC3339(applied.from),
    to: localToRFC3339(applied.to),
  }

  const callsQuery = useQuery({
    queryKey: ['distill-detect', 'jev-calls', query],
    queryFn: () => listDistillJevCalls(query),
    placeholderData: keepPreviousData,
  })
  const pageData = callsQuery.data?.success ? callsQuery.data.data : undefined
  const items = pageData?.items ?? []
  const total = pageData?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const applyFilters = (next: Filters = draft) => {
    if (next.userId.trim() && !/^\d+$/.test(next.userId.trim())) {
      toast.error(t('User ID must be a number'))
      return
    }
    setPage(1)
    setApplied(next)
  }

  const resetFilters = () => {
    setDraft(EMPTY_FILTERS)
    setApplied(EMPTY_FILTERS)
    setPage(1)
  }

  return (
    <SettingsSection title={t('Jev call records')}>
      <div className='flex flex-col gap-4'>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Every real call to the judge service, including failures. Dedup-skipped requests are not called and not listed.'
          )}
        </p>

        <SummaryCards summary={pageData?.summary} />

        <div className='border-border/70 bg-muted/20 flex flex-wrap items-end gap-2 rounded-md border p-3'>
          <label className='flex flex-col gap-1 text-xs'>
            <span className='text-muted-foreground'>{t('User ID')}</span>
            <Input
              className='w-32'
              inputMode='numeric'
              value={draft.userId}
              onChange={(e) => setDraft({ ...draft, userId: e.target.value })}
              onKeyDown={(e) => {
                if (e.key === 'Enter') applyFilters()
              }}
            />
          </label>
          <label className='flex flex-col gap-1 text-xs'>
            <span className='text-muted-foreground'>{t('Result')}</span>
            <Select
              items={[
                { value: ALL, label: t('All') },
                ...DISTILL_JEV_OUTCOMES.map((o) => ({
                  value: o,
                  label: t(OUTCOME_LABEL[o] ?? o),
                })),
              ]}
              value={draft.outcome || ALL}
              onValueChange={(value) => {
                const next = {
                  ...draft,
                  outcome: !value || value === ALL ? '' : String(value),
                }
                setDraft(next)
                applyFilters(next)
              }}
            >
              <SelectTrigger className='w-40'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value={ALL}>{t('All')}</SelectItem>
                  {DISTILL_JEV_OUTCOMES.map((o) => (
                    <SelectItem key={o} value={o}>
                      {t(OUTCOME_LABEL[o] ?? o)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
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
          <Button size='sm' onClick={() => applyFilters()}>
            {t('Search')}
          </Button>
          <Button size='sm' variant='outline' onClick={resetFilters}>
            {t('Reset')}
          </Button>
          <Button
            variant='outline'
            size='icon'
            className='ml-auto'
            onClick={() => callsQuery.refetch()}
            title={t('Refresh')}
            aria-label={t('Refresh')}
          >
            <RefreshCw
              className={callsQuery.isFetching ? 'animate-spin' : ''}
            />
          </Button>
        </div>

        <div className='overflow-auto rounded-md border'>
          {callsQuery.isError ||
          (callsQuery.data && !callsQuery.data.success) ? (
            <div className='flex items-center gap-3 p-4 text-sm'>
              <span className='text-destructive'>
                {t('Failed to load Jev call records')}
              </span>
              <Button
                size='sm'
                variant='outline'
                onClick={() => callsQuery.refetch()}
              >
                {t('Retry')}
              </Button>
            </div>
          ) : null}
          {!callsQuery.isLoading && pageData && items.length === 0 ? (
            <p className='text-muted-foreground p-6 text-center text-sm'>
              {t('No Jev calls in this range')}
            </p>
          ) : null}
          {items.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-8' />
                  <TableHead>{t('Time')}</TableHead>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Request ID')}</TableHead>
                  <TableHead>{t('Model')}</TableHead>
                  <TableHead className='text-right'>RPM</TableHead>
                  <TableHead>{t('Result')}</TableHead>
                  <TableHead className='text-right'>{t('Score')}</TableHead>
                  <TableHead className='text-right'>{t('Duration')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((call) => (
                  <CallRow key={call.id} call={call} />
                ))}
              </TableBody>
            </Table>
          ) : null}
        </div>

        <div className='flex items-center justify-between text-sm'>
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
    </SettingsSection>
  )
}

function SummaryCards(props: { summary?: DistillJevCallsSummary }) {
  const { t } = useTranslation()
  const s = props.summary
  const cards: Array<{ label: string; value: string; tone?: string }> = [
    { label: t('Total calls'), value: s ? String(s.total) : '-' },
    { label: t('Succeeded'), value: s ? String(s.success) : '-' },
    {
      label: t('Failed'),
      value: s ? String(s.failed) : '-',
      tone: s && s.failed > 0 ? 'text-destructive' : undefined,
    },
    {
      label: t('Average score'),
      value: s?.avg_score == null ? '-' : s.avg_score.toFixed(3),
    },
    {
      label: t('Average duration'),
      value:
        s?.avg_duration_ms == null
          ? '-'
          : `${Math.round(s.avg_duration_ms)} ms`,
    },
  ]
  return (
    <div className='grid grid-cols-2 gap-3 md:grid-cols-5'>
      {cards.map((card) => (
        <div key={card.label} className='rounded-lg border p-3'>
          <div className='text-muted-foreground text-xs'>{card.label}</div>
          <div
            className={`mt-1 text-xl font-semibold tabular-nums ${card.tone ?? ''}`}
          >
            {card.value}
          </div>
        </div>
      ))}
    </div>
  )
}

function OutcomeBadge(props: { outcome: string; httpStatus: number }) {
  const { t } = useTranslation()
  const label = t(OUTCOME_LABEL[props.outcome] ?? props.outcome)
  if (props.outcome === 'success') {
    return <Badge variant='secondary'>{label}</Badge>
  }
  return (
    <Badge variant='destructive'>
      {label}
      {props.outcome === 'http_error' && props.httpStatus > 0
        ? ` ${props.httpStatus}`
        : ''}
    </Badge>
  )
}

function CallRow(props: { call: DistillJevCall }) {
  const { t } = useTranslation()
  const call = props.call
  const [open, setOpen] = useState(false)
  const failed = call.outcome !== 'success'
  const hasDetail = failed && Boolean(call.error)
  const hit = call.score != null && call.score >= call.threshold

  const copyRequestId = async () => {
    if (await copyToClipboard(call.request_id)) {
      toast.success(t('Copied to clipboard'))
    }
  }

  return (
    <>
      <TableRow>
        <TableCell className='w-8 px-1'>
          {hasDetail ? (
            <Button
              size='icon'
              variant='ghost'
              className='size-7'
              onClick={() => setOpen(!open)}
              aria-expanded={open}
              title={open ? t('Collapse') : t('Show error')}
              aria-label={open ? t('Collapse') : t('Show error')}
            >
              {open ? (
                <ChevronDown className='size-4' />
              ) : (
                <ChevronRight className='size-4' />
              )}
            </Button>
          ) : null}
        </TableCell>
        <TableCell className='whitespace-nowrap'>
          {formatTime(call.created_at)}
        </TableCell>
        <TableCell className='whitespace-nowrap'>
          {call.username || '-'}
          <span className='text-muted-foreground ml-1 text-xs'>
            #{call.user_id}
          </span>
        </TableCell>
        <TableCell>
          {call.request_id ? (
            <div className='flex items-center gap-1'>
              <code
                className='max-w-[16rem] truncate font-mono text-xs'
                title={call.request_id}
              >
                {call.request_id}
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
          ) : (
            '-'
          )}
        </TableCell>
        <TableCell className='font-mono text-xs'>{call.model || '-'}</TableCell>
        <TableCell className='text-right font-mono tabular-nums'>
          {call.rpm}
        </TableCell>
        <TableCell>
          <OutcomeBadge outcome={call.outcome} httpStatus={call.http_status} />
        </TableCell>
        <TableCell
          className='text-right font-mono tabular-nums'
          title={t('Threshold at the time: {{value}}', {
            value: call.threshold.toFixed(2),
          })}
        >
          {call.score == null ? (
            '-'
          ) : (
            <span className={hit ? 'text-destructive font-semibold' : ''}>
              {call.score.toFixed(3)}
            </span>
          )}
          <span className='text-muted-foreground ml-1 text-xs'>
            / {call.threshold.toFixed(2)}
          </span>
        </TableCell>
        <TableCell className='text-right font-mono whitespace-nowrap tabular-nums'>
          {call.duration_ms} ms
        </TableCell>
      </TableRow>
      {open && hasDetail ? (
        <TableRow className='bg-muted/30 hover:bg-muted/30'>
          <TableCell />
          <TableCell colSpan={COLUMN_COUNT - 1}>
            <pre className='text-destructive font-mono text-xs break-all whitespace-pre-wrap'>
              {call.error}
            </pre>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  )
}
