import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import {
  BadgeCheck,
  Check,
  Clipboard,
  Eraser,
  FileKey2,
  FileText,
  Gauge,
  Handshake,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Server,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { getLobeIcon } from '@/lib/lobe-icon'

import {
  CHANNEL_TYPE_OPTIONS,
  FIELD_DESCRIPTIONS,
  FIELD_PLACEHOLDERS,
} from '../channels/constants'
import { getChannelTypeConfig } from '../channels/lib/channel-type-config'
import {
  getChannelTypeIcon,
  getKeyPromptForType,
} from '../channels/lib/channel-utils'
import { BenchmarkRunDetails } from '../upstreams/components/benchmark-history-dialog'
import {
  countBenchmarkUnmetMetrics,
  type UpstreamInput,
} from '../upstreams/types'
import {
  createBusinessCooperation,
  deleteBusinessCooperation,
  discoverBusinessCooperationModels,
  listBusinessCooperations,
  updateBusinessCooperation,
  type BusinessCooperation,
} from './api'
import { BUSINESS_COOPERATION_MODEL_FETCHABLE_TYPES } from './constants'

const emptyInput: UpstreamInput = {
  type: 1,
  name: '',
  base_url: '',
  api_key: '',
  models: '',
  contact: '',
  remark: '',
}

const emptyBusinessCooperations: BusinessCooperation[] = []

function applicationStatusKey(status: string) {
  switch (status) {
    case 'benchmark_running':
      return 'Application status benchmarking'
    case 'pending_decision':
      return 'Application status pending review'
    case 'approved':
      return 'Application status approved'
    case 'rejected':
      return 'Application status failed'
    default:
      return 'Application status pending review'
  }
}

function isApplicationPendingReviewStatus(status: string) {
  return status === 'pending_review' || status === 'pending_decision'
}

const applicationStatusClasses: Record<string, string> = {
  approved:
    'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300',
  benchmark_running:
    'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300',
  pending_decision:
    'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300',
  rejected:
    'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300',
  pending_review:
    'border-slate-200 bg-slate-50 text-slate-700 dark:border-slate-800 dark:bg-slate-900/60 dark:text-slate-300',
}

function formatBenchmarkDuration(durationMS: number | undefined, t: TFunction) {
  if (!durationMS || durationMS < 0) return '-'
  const totalSeconds = Math.max(0, Math.round(durationMS / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (minutes === 0) return t('{{seconds}}s', { seconds })
  return t('{{minutes}}m {{seconds}}s', { minutes, seconds })
}

function formatBenchmarkScore(score: number | undefined) {
  if (score == null || !Number.isFinite(score)) return '-'
  return Number.isInteger(score)
    ? score.toLocaleString()
    : score.toLocaleString(undefined, { maximumFractionDigits: 2 })
}

function ChannelTypeLogo(props: { type: number; size?: number }) {
  const knownType = CHANNEL_TYPE_OPTIONS.some(
    (option) => option.value === props.type
  )
  if (!knownType) {
    return (
      <Server
        className='text-muted-foreground shrink-0'
        style={{ width: props.size ?? 16, height: props.size ?? 16 }}
        aria-hidden='true'
      />
    )
  }
  return (
    <span className='inline-flex shrink-0'>
      {getLobeIcon(`${getChannelTypeIcon(props.type)}.Color`, props.size ?? 16)}
    </span>
  )
}

export function BusinessCooperations() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [input, setInput] = useState(emptyInput)
  const [current, setCurrent] = useState<BusinessCooperation | null>(null)
  const [details, setDetails] = useState<BusinessCooperation | null>(null)
  const [keyword, setKeyword] = useState('')
  const [typeFilter, setTypeFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const query = useQuery({
    queryKey: ['business-cooperations'],
    queryFn: listBusinessCooperations,
    refetchInterval: (current) =>
      current.state.data?.data?.items.some((item) =>
        ['benchmark_running', 'pending_decision'].includes(item.status)
      )
        ? 5000
        : false,
  })
  const mutation = useMutation({
    mutationFn: (upstream: UpstreamInput) =>
      current
        ? updateBusinessCooperation(
            current.id,
            upstream,
            current.status === 'rejected'
          )
        : createBusinessCooperation(upstream),
    onSuccess: (result) => {
      if (!result.success) return
      setOpen(false)
      setInput(emptyInput)
      setCurrent(null)
      toast.success(t('Application submitted'))
      queryClient.invalidateQueries({ queryKey: ['business-cooperations'] })
    },
  })
  const modelMutation = useMutation({
    mutationFn: discoverBusinessCooperationModels,
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(result.message || t('Failed to fetch models'))
        return
      }
      const models = result.data?.models ?? []
      setAvailableModels(models)
      toast.success(
        t('Fetched {{count}} model(s) from upstream', { count: models.length })
      )
    },
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : t('Failed to fetch models')
      ),
  })
  const deleteMutation = useMutation({
    mutationFn: deleteBusinessCooperation,
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(t('Application deleted'))
      queryClient.invalidateQueries({ queryKey: ['business-cooperations'] })
    },
  })
  const items = query.data?.data?.items ?? emptyBusinessCooperations
  const filteredItems = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase()
    return items.filter((item) => {
      if (typeFilter !== 'all' && String(item.upstream.type) !== typeFilter) {
        return false
      }
      if (
        statusFilter !== 'all' &&
        (statusFilter === 'pending_review'
          ? !isApplicationPendingReviewStatus(item.status)
          : item.status !== statusFilter)
      ) {
        return false
      }
      if (!normalizedKeyword) return true
      return [item.upstream.name, item.upstream.base_url].some((value) =>
        value.toLowerCase().includes(normalizedKeyword)
      )
    })
  }, [items, keyword, statusFilter, typeFilter])
  const selectedModels = useMemo(
    () =>
      input.models
        .split(',')
        .map((model) => model.trim())
        .filter(Boolean),
    [input.models]
  )
  const relatedModels = useMemo(() => {
    const configured = getChannelTypeConfig(input.type).hints?.models ?? ''
    return configured
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean)
  }, [input.type])
  const modelOptions = useMemo(
    () =>
      [...new Set([...availableModels, ...selectedModels])].map((model) => ({
        value: model,
        label: model,
      })),
    [availableModels, selectedModels]
  )
  const channelTypeOptions = useMemo(
    () =>
      CHANNEL_TYPE_OPTIONS.map((option) => ({
        value: String(option.value),
        label: t(option.label),
        icon: <ChannelTypeLogo type={option.value} size={16} />,
      })),
    [t]
  )
  const cooperationTypeFilterOptions = useMemo(
    () => [
      { value: 'all', label: t('All Types') },
      ...CHANNEL_TYPE_OPTIONS.map((option) => ({
        value: String(option.value),
        label: t(option.label),
        icon: <ChannelTypeLogo type={option.value} size={16} />,
      })),
    ],
    [t]
  )
  const cooperationStatusOptions = useMemo(
    () => [
      { value: 'all', label: t('All statuses') },
      ...['pending_review', 'benchmark_running', 'approved', 'rejected'].map(
        (status) => ({
          value: status,
          label: t(applicationStatusKey(status)),
        })
      ),
    ],
    [t]
  )
  const openCreateDialog = () => {
    setCurrent(null)
    setInput(emptyInput)
    setAvailableModels([])
    setOpen(true)
  }
  const openEditDialog = (item: BusinessCooperation) => {
    setCurrent(item)
    setInput({
      type: item.upstream.type,
      name: item.upstream.name,
      base_url: item.upstream.base_url,
      api_key: '',
      models: item.upstream.models || '',
      contact: item.upstream.contact,
      remark: item.upstream.remark,
    })
    setAvailableModels([])
    setOpen(true)
  }
  const updateSelectedModels = (models: string[]) =>
    setInput((currentInput) => ({ ...currentInput, models: models.join(',') }))
  const fillModels = (models: string[]) =>
    updateSelectedModels([
      ...new Set(models.map((model) => model.trim()).filter(Boolean)),
    ])
  const handleFetchModels = () => {
    if (!input.api_key.trim()) {
      toast.error(t('Please enter API key first'))
      return
    }
    modelMutation.mutate(input)
  }
  const handleCopyModels = async () => {
    if (!input.models.trim()) {
      toast.info(t('No models to copy'))
      return
    }
    if (await copyToClipboard(input.models)) {
      toast.success(t('Copied to clipboard'))
    }
  }
  let dialogTitleKey = 'New cooperation application'
  if (current) {
    dialogTitleKey =
      current.status === 'rejected'
        ? 'Resubmit application'
        : 'Edit application'
  }

  let tableContent = (
    <div className='h-full overflow-y-auto'>
      <section className='flex min-h-full items-center py-2 sm:py-4'>
        <div className='grid w-full overflow-hidden rounded-md border lg:grid-cols-[minmax(0,1.05fr)_minmax(360px,0.95fr)]'>
          <div className='flex flex-col justify-center p-6 sm:p-8 lg:p-10'>
            <div className='bg-primary/10 text-primary mb-5 flex size-11 items-center justify-center rounded-md'>
              <Handshake aria-hidden='true' className='size-5' />
            </div>
            <h3 className='max-w-xl text-xl font-semibold sm:text-2xl'>
              {t('Connect your upstream to start cooperating')}
            </h3>
            <p className='text-muted-foreground mt-3 max-w-xl text-sm leading-6 sm:text-base'>
              {t(
                'Submit your channel type, API endpoint, credentials, contact details, and supported models for review.'
              )}
            </p>
            <Button className='mt-6 self-start' onClick={openCreateDialog}>
              <Plus data-icon='inline-start' />
              {t('New application')}
            </Button>
          </div>

          <div className='bg-muted/30 flex flex-col justify-center border-t px-6 py-5 sm:px-8 lg:border-t-0 lg:border-l lg:px-9 lg:py-8'>
            <div className='flex gap-4 py-4 first:pt-0 last:pb-0'>
              <div className='bg-background text-foreground flex size-9 shrink-0 items-center justify-center rounded-md border'>
                <FileKey2 aria-hidden='true' className='size-4' />
              </div>
              <div className='min-w-0'>
                <h4 className='text-sm font-medium'>
                  {t('Prepare upstream details')}
                </h4>
                <p className='text-muted-foreground mt-1 text-sm leading-5'>
                  {t(
                    'Provide the connection information and models required for verification.'
                  )}
                </p>
              </div>
            </div>
            <Separator />
            <div className='flex gap-4 py-4 first:pt-0 last:pb-0'>
              <div className='bg-background text-foreground flex size-9 shrink-0 items-center justify-center rounded-md border'>
                <Gauge aria-hidden='true' className='size-4' />
              </div>
              <div className='min-w-0'>
                <h4 className='text-sm font-medium'>
                  {t('Platform benchmark review')}
                </h4>
                <p className='text-muted-foreground mt-1 text-sm leading-5'>
                  {t(
                    'The platform verifies connectivity, stability, concurrency, and error rate.'
                  )}
                </p>
              </div>
            </div>
            <Separator />
            <div className='flex gap-4 py-4 first:pt-0 last:pb-0'>
              <div className='bg-background text-foreground flex size-9 shrink-0 items-center justify-center rounded-md border'>
                <BadgeCheck aria-hidden='true' className='size-4' />
              </div>
              <div className='min-w-0'>
                <h4 className='text-sm font-medium'>{t('Review result')}</h4>
                <p className='text-muted-foreground mt-1 text-sm leading-5'>
                  {t(
                    'Approved applications can be synchronized into channels; rejected applications can be revised and resubmitted.'
                  )}
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>
  )
  if (query.isLoading) {
    tableContent = (
      <div className='flex h-48 items-center justify-center'>
        <LoaderCircle className='animate-spin' />
      </div>
    )
  } else if (items.length > 0) {
    tableContent = (
      <div className='h-full overflow-y-auto'>
        <div className='space-y-4 pb-1'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'For providers with existing relay capabilities seeking users and traffic distribution through the platform.'
            )}
          </p>

          <section className='bg-card rounded-md border px-4 py-4 sm:px-5'>
            <div className='grid gap-4 lg:grid-cols-[minmax(0,2.1fr)_repeat(3,minmax(140px,1fr))]'>
              <div className='min-w-0 lg:pr-5'>
                <h3 className='text-sm font-semibold'>
                  {t('Cooperation guide')}
                </h3>
                <p className='text-muted-foreground mt-2 text-sm leading-5'>
                  {t(
                    'Submit your channel type, API endpoint, credentials, contact details, and supported models for review.'
                  )}{' '}
                  {t(
                    'The platform verifies connectivity, stability, concurrency, and error rate.'
                  )}{' '}
                  {t(
                    'Approved applications can be synchronized into channels; rejected applications can be revised and resubmitted.'
                  )}
                </p>
              </div>
              <div className='border-border/70 border-l pl-4'>
                <p className='text-muted-foreground text-xs'>
                  {t('Review criteria')}
                </p>
                <p className='mt-1 text-sm'>
                  {t('Stability, latency, available models, and error rate')}
                </p>
              </div>
              <div className='border-border/70 border-l pl-4'>
                <p className='text-muted-foreground text-xs'>
                  {t('Credential usage')}
                </p>
                <p className='mt-1 text-sm'>
                  {t('Only used for internal benchmark and acceptance testing')}
                </p>
              </div>
              <div className='border-border/70 border-l pl-4'>
                <p className='text-muted-foreground text-xs'>
                  {t('Result handling')}
                </p>
                <p className='mt-1 text-sm'>
                  {t(
                    'Approved for integration; rejected applications can be resubmitted'
                  )}
                </p>
              </div>
            </div>
          </section>

          <div className='flex flex-wrap items-center gap-2'>
            <div className='relative min-w-48 flex-1 sm:max-w-72'>
              <Input
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                placeholder={t('Search by name or URL...')}
                aria-label={t('Search by name or URL...')}
              />
            </div>
            <Combobox
              options={cooperationTypeFilterOptions}
              value={typeFilter}
              onValueChange={(value) => setTypeFilter(value ?? 'all')}
              placeholder={t('All Types')}
              searchPlaceholder={t('Search channel type...')}
              emptyText={t('No channel type found.')}
              className='w-48'
              openOnFocus={false}
            />
            <Combobox
              options={cooperationStatusOptions}
              value={statusFilter}
              onValueChange={(value) => setStatusFilter(value ?? 'all')}
              placeholder={t('All statuses')}
              searchPlaceholder={t('Search status...')}
              emptyText={t('No status found.')}
              className='w-32'
              openOnFocus={false}
            />
            <Button className='ml-auto' onClick={openCreateDialog}>
              <Plus data-icon='inline-start' />
              {t('New application')}
            </Button>
          </div>

          <div className='overflow-hidden rounded-md border'>
            <Table className='min-w-[920px]'>
              <TableHeader className='bg-muted/40'>
                <TableRow>
                  <TableHead>{t('Name')}</TableHead>
                  <TableHead>{t('Type')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Latest benchmark duration')}</TableHead>
                  <TableHead>{t('Latest benchmark score')}</TableHead>
                  <TableHead>{t('Created At')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredItems.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={7}
                      className='text-muted-foreground h-24 text-center'
                    >
                      {t('No matching applications')}
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredItems.map((item) => {
                    const benchmark = item.upstream.latest_benchmark
                    const typeOption = CHANNEL_TYPE_OPTIONS.find(
                      (option) => option.value === item.upstream.type
                    )
                    const score = benchmark?.overall_score
                    const unmetCount = benchmark
                      ? countBenchmarkUnmetMetrics(benchmark)
                      : 0
                    return (
                      <TableRow key={item.id}>
                        <TableCell className='max-w-64'>
                          <div className='min-w-0'>
                            <p className='truncate font-medium'>
                              {item.upstream.name}
                            </p>
                            <p className='text-muted-foreground truncate text-xs'>
                              {item.upstream.base_url}
                            </p>
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className='flex items-center gap-2'>
                            <ChannelTypeLogo
                              type={item.upstream.type}
                              size={16}
                            />
                            <span>{t(typeOption?.label ?? 'Unknown')}</span>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge
                            className={applicationStatusClasses[item.status]}
                            variant='outline'
                          >
                            {t(applicationStatusKey(item.status))}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {formatBenchmarkDuration(benchmark?.duration_ms, t)}
                        </TableCell>
                        <TableCell>
                          {score == null ? (
                            '-'
                          ) : (
                            <div className='flex flex-col gap-0.5'>
                              <span className='text-base font-semibold tabular-nums'>
                                {formatBenchmarkScore(score)} / 100
                              </span>
                              {unmetCount > 0 && (
                                <span className='text-muted-foreground text-xs whitespace-nowrap'>
                                  {t('{{count}} criteria unmet', {
                                    count: unmetCount,
                                  })}
                                </span>
                              )}
                            </div>
                          )}
                        </TableCell>
                        <TableCell>
                          {new Date(item.created_time * 1000).toLocaleString()}
                        </TableCell>
                        <TableCell>
                          <div className='flex justify-end gap-1'>
                            {item.status === 'pending_review' && (
                              <>
                                <Button
                                  variant='ghost'
                                  size='icon'
                                  title={t('Edit')}
                                  onClick={() => openEditDialog(item)}
                                >
                                  <Pencil />
                                </Button>
                                <Button
                                  variant='ghost'
                                  size='icon'
                                  title={t('Delete')}
                                  disabled={deleteMutation.isPending}
                                  onClick={() => {
                                    if (window.confirm(t('Confirm delete?'))) {
                                      deleteMutation.mutate(item.id)
                                    }
                                  }}
                                >
                                  <Trash2 />
                                </Button>
                              </>
                            )}
                            {item.status !== 'pending_review' &&
                              item.status !== 'benchmark_running' && (
                                <Button
                                  variant='ghost'
                                  size='sm'
                                  onClick={() => setDetails(item)}
                                >
                                  {t('Details')}
                                </Button>
                              )}
                            {item.status === 'rejected' && (
                              <Button
                                variant='ghost'
                                size='sm'
                                title={t('Resubmit')}
                                aria-label={t('Resubmit')}
                                disabled={mutation.isPending}
                                onClick={() => openEditDialog(item)}
                              >
                                {mutation.isPending &&
                                current?.id === item.id ? (
                                  <LoaderCircle
                                    className='animate-spin'
                                    data-icon='inline-start'
                                  />
                                ) : (
                                  <RotateCcw data-icon='inline-start' />
                                )}
                                {t('Resubmit')}
                              </Button>
                            )}
                            {item.status === 'benchmark_running' && (
                              <span className='text-muted-foreground px-2'>
                                -
                              </span>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </div>
        </div>
      </div>
    )
  }

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t('Business Cooperation')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>{tableContent}</SectionPageLayout.Content>
      </SectionPageLayout>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='flex max-h-[90vh] flex-col overflow-hidden sm:max-w-2xl'>
          <DialogHeader className='shrink-0'>
            <DialogTitle>{t(dialogTitleKey)}</DialogTitle>
            <DialogDescription>
              {t('Submit upstream access for administrator benchmark review.')}
            </DialogDescription>
          </DialogHeader>
          <div className='min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1'>
            <div className='grid gap-4 sm:grid-cols-2'>
              <div className='grid gap-2'>
                <Label htmlFor='cooperation-type'>{t('Type *')}</Label>
                <div className='relative'>
                  <span className='pointer-events-none absolute top-1/2 left-3 z-10 flex -translate-y-1/2'>
                    <ChannelTypeLogo type={input.type} size={18} />
                  </span>
                  <Combobox
                    id='cooperation-type'
                    options={channelTypeOptions}
                    value={String(input.type)}
                    onValueChange={(value) => {
                      const nextType = Number(value)
                      if (!Number.isInteger(nextType) || nextType <= 0) return
                      setAvailableModels([])
                      setInput((currentInput) => ({
                        ...currentInput,
                        type: nextType,
                      }))
                    }}
                    placeholder={t('Select channel type')}
                    searchPlaceholder={t('Search channel type...')}
                    emptyText={t('No channel type found.')}
                    className='pl-10'
                    openOnFocus={false}
                  />
                </div>
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='cooperation-name'>{t('Name *')}</Label>
                <Input
                  id='cooperation-name'
                  placeholder={t(FIELD_PLACEHOLDERS.NAME)}
                  value={input.name}
                  onChange={(event) =>
                    setInput((currentInput) => ({
                      ...currentInput,
                      name: event.target.value,
                    }))
                  }
                />
              </div>
              <div className='grid gap-2'>
                <Label htmlFor='cooperation-contact'>{t('Contact')}</Label>
                <Input
                  id='cooperation-contact'
                  value={input.contact}
                  onChange={(event) =>
                    setInput({ ...input, contact: event.target.value })
                  }
                />
              </div>
              <div className='border-border/60 rounded-lg border p-4 sm:col-span-2'>
                <div className='grid gap-4'>
                  <div>
                    <p className='text-sm font-medium'>{t('Credentials')}</p>
                    <p className='text-muted-foreground text-xs'>
                      {t('Authentication')}
                    </p>
                  </div>
                  <div className='grid gap-2'>
                    <Label htmlFor='cooperation-url'>
                      {t('API Base URL *')}
                    </Label>
                    <Input
                      id='cooperation-url'
                      type='url'
                      value={input.base_url}
                      onChange={(event) =>
                        setInput((currentInput) => ({
                          ...currentInput,
                          base_url: event.target.value,
                        }))
                      }
                      placeholder='https://api.example.com'
                    />
                    <p className='text-muted-foreground text-xs'>
                      {t('Base URL is required for this channel type')}
                    </p>
                  </div>
                  <div className='grid gap-2'>
                    <Label htmlFor='cooperation-key'>{t('API Key *')}</Label>
                    <Textarea
                      id='cooperation-key'
                      rows={3}
                      value={input.api_key}
                      onChange={(event) =>
                        setInput((currentInput) => ({
                          ...currentInput,
                          api_key: event.target.value,
                        }))
                      }
                      placeholder={t(
                        current
                          ? 'Leave empty to keep existing key'
                          : getKeyPromptForType(input.type)
                      )}
                    />
                    <p className='text-muted-foreground text-xs'>
                      {t(FIELD_DESCRIPTIONS.KEY)}
                    </p>
                  </div>
                </div>
              </div>
              <div className='border-border/60 bg-muted/10 rounded-lg border p-4 sm:col-span-2'>
                <div className='grid gap-3'>
                  <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                    <div>
                      <Label>{t('Models *')}</Label>
                      <p className='text-muted-foreground text-xs'>
                        {t('Select models or add custom ones')}
                      </p>
                    </div>
                    <Badge variant='outline'>
                      {t('Selected {{count}}', {
                        count: selectedModels.length,
                      })}
                    </Badge>
                  </div>
                  <MultiSelect
                    id='cooperation-models'
                    options={modelOptions}
                    selected={selectedModels}
                    onChange={updateSelectedModels}
                    placeholder={t('Select models or add custom ones')}
                    allowCreate
                    createLabel='Add custom model "{{value}}"'
                    maxVisibleChips={6}
                  />
                  <Separator />
                  <div>
                    <p className='mb-1 text-sm font-medium'>
                      {t('Quick actions')}
                    </p>
                    <p className='text-muted-foreground mb-2 text-xs'>
                      {t(
                        'Use presets or upstream discovery to populate the model list faster.'
                      )}
                    </p>
                    <div className='flex flex-wrap gap-2'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => fillModels(relatedModels)}
                        disabled={!relatedModels.length}
                      >
                        <FileText data-icon='inline-start' />
                        {t('Fill Related Models')}
                      </Button>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() => fillModels(availableModels)}
                        disabled={!availableModels.length}
                      >
                        <Check data-icon='inline-start' />
                        {t('Fill All Models')}
                      </Button>
                      {BUSINESS_COOPERATION_MODEL_FETCHABLE_TYPES.has(
                        input.type
                      ) && (
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={handleFetchModels}
                          disabled={modelMutation.isPending}
                        >
                          {modelMutation.isPending ? (
                            <RefreshCw
                              className='animate-spin'
                              data-icon='inline-start'
                            />
                          ) : (
                            <Sparkles data-icon='inline-start' />
                          )}
                          {t('Fetch from Upstream')}
                        </Button>
                      )}
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={handleCopyModels}
                        disabled={!selectedModels.length}
                      >
                        <Clipboard data-icon='inline-start' />
                        {t('Copy All')}
                      </Button>
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        onClick={() => updateSelectedModels([])}
                        disabled={!selectedModels.length}
                      >
                        <Eraser data-icon='inline-start' />
                        {t('Clear All')}
                      </Button>
                    </div>
                  </div>
                </div>
              </div>
              <div className='grid gap-2 sm:col-span-2'>
                <Label htmlFor='cooperation-remark'>{t('Remark')}</Label>
                <Textarea
                  id='cooperation-remark'
                  value={input.remark}
                  onChange={(event) =>
                    setInput({ ...input, remark: event.target.value })
                  }
                />
              </div>
            </div>
          </div>
          <DialogFooter className='shrink-0'>
            <Button variant='outline' onClick={() => setOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={
                !input.name ||
                !input.base_url ||
                !input.models.trim() ||
                (!current && !input.api_key) ||
                mutation.isPending
              }
              onClick={() => mutation.mutate(input)}
            >
              {mutation.isPending && <LoaderCircle className='animate-spin' />}
              {t('Submit')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {details?.upstream.latest_benchmark ? (
        <BenchmarkRunDetails
          run={details.upstream.latest_benchmark}
          upstreamName={details.upstream.name}
          onClose={() => setDetails(null)}
          summaryOnly
        />
      ) : (
        <Dialog
          open={details !== null}
          onOpenChange={(isOpen) => {
            if (!isOpen) setDetails(null)
          }}
        >
          <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
            <DialogHeader>
              <DialogTitle>{details?.upstream.name}</DialogTitle>
              <DialogDescription>{t('Application details')}</DialogDescription>
            </DialogHeader>
            {details && (
              <div className='grid gap-4 sm:grid-cols-2'>
                <div>
                  <p className='text-muted-foreground text-xs'>{t('Type')}</p>
                  <div className='mt-1 flex items-center gap-2 text-sm'>
                    <ChannelTypeLogo type={details.upstream.type} size={16} />
                    {t(
                      CHANNEL_TYPE_OPTIONS.find(
                        (option) => option.value === details.upstream.type
                      )?.label ?? 'Unknown'
                    )}
                  </div>
                </div>
                <div>
                  <p className='text-muted-foreground text-xs'>{t('Status')}</p>
                  <Badge
                    className={`mt-1 ${applicationStatusClasses[details.status]}`}
                    variant='outline'
                  >
                    {t(applicationStatusKey(details.status))}
                  </Badge>
                </div>
                <div className='sm:col-span-2'>
                  <p className='text-muted-foreground text-xs'>
                    {t('API URL')}
                  </p>
                  <p className='mt-1 text-sm break-all'>
                    {details.upstream.base_url}
                  </p>
                </div>
                <div>
                  <p className='text-muted-foreground text-xs'>
                    {t('Contact')}
                  </p>
                  <p className='mt-1 text-sm'>
                    {details.upstream.contact || '-'}
                  </p>
                </div>
                <div>
                  <p className='text-muted-foreground text-xs'>
                    {t('Created At')}
                  </p>
                  <p className='mt-1 text-sm'>
                    {new Date(details.created_time * 1000).toLocaleString()}
                  </p>
                </div>
                {details.reject_reason && (
                  <div className='sm:col-span-2'>
                    <p className='text-muted-foreground text-xs'>
                      {t('Review result')}
                    </p>
                    <p className='mt-1 text-sm'>{details.reject_reason}</p>
                  </div>
                )}
              </div>
            )}
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
