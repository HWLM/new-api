import { useQuery } from '@tanstack/react-query'
import { History, LoaderCircle } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { listBenchmarkRuns } from '../api'
import type { BenchmarkMetric, BenchmarkRun } from '../types'

type BenchmarkHistoryDialogProps = {
  open: boolean
  upstreamId: number | null
  upstreamName?: string
  onOpenChange: (open: boolean) => void
}

const benchmarkMetricOrder = [
  'RPM',
  'TPM',
  'TTFT_P50_8K',
  'TTFT_P90_8K',
  'TTFT_P50_32K',
  'TTFT_P90_32K',
  'TTFT_P50_128K',
  'TTFT_P90_128K',
  'OTPS_P50_128K',
]

function benchmarkRunStatusKey(status: string) {
  if (status === 'succeeded') return 'Succeeded'
  if (status === 'failed') return 'Failed'
  if (status === 'cancelled') return 'Cancelled'
  if (status === 'running') return 'Running'
  return 'Pending'
}

function benchmarkRunStatusVariant(
  status: string
): 'default' | 'secondary' | 'warning' | 'destructive' {
  if (status === 'succeeded') return 'secondary'
  if (status === 'failed') return 'destructive'
  if (status === 'running') return 'default'
  return 'warning'
}

function formatDateTime(timestamp: number | undefined) {
  return timestamp ? new Date(timestamp * 1000).toLocaleString() : '-'
}

function formatDuration(
  durationMS: number | undefined,
  t: ReturnType<typeof useTranslation>['t']
) {
  if (!durationMS || durationMS < 0) return '-'
  const totalSeconds = Math.max(0, Math.round(durationMS / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) {
    return t('{{hours}}h {{minutes}}m {{seconds}}s', {
      hours,
      minutes: minutes % 60,
      seconds,
    })
  }
  return minutes === 0
    ? t('{{seconds}}s', { seconds })
    : t('{{minutes}}m {{seconds}}s', { minutes, seconds })
}

function formatNumber(value: number | undefined) {
  if (value == null || !Number.isFinite(value)) return '-'
  return Number.isInteger(value)
    ? value.toLocaleString()
    : value.toLocaleString(undefined, { maximumFractionDigits: 2 })
}

function metricMet(
  actual: number | undefined,
  baseline: BenchmarkMetric | undefined
) {
  if (actual == null || baseline == null || !baseline.enabled) return null
  return baseline.direction === 'higher'
    ? actual >= baseline.admission
    : actual <= baseline.admission
}

function benchmarkResultLabel(
  met: boolean | null,
  t: ReturnType<typeof useTranslation>['t']
) {
  if (met == null) return '-'
  if (met) return t('Met')
  return t('Unmet')
}

function benchmarkResultVariant(
  met: boolean | null
): 'default' | 'secondary' | 'outline' | 'destructive' {
  if (met === false) return 'destructive'
  if (met === true) return 'secondary'
  return 'outline'
}

function DetailStat({
  label,
  value,
  valueClassName,
}: {
  label: string
  value: string | number
  valueClassName?: string
}) {
  return (
    <div className='min-w-0 space-y-0.5'>
      <p className='text-muted-foreground text-[11px] leading-4'>{label}</p>
      <p
        className={`truncate text-sm leading-5 font-medium ${valueClassName ?? ''}`}
      >
        {value}
      </p>
    </div>
  )
}

export function BenchmarkRunDetails({
  run,
  upstreamName,
  onClose,
  summaryOnly = false,
}: {
  run: BenchmarkRun
  upstreamName?: string
  onClose: () => void
  summaryOnly?: boolean
}) {
  const { t } = useTranslation()
  const metricNames = useMemo(() => {
    const names = new Set([
      ...benchmarkMetricOrder,
      ...Object.keys(run.baseline ?? {}),
      ...Object.keys(run.metrics ?? {}),
      ...Object.keys(run.scores ?? {}),
    ])
    return [...names].sort((left, right) => {
      const leftIndex = benchmarkMetricOrder.indexOf(left)
      const rightIndex = benchmarkMetricOrder.indexOf(right)
      if (leftIndex === -1 && rightIndex === -1) {
        return left.localeCompare(right)
      }
      if (leftIndex === -1) {
        return 1
      }
      if (rightIndex === -1) {
        return -1
      }
      return leftIndex - rightIndex
    })
  }, [run.baseline, run.metrics, run.scores])

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='grid max-h-[min(860px,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)] overflow-hidden p-0 sm:max-w-5xl'>
        <DialogHeader className='border-b px-5 py-3'>
          <DialogTitle>{t('Details')}</DialogTitle>
          <DialogDescription className='hidden'>
            {t('Run')} #{run.id} · {run.model || t('Model')}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className='min-h-0'>
          <div className='space-y-3 p-3 sm:p-4'>
            {summaryOnly ? (
              <>
                <div className='grid gap-x-6 gap-y-2 border-b px-4 py-3 sm:grid-cols-3'>
                  <div className='flex min-w-0 items-center gap-2'>
                    <span className='text-muted-foreground shrink-0 text-xs'>
                      {t('Start Time')}:
                    </span>
                    <span className='truncate text-sm font-medium'>
                      {formatDateTime(run.started_time)}
                    </span>
                  </div>
                  <div className='flex min-w-0 items-center gap-2'>
                    <span className='text-muted-foreground shrink-0 text-xs'>
                      {t('Completed at')}:
                    </span>
                    <span className='truncate text-sm font-medium'>
                      {formatDateTime(run.finished_time)}
                    </span>
                  </div>
                  <div className='flex min-w-0 items-center gap-2'>
                    <span className='text-muted-foreground shrink-0 text-xs'>
                      {t('Model')}:
                    </span>
                    <span className='truncate text-sm font-medium'>
                      {run.model || '-'}
                    </span>
                  </div>
                </div>
                <div className='grid gap-2 sm:grid-cols-2'>
                  <div className='rounded-xl border px-3 py-2.5'>
                    <DetailStat
                      label={t('Score')}
                      value={
                        run.overall_score == null
                          ? '-'
                          : `${formatNumber(run.overall_score)} / 100`
                      }
                      valueClassName='text-primary text-xl leading-7'
                    />
                  </div>
                  <div className='rounded-xl border px-3 py-2.5'>
                    <DetailStat
                      label={t('Duration')}
                      value={formatDuration(run.duration_ms, t)}
                      valueClassName='text-xl leading-7'
                    />
                  </div>
                </div>
              </>
            ) : (
              <div className='space-y-3 p-3 sm:p-4'>
                <div className='grid gap-2 sm:grid-cols-2'>
                  <div className='bg-muted/20 rounded-md border px-3 py-2.5'>
                    <DetailStat
                      label={t('Score')}
                      value={
                        run.overall_score == null
                          ? '-'
                          : `${formatNumber(run.overall_score)} / 100`
                      }
                      valueClassName='text-primary text-2xl leading-7'
                    />
                  </div>
                  <div className='bg-muted/20 rounded-md border px-3 py-2.5'>
                    <DetailStat
                      label={t('Duration')}
                      value={formatDuration(run.duration_ms, t)}
                      valueClassName='text-2xl leading-7'
                    />
                  </div>
                </div>

                <div className='bg-muted/10 rounded-md border px-4 py-3'>
                  <div className='grid grid-cols-2 gap-x-5 gap-y-2 sm:grid-cols-4'>
                    <DetailStat
                      label={t('Start Time')}
                      value={formatDateTime(run.started_time)}
                    />
                    <DetailStat
                      label={t('Completed at')}
                      value={formatDateTime(run.finished_time)}
                    />
                    <DetailStat label={t('Model')} value={run.model || '-'} />
                    <DetailStat
                      label={t('Channel Name')}
                      value={upstreamName || '-'}
                    />
                    <DetailStat
                      label={t('Benchmark profile')}
                      value={run.profile_name || '-'}
                    />
                    <DetailStat
                      label={t('Status Code')}
                      value={run.status_code ?? '-'}
                    />
                    <DetailStat
                      label={t('Parallel requests')}
                      value={run.concurrency ?? '-'}
                    />
                    <DetailStat
                      label={t('Status')}
                      value={t(benchmarkRunStatusKey(run.status))}
                      valueClassName='truncate'
                    />
                  </div>
                </div>

                {run.error && (
                  <div className='border-destructive/30 bg-destructive/5 rounded-md border px-3 py-2'>
                    <p className='mb-0.5 text-xs font-medium'>{t('Error')}</p>
                    <p className='text-destructive text-xs leading-5 break-words'>
                      {run.error}
                    </p>
                  </div>
                )}
              </div>
            )}

            <div className='overflow-x-auto rounded-md border'>
              <Table className='min-w-[760px] text-xs'>
                <TableHeader>
                  <TableRow className='bg-muted/60 hover:bg-muted/60'>
                    <TableHead className='h-9 px-3 text-xs font-medium'>
                      {t('Metric')}
                    </TableHead>
                    <TableHead className='h-9 px-3 text-right text-xs font-medium whitespace-nowrap'>
                      {t('Actual')}
                    </TableHead>
                    <TableHead className='h-9 px-3 text-right text-xs font-medium whitespace-nowrap'>
                      {t('Admission threshold')}
                    </TableHead>
                    <TableHead className='h-9 px-3 text-right text-xs font-medium whitespace-nowrap'>
                      {t('Full score')}
                    </TableHead>
                    <TableHead className='h-9 px-3 text-right text-xs font-medium'>
                      {t('Weight')}
                    </TableHead>
                    <TableHead className='h-9 px-3 text-right text-xs font-medium'>
                      {t('Score')}
                    </TableHead>
                    <TableHead className='h-9 px-3 text-center text-xs font-medium whitespace-nowrap'>
                      {t('Benchmark result')}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {metricNames.map((name) => {
                    const actual = run.metrics?.[name]
                    const baseline = run.baseline?.[name]
                    const score = run.scores?.[name]
                    const met = metricMet(actual, baseline)
                    return (
                      <TableRow
                        key={name}
                        className={
                          met === false ? 'bg-destructive/10' : undefined
                        }
                      >
                        <TableCell className='px-3 py-2 font-medium whitespace-nowrap'>
                          {name}
                        </TableCell>
                        <TableCell className='px-3 py-2 text-right whitespace-nowrap'>
                          {formatNumber(actual)}
                        </TableCell>
                        <TableCell className='px-3 py-2 text-right whitespace-nowrap'>
                          {formatNumber(baseline?.admission)}
                        </TableCell>
                        <TableCell className='px-3 py-2 text-right whitespace-nowrap'>
                          {formatNumber(baseline?.full)}
                        </TableCell>
                        <TableCell className='px-3 py-2 text-right whitespace-nowrap'>
                          {formatNumber(baseline?.weight)}
                        </TableCell>
                        <TableCell className='px-3 py-2 text-right whitespace-nowrap'>
                          {formatNumber(score)}
                        </TableCell>
                        <TableCell className='px-3 py-2 text-center'>
                          <Badge
                            variant={benchmarkResultVariant(met)}
                            className='px-2 py-0 text-[11px]'
                          >
                            {benchmarkResultLabel(met, t)}
                          </Badge>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

export function BenchmarkHistoryDialog({
  open,
  upstreamId,
  upstreamName,
  onOpenChange,
}: BenchmarkHistoryDialogProps) {
  const { t } = useTranslation()
  const [selectedRun, setSelectedRun] = useState<BenchmarkRun | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const historyQuery = useQuery({
    queryKey: ['upstream-benchmark-history', upstreamId],
    queryFn: () => listBenchmarkRuns(upstreamId as number),
    enabled: open && upstreamId !== null,
  })
  const runs = historyQuery.data?.data ?? []

  useEffect(() => {
    setSelectedRun(null)
    setDetailOpen(false)
  }, [upstreamId])

  const historyTable = (
    <div className='overflow-x-auto rounded-md border'>
      <Table className='min-w-[620px] text-sm'>
        <TableHeader>
          <TableRow className='bg-muted/60 hover:bg-muted/60'>
            <TableHead className='h-9 px-3 text-xs font-medium'>
              {t('Run')}
            </TableHead>
            <TableHead className='h-9 px-3 text-xs font-medium'>
              {t('Model')}
            </TableHead>
            <TableHead className='h-9 px-3 text-xs font-medium'>
              {t('Status')}
            </TableHead>
            <TableHead className='h-9 px-3 text-right text-xs font-medium'>
              {t('Score')}
            </TableHead>
            <TableHead className='h-9 px-3 text-xs font-medium whitespace-nowrap'>
              {t('Completed at')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {runs.map((run) => (
            <TableRow
              key={run.id}
              tabIndex={0}
              className='hover:bg-muted/50 focus-visible:bg-muted/50 cursor-pointer focus-visible:outline-none'
              aria-label={`${t('Details')} #${run.id}`}
              onClick={() => {
                setSelectedRun(run)
                setDetailOpen(true)
                onOpenChange(false)
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  setSelectedRun(run)
                  setDetailOpen(true)
                  onOpenChange(false)
                }
              }}
            >
              <TableCell className='px-3 py-2.5 font-medium whitespace-nowrap'>
                #{run.id}
              </TableCell>
              <TableCell className='max-w-52 truncate px-3 py-2.5'>
                {run.model || '-'}
              </TableCell>
              <TableCell className='px-3 py-2.5'>
                <Badge
                  variant={benchmarkRunStatusVariant(run.status)}
                  className='px-2 py-0 text-[11px]'
                >
                  {t(benchmarkRunStatusKey(run.status))}
                </Badge>
              </TableCell>
              <TableCell className='px-3 py-2.5 text-right tabular-nums'>
                {run.overall_score ?? '-'}
              </TableCell>
              <TableCell className='text-muted-foreground px-3 py-2.5 whitespace-nowrap'>
                {formatDateTime(run.finished_time)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
  let historyBody = historyTable
  if (historyQuery.isLoading) {
    historyBody = (
      <div className='flex min-h-72 items-center justify-center'>
        <LoaderCircle className='text-muted-foreground size-5 animate-spin' />
      </div>
    )
  } else if (runs.length === 0) {
    historyBody = (
      <EmptyState icon={History} title={t('No Data')} className='min-h-72' />
    )
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className='grid max-h-[min(700px,calc(100vh-2rem))] grid-rows-[auto_minmax(0,1fr)] overflow-hidden p-0 sm:max-w-3xl'>
          <DialogHeader className='border-b px-5 py-3'>
            <DialogTitle>{t('Benchmark history')}</DialogTitle>
          </DialogHeader>
          <ScrollArea className='min-h-0'>
            <div className='p-3 sm:p-4'>{historyBody}</div>
          </ScrollArea>
        </DialogContent>
      </Dialog>
      {selectedRun && detailOpen && (
        <BenchmarkRunDetails
          run={selectedRun}
          upstreamName={upstreamName}
          onClose={() => {
            setDetailOpen(false)
            setSelectedRun(null)
          }}
        />
      )}
    </>
  )
}
