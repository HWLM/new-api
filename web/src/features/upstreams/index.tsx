import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  Clipboard,
  CircleCheck,
  CircleX,
  CircleStop,
  CirclePlus,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Clock3,
  FileText,
  Gauge,
  History,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Send,
  Server,
  Settings2,
  SlidersHorizontal,
  Sparkles,
  Eraser,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import { MultiSelect } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
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
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
import { getPageNumbers } from '@/lib/utils'

import { BUSINESS_COOPERATION_MODEL_FETCHABLE_TYPES } from '../business-cooperations/constants'
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
import {
  cancelBenchmark,
  createUpstream,
  getAutoSyncConfig,
  getBenchmarkProfile,
  discoverUpstreamModels,
  listUpstreamModels,
  listUpstreams,
  rejectUpstream,
  startBenchmark,
  syncUpstreams,
  updateAutoSyncConfig,
  updateBenchmarkProfile,
  updateUpstream,
} from './api'
import { BenchmarkHistoryDialog } from './components/benchmark-history-dialog'
import {
  countBenchmarkUnmetMetrics,
  type AutoSyncConfig,
  type BenchmarkProfile,
  type UpstreamCandidate,
  type UpstreamInput,
} from './types'

const emptyInput: UpstreamInput = {
  type: 1,
  name: '',
  base_url: '',
  api_key: '',
  models: '',
  contact: '',
  remark: '',
}
const emptyUpstreams: UpstreamCandidate[] = []
const pageSizeOptions = [10, 20, 50, 100] as const
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

type FilterOption = {
  value: string
  label: string
}

function SearchFilter({
  title,
  value,
  options,
  searchPlaceholder,
  emptyText,
  clearText,
  onValueChange,
  className,
}: {
  title: string
  value: string
  options: FilterOption[]
  searchPlaceholder: string
  emptyText: string
  clearText: string
  onValueChange: (value: string) => void
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const selectedOptions = options.filter(
    (option) => option.value === value && option.value !== 'all'
  )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            size='sm'
            className={`h-8 border-dashed ${className ?? ''}`}
          />
        }
      >
        <CirclePlus className='size-4' />
        {title}
        {selectedOptions.length > 0 && (
          <>
            <Separator orientation='vertical' className='mx-1 h-4' />
            {selectedOptions.map((option) => (
              <Badge
                key={option.value}
                variant='secondary'
                className='rounded-sm px-1 font-normal'
              >
                {option.label}
              </Badge>
            ))}
          </>
        )}
      </PopoverTrigger>
      <PopoverContent align='start' className='max-w-[360px] min-w-[200px] p-0'>
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList className='max-h-72'>
            <CommandEmpty>{emptyText}</CommandEmpty>
            <CommandGroup>
              {options.map((option) => {
                const isSelected = option.value === value
                return (
                  <CommandItem
                    key={option.value}
                    value={`${option.label} ${option.value}`}
                    onSelect={() => {
                      onValueChange(option.value)
                      setOpen(false)
                    }}
                  >
                    <span
                      className={`flex size-4 items-center justify-center rounded-sm border ${
                        isSelected
                          ? 'border-primary bg-primary text-primary-foreground'
                          : 'border-input opacity-60 [&_svg]:invisible'
                      }`}
                    >
                      <Check className='size-3' />
                    </span>
                    <span className='min-w-0 flex-1 truncate'>
                      {option.label}
                    </span>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          </CommandList>
          {value !== 'all' && (
            <div className='border-border border-t p-1'>
              <button
                type='button'
                className='text-muted-foreground hover:bg-muted hover:text-foreground flex h-8 w-full items-center justify-center rounded-sm px-2 text-sm transition-colors'
                onClick={() => {
                  onValueChange('all')
                  setOpen(false)
                }}
              >
                {clearText}
              </button>
            </div>
          )}
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function benchmarkStatusKey(status: string) {
  switch (status) {
    case 'running':
      return 'Benchmark status running'
    case 'completed':
      return 'Benchmark status completed'
    default:
      return 'Benchmark status pending'
  }
}

function benchmarkStatusVariant(
  status: string
): 'default' | 'secondary' | 'warning' {
  if (status === 'running') return 'default'
  if (status === 'completed') return 'secondary'
  return 'warning'
}

function formatDateTime(timestamp: number | undefined) {
  return timestamp ? new Date(timestamp * 1000).toLocaleString() : '-'
}

function formatBenchmarkDuration(
  durationMS: number | undefined,
  t: ReturnType<typeof useTranslation>['t']
) {
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

export function Upstreams() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(100)
  const [keyword, setKeyword] = useState('')
  const [isKeywordComposing, setIsKeywordComposing] = useState(false)
  const [status, setStatus] = useState('all')
  const [typeFilter, setTypeFilter] = useState('all')
  const [officialFilter, setOfficialFilter] = useState('all')
  const [scoreOperator, setScoreOperator] = useState<'gte' | 'lte'>('gte')
  const [minimumScore, setMinimumScore] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [profileOpen, setProfileOpen] = useState(false)
  const [autoSyncOpen, setAutoSyncOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [benchmarkOpen, setBenchmarkOpen] = useState(false)
  const [benchmarkUpstreamId, setBenchmarkUpstreamId] = useState<number | null>(
    null
  )
  const [benchmarkModel, setBenchmarkModel] = useState('')
  const [benchmarkConcurrency, setBenchmarkConcurrency] = useState(1)
  const [syncOpen, setSyncOpen] = useState(false)
  const [syncTag, setSyncTag] = useState('')
  const [rejectOpen, setRejectOpen] = useState(false)
  const [rejectUpstreamId, setRejectUpstreamId] = useState<number | null>(null)
  const [rejectReason, setRejectReason] = useState('')
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [historyUpstreamId, setHistoryUpstreamId] = useState<number | null>(
    null
  )
  const [historyUpstreamName, setHistoryUpstreamName] = useState('')
  const [profileDraft, setProfileDraft] = useState<BenchmarkProfile | null>(
    null
  )
  const [editingProfileCell, setEditingProfileCell] = useState<{
    name: string
    field: 'admission' | 'full' | 'weight'
  } | null>(null)
  const [autoSyncDraft, setAutoSyncDraft] = useState<AutoSyncConfig>({
    enabled: false,
    channel_tag: '',
    min_score: 80,
  })
  const profileQuery = useQuery({
    queryKey: ['upstream-benchmark-profile'],
    queryFn: getBenchmarkProfile,
    enabled: profileOpen,
  })
  const autoSyncQuery = useQuery({
    queryKey: ['upstream-auto-sync'],
    queryFn: getAutoSyncConfig,
    enabled: autoSyncOpen,
  })
  const modelsQuery = useQuery({
    queryKey: ['upstream-models', benchmarkUpstreamId],
    queryFn: () => listUpstreamModels(benchmarkUpstreamId as number),
    enabled: benchmarkOpen && benchmarkUpstreamId !== null,
  })
  const [input, setInput] = useState<UpstreamInput>(emptyInput)

  const queryKey = useMemo(
    () => ['upstreams', page, pageSize, keyword, status],
    [keyword, page, pageSize, status]
  )
  const query = useQuery({
    queryKey,
    queryFn: () =>
      listUpstreams({
        p: page,
        page_size: pageSize,
        keyword: keyword || undefined,
        benchmark_status: status === 'all' ? undefined : status || undefined,
      }),
    refetchInterval: (current) =>
      current.state.data?.data?.items.some(
        (item) =>
          item.latest_benchmark?.status === 'pending' ||
          item.latest_benchmark?.status === 'running'
      )
        ? 3000
        : false,
  })

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
        icon: (
          <span className='inline-flex shrink-0'>
            {getLobeIcon(`${getChannelTypeIcon(option.value)}.Color`, 16)}
          </span>
        ),
      })),
    [t]
  )

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['upstreams'] })
  const createMutation = useMutation({
    mutationFn: (value: UpstreamInput) =>
      editingId ? updateUpstream(editingId, value) : createUpstream(value),
    onSuccess: (result) => {
      if (!result.success) return
      setDialogOpen(false)
      setInput(emptyInput)
      setEditingId(null)
      toast.success(t(editingId ? 'Saved successfully' : 'Upstream created'))
      refresh()
    },
  })
  const modelMutation = useMutation({
    mutationFn: discoverUpstreamModels,
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
  const benchmarkMutation = useMutation({
    mutationFn: ({
      id,
      model,
      concurrency,
    }: {
      id: number
      model: string
      concurrency: number
    }) => startBenchmark(id, { model, concurrency }),
    onSuccess: (result) => {
      if (!result.success) return
      setBenchmarkOpen(false)
      toast.success(t('Benchmark queued'))
      refresh()
    },
  })
  const cancelBenchmarkMutation = useMutation({
    mutationFn: cancelBenchmark,
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(t('Cancelled'))
      refresh()
    },
  })
  const syncMutation = useMutation({
    mutationFn: ({ ids, tag }: { ids: number[]; tag: string }) =>
      syncUpstreams(ids, tag),
    onSuccess: (result) => {
      if (!result.success) return
      setSyncOpen(false)
      setSelectedIds([])
      toast.success(t('Upstream synchronized'))
      refresh()
    },
  })
  const rejectMutation = useMutation({
    mutationFn: ({ id, reason }: { id: number; reason: string }) =>
      rejectUpstream(id, reason),
    onSuccess: (result) => {
      if (!result.success) return
      setRejectOpen(false)
      setRejectUpstreamId(null)
      setRejectReason('')
      toast.success(t('Benchmark marked as failed'))
      refresh()
    },
  })
  const profileMutation = useMutation({
    mutationFn: updateBenchmarkProfile,
    onSuccess: (result) => {
      if (!result.success) return
      if (result.data) {
        setProfileDraft(result.data)
        queryClient.setQueryData(['upstream-benchmark-profile'], result)
      }
      setProfileOpen(false)
      toast.success(t('Benchmark profile saved'))
    },
  })
  const autoSyncMutation = useMutation({
    mutationFn: updateAutoSyncConfig,
    onSuccess: (result) => {
      if (!result.success) return
      if (result.data) {
        queryClient.setQueryData(['upstream-auto-sync'], result)
      }
      setAutoSyncOpen(false)
      toast.success(t('Automatic sync settings saved'))
      refresh()
    },
  })
  useEffect(() => {
    if (profileQuery.data?.data && profileOpen && !profileDraft) {
      setProfileDraft(profileQuery.data.data)
    }
  }, [profileDraft, profileOpen, profileQuery.data])
  useEffect(() => {
    setEditingProfileCell(null)
  }, [profileOpen])
  useEffect(() => {
    if (autoSyncQuery.data?.data && autoSyncOpen) {
      setAutoSyncDraft(autoSyncQuery.data.data)
    }
  }, [autoSyncOpen, autoSyncQuery.data])
  useEffect(() => {
    const firstModel = modelsQuery.data?.data?.models?.[0]
    if (benchmarkOpen && firstModel && !benchmarkModel) {
      setBenchmarkModel(firstModel)
    }
  }, [benchmarkModel, benchmarkOpen, modelsQuery.data])

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

  const items = query.data?.data?.items ?? emptyUpstreams
  const total = query.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const pageNumbers = getPageNumbers(page, totalPages)
  const pendingCount = items.filter(
    (item) => item.benchmark_status === 'pending'
  ).length
  const officialCount = items.filter((item) => item.channel_id).length
  const filteredItems = useMemo(() => {
    const score = minimumScore.trim() === '' ? null : Number(minimumScore)
    return items.filter((item) => {
      if (status !== 'all' && item.benchmark_status !== status) return false
      if (typeFilter !== 'all' && String(item.type) !== typeFilter) return false
      if (officialFilter === 'yes' && !item.channel_id) return false
      if (officialFilter === 'no' && item.channel_id) return false
      if (
        score !== null &&
        (!Number.isFinite(score) ||
          (scoreOperator === 'gte'
            ? (item.latest_benchmark?.overall_score ?? 0) < score
            : (item.latest_benchmark?.overall_score ?? 0) > score))
      ) {
        return false
      }
      return true
    })
  }, [
    items,
    minimumScore,
    officialFilter,
    scoreOperator,
    status,
    typeFilter,
  ])
  const selectableItems = filteredItems.filter(
    (item) => item.benchmark_status === 'completed' && !item.channel_id
  )
  const allSelectableSelected =
    selectableItems.length > 0 &&
    selectableItems.every((item) => selectedIds.includes(item.id))
  const typeFilterOptions = useMemo(
    () =>
      CHANNEL_TYPE_OPTIONS.map((option) => ({
        value: String(option.value),
        label: t(option.label),
      })),
    [t]
  )
  const officialFilterOptions = useMemo(
    () => [
      { value: 'yes', label: t('Yes') },
      { value: 'no', label: t('No') },
    ],
    [t]
  )
  const benchmarkStatusOptions = useMemo(
    () => [
      { value: 'pending', label: t('Benchmark status pending') },
      { value: 'running', label: t('Benchmark status running') },
      { value: 'completed', label: t('Benchmark status completed') },
    ],
    [t]
  )
  const profileData = profileQuery.data?.data
  const profile = profileDraft ?? profileData
  const hasProfileData = Boolean(
    profileData && Object.keys(profileData.metrics ?? {}).length > 0
  )
  const profileChanged = Boolean(
    profile &&
    profileData &&
    JSON.stringify(profile.metrics) !== JSON.stringify(profileData.metrics)
  )

  let tableContent = (
    <div className='bg-muted/10 flex min-h-0 flex-1 items-center justify-center rounded-md border'>
      <EmptyState title={t('No upstreams')} />
    </div>
  )
  if (query.isLoading) {
    tableContent = (
      <div className='bg-muted/10 flex min-h-0 flex-1 items-center justify-center rounded-md border'>
        <span className='animate-spin'>
          <LoaderCircle aria-hidden='true' />
        </span>
      </div>
    )
  } else if (items.length > 0) {
    tableContent = (
      <div className='border-border/70 bg-background min-h-0 flex-1 overflow-auto rounded-md border shadow-xs'>
        <Table className='min-w-[1360px]' containerClassName='overflow-visible'>
          <TableHeader className='bg-muted/90 sticky top-0 z-10 backdrop-blur-sm'>
            <TableRow>
              <TableHead className='w-10'>
                <Checkbox
                  checked={allSelectableSelected}
                  onCheckedChange={(checked) =>
                    setSelectedIds(
                      checked === true
                        ? selectableItems.map((item) => item.id)
                        : []
                    )
                  }
                  aria-label={t('Select all')}
                />
              </TableHead>
              <TableHead className='min-w-64'>{t('Name')}</TableHead>
              <TableHead>{t('Type')}</TableHead>
              <TableHead>{t('Official use')}</TableHead>
              <TableHead>{t('Latest benchmark start')}</TableHead>
              <TableHead>{t('Latest benchmark duration')}</TableHead>
              <TableHead>{t('Latest benchmark score')}</TableHead>
              <TableHead>{t('Benchmark status')}</TableHead>
              <TableHead>{t('Created At')}</TableHead>
              <TableHead>{t('Contact')}</TableHead>
              <TableHead>{t('Remark')}</TableHead>
              <TableHead className='w-[168px] text-right'>
                {t('Actions')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredItems.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={12}
                  className='text-muted-foreground h-24 text-center'
                >
                  {t('No matching upstreams')}
                </TableCell>
              </TableRow>
            ) : (
              filteredItems.map((item) => {
                const benchmark = item.latest_benchmark
                const unmetCount = benchmark
                  ? countBenchmarkUnmetMetrics(benchmark)
                  : 0
                const benchmarkActive =
                  benchmark?.status === 'pending' ||
                  benchmark?.status === 'running'
                const canSync =
                  item.benchmark_status === 'completed' && !item.channel_id
                const typeOption = CHANNEL_TYPE_OPTIONS.find(
                  (option) => option.value === item.type
                )
                return (
                  <TableRow key={item.id} className='content-auto'>
                    <TableCell>
                      <Checkbox
                        checked={selectedIds.includes(item.id)}
                        disabled={
                          item.benchmark_status !== 'completed' ||
                          Boolean(item.channel_id)
                        }
                        onCheckedChange={(checked) =>
                          setSelectedIds((current) =>
                            checked === true
                              ? [...new Set([...current, item.id])]
                              : current.filter((id) => id !== item.id)
                          )
                        }
                        aria-label={item.name}
                      />
                    </TableCell>
                    <TableCell className='min-w-64'>
                      <div className='min-w-0'>
                        <div className='flex min-w-0 items-center gap-2'>
                          <p className='truncate font-semibold'>{item.name}</p>
                          <Badge
                            className={`h-4 shrink-0 px-1.5 text-[10px] ${
                              item.source === 'admin'
                                ? 'border-destructive/30 bg-destructive/10 text-destructive'
                                : 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300'
                            }`}
                            variant='outline'
                          >
                            {t(
                              item.source === 'admin'
                                ? 'Admin entry'
                                : 'Self submission'
                            )}
                          </Badge>
                        </div>
                        <p
                          className='text-muted-foreground mt-1 max-w-64 truncate text-xs'
                          title={item.base_url}
                        >
                          {item.base_url}
                        </p>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center gap-2'>
                        {typeOption ? (
                          <span className='inline-flex size-5 shrink-0 items-center justify-center'>
                            {getLobeIcon(
                              `${getChannelTypeIcon(item.type)}.Color`,
                              18
                            )}
                          </span>
                        ) : (
                          <Server
                            className='text-muted-foreground size-4 shrink-0'
                            aria-hidden='true'
                          />
                        )}
                        <span className='font-medium'>
                          {t(typeOption?.label ?? 'Unknown')}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={item.channel_id ? 'secondary' : 'outline'}
                        className='min-w-10'
                      >
                        {item.channel_id ? t('Yes') : t('No')}
                      </Badge>
                    </TableCell>
                    <TableCell className='whitespace-nowrap'>
                      {formatDateTime(benchmark?.started_time)}
                    </TableCell>
                    <TableCell className='whitespace-nowrap'>
                      {formatBenchmarkDuration(benchmark?.duration_ms, t)}
                    </TableCell>
                    <TableCell>
                      {benchmark?.overall_score == null ? (
                        <span className='text-muted-foreground'>-</span>
                      ) : (
                        <div className='flex flex-col gap-0.5'>
                          <span className='text-base font-semibold tabular-nums'>
                            {formatBenchmarkScore(benchmark.overall_score)} /
                            100
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
                      <div className='flex items-center gap-1'>
                        <Badge
                          variant={benchmarkStatusVariant(
                            item.benchmark_status
                          )}
                        >
                          {t(benchmarkStatusKey(item.benchmark_status))}
                        </Badge>
                        {benchmarkActive && (
                          <Button
                            variant='ghost'
                            size='icon-xs'
                            title={t('Cancel')}
                            aria-label={t('Cancel')}
                            disabled={cancelBenchmarkMutation.isPending}
                            onClick={() =>
                              cancelBenchmarkMutation.mutate(item.id)
                            }
                          >
                            <CircleStop />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className='whitespace-nowrap'>
                      {formatDateTime(item.created_time)}
                    </TableCell>
                    <TableCell className='max-w-40 truncate'>
                      {item.contact || '-'}
                    </TableCell>
                    <TableCell className='max-w-48 truncate'>
                      {item.remark || '-'}
                    </TableCell>
                    <TableCell className='w-[168px]'>
                      <div className='flex justify-end gap-1'>
                        {item.source === 'admin' &&
                          item.benchmark_status === 'pending' &&
                          !benchmarkActive && (
                            <Button
                              variant='ghost'
                              size='icon'
                              title={t('Edit')}
                              aria-label={t('Edit')}
                              onClick={() => {
                                setEditingId(item.id)
                                setAvailableModels([])
                                setInput({
                                  type: item.type,
                                  name: item.name,
                                  base_url: item.base_url,
                                  api_key: '',
                                  models: item.models || '',
                                  contact: item.contact,
                                  remark: item.remark,
                                })
                                setDialogOpen(true)
                              }}
                            >
                              <Pencil />
                            </Button>
                          )}
                        <Button
                          variant='ghost'
                          size='icon'
                          title={t('Benchmark history')}
                          aria-label={t('Benchmark history')}
                          onClick={() => {
                            setHistoryUpstreamId(item.id)
                            setHistoryUpstreamName(item.name)
                            setHistoryOpen(true)
                          }}
                        >
                          <History />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon'
                          disabled={
                            benchmarkActive || benchmarkMutation.isPending
                          }
                          title={t('Run benchmark')}
                          aria-label={t('Run benchmark')}
                          onClick={() => {
                            setBenchmarkUpstreamId(item.id)
                            setBenchmarkModel('')
                            setBenchmarkConcurrency(1)
                            setBenchmarkOpen(true)
                          }}
                        >
                          <Gauge />
                        </Button>
                        {item.benchmark_status === 'completed' &&
                          !item.channel_id && (
                            <>
                              {item.source === 'self' &&
                                item.application_status ===
                                  'pending_decision' && (
                                  <Button
                                    variant='ghost'
                                    size='icon'
                                    title={t('Mark as benchmark failed')}
                                    aria-label={t('Mark as benchmark failed')}
                                    onClick={() => {
                                      setRejectUpstreamId(item.id)
                                      setRejectReason('')
                                      setRejectOpen(true)
                                    }}
                                  >
                                    <CircleX />
                                  </Button>
                                )}
                              {canSync && (
                                <Button
                                  variant='ghost'
                                  size='icon'
                                  title={t('Sync to channels')}
                                  aria-label={t('Sync to channels')}
                                  disabled={syncMutation.isPending}
                                  onClick={() => {
                                    setSelectedIds([item.id])
                                    setSyncTag('')
                                    setSyncOpen(true)
                                  }}
                                >
                                  <Send />
                                </Button>
                              )}
                            </>
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
    )
  }

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          {t('Upstream Management')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            size='icon'
            onClick={() => query.refetch()}
            title={t('Refresh')}
            aria-label={t('Refresh')}
          >
            <RefreshCw />
          </Button>
          <Button
            variant='outline'
            onClick={() => {
              setProfileOpen(true)
              if (profileQuery.data?.data) {
                setProfileDraft(profileQuery.data.data)
              }
            }}
            title={t('Benchmark profile')}
          >
            <SlidersHorizontal data-icon='inline-start' />
            {t('Benchmark profile')}
          </Button>
          <Button
            variant='outline'
            onClick={() => {
              setAutoSyncOpen(true)
              if (autoSyncQuery.data?.data) {
                setAutoSyncDraft(autoSyncQuery.data.data)
              }
            }}
            title={t('Automatic sync')}
          >
            <Settings2 data-icon='inline-start' />
            {t('Automatic sync')}
          </Button>
          <Button
            variant='secondary'
            disabled={selectedIds.length === 0 || syncMutation.isPending}
            onClick={() => setSyncOpen(true)}
          >
            <Send data-icon='inline-start' />
            {t('Sync to channels')}
          </Button>
          <Button
            onClick={() => {
              setEditingId(null)
              setInput(emptyInput)
              setAvailableModels([])
              setDialogOpen(true)
            }}
          >
            <Plus data-icon='inline-start' />
            {t('Add upstream')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-3'>
            <div className='border-border/70 bg-muted/20 flex shrink-0 flex-wrap items-center gap-2 rounded-md border p-3'>
              <div className='w-full sm:w-auto'>
                <Input
                  value={keyword}
                  onChange={(event) => {
                    setKeyword(event.target.value)
                    if (!isKeywordComposing) setPage(1)
                  }}
                  onCompositionStart={() => setIsKeywordComposing(true)}
                  onCompositionEnd={(event) => {
                    setIsKeywordComposing(false)
                    setKeyword(event.currentTarget.value)
                    setPage(1)
                  }}
                  placeholder={t('Filter by name...')}
                  aria-label={t('Filter by name...')}
                  className='w-full sm:w-[200px] lg:w-[240px]'
                />
              </div>
              <div className='flex w-full sm:w-auto'>
                <Select
                  value={scoreOperator}
                  onValueChange={(value) => {
                    setScoreOperator(value as 'gte' | 'lte')
                    setPage(1)
                  }}
                >
                  <SelectTrigger className='h-8 w-44 rounded-r-none border-r-0'>
                    <SelectValue>
                      {scoreOperator === 'gte'
                        ? t('Greater than or equal')
                        : t('Less than or equal')}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='gte'>
                      {t('Greater than or equal')}
                    </SelectItem>
                    <SelectItem value='lte'>
                      {t('Less than or equal')}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <Input
                  type='number'
                  min={0}
                  max={100}
                  value={minimumScore}
                  onChange={(event) => {
                    setMinimumScore(event.target.value)
                    setPage(1)
                  }}
                  placeholder={t('Minimum score')}
                  aria-label={t('Minimum score')}
                  className='h-8 w-28 rounded-l-none sm:w-32'
                />
              </div>
              <SearchFilter
                title={t('Type')}
                options={typeFilterOptions}
                value={typeFilter}
                onValueChange={(value) => {
                  setTypeFilter(value ?? 'all')
                  setPage(1)
                }}
                searchPlaceholder={t('Search channel type...')}
                emptyText={t('No channel type found.')}
                clearText={t('Clear filters')}
                className='w-full sm:w-auto sm:max-w-64'
              />
              <SearchFilter
                title={t('Official use')}
                options={officialFilterOptions}
                value={officialFilter}
                onValueChange={(value) => {
                  setOfficialFilter(value ?? 'all')
                }}
                searchPlaceholder={t('Search official use...')}
                emptyText={t('No option found.')}
                clearText={t('Clear filters')}
                className='w-full sm:w-auto'
              />
              <SearchFilter
                title={t('Status')}
                options={benchmarkStatusOptions}
                value={status}
                onValueChange={(value) => {
                  setStatus(value ?? 'all')
                  setPage(1)
                }}
                searchPlaceholder={t('Search status...')}
                emptyText={t('No status found.')}
                clearText={t('Clear filters')}
                className='w-full sm:w-auto'
              />
            </div>
            <div className='border-border/70 bg-muted/10 flex min-h-11 shrink-0 flex-wrap items-center gap-x-6 gap-y-2 border-y px-3 py-2'>
              <div className='flex items-center gap-2 text-sm'>
                <span className='bg-warning/10 text-warning flex size-7 items-center justify-center rounded-md'>
                  <Clock3 className='size-4' aria-hidden='true' />
                </span>
                <span className='text-muted-foreground'>
                  {t('Benchmark status pending')}
                </span>
                <span className='font-semibold tabular-nums'>
                  {pendingCount}
                </span>
              </div>
              <div className='flex items-center gap-2 text-sm'>
                <span className='bg-secondary text-secondary-foreground flex size-7 items-center justify-center rounded-md'>
                  <CircleCheck className='size-4' aria-hidden='true' />
                </span>
                <span className='text-muted-foreground'>
                  {t('Official use')}
                </span>
                <span className='font-semibold tabular-nums'>
                  {officialCount}
                </span>
              </div>
            </div>
            {tableContent}
            <div className='border-border/70 bg-muted/20 flex shrink-0 flex-col gap-3 rounded-md border px-3 py-2 sm:flex-row sm:items-center sm:justify-between'>
              <div className='flex items-center gap-1.5 text-sm whitespace-nowrap'>
                <span className='text-muted-foreground'>{t('Total:')}</span>
                <span className='font-medium tabular-nums'>{total}</span>
              </div>
              <div className='flex min-w-0 items-center gap-3 overflow-x-auto'>
                <div className='flex shrink-0 items-center gap-2'>
                  <span className='text-muted-foreground hidden text-sm whitespace-nowrap md:inline'>
                    {t('Rows per page')}
                  </span>
                  <Select
                    value={String(pageSize)}
                    onValueChange={(value) => {
                      setPageSize(Number(value))
                      setPage(1)
                    }}
                  >
                    <SelectTrigger className='w-[72px]'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      {pageSizeOptions.map((size) => (
                        <SelectItem key={size} value={String(size)}>
                          {size}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='flex shrink-0 items-center gap-1'>
                  <Button
                    variant='outline'
                    size='icon'
                    className='size-8'
                    disabled={page <= 1}
                    onClick={() => setPage(1)}
                    aria-label={t('Go to first page')}
                  >
                    <ChevronsLeft />
                  </Button>
                  <Button
                    variant='outline'
                    size='icon'
                    className='size-8'
                    disabled={page <= 1}
                    onClick={() => setPage((value) => Math.max(1, value - 1))}
                    aria-label={t('Go to previous page')}
                  >
                    <ChevronLeft />
                  </Button>
                  {pageNumbers.map((pageNumber, index) =>
                    typeof pageNumber === 'string' ? (
                      <span
                        key={`ellipsis-${pageNumbers[index - 1]}-${pageNumbers[index + 1]}`}
                        className='text-muted-foreground flex size-8 items-center justify-center'
                      >
                        ...
                      </span>
                    ) : (
                      <Button
                        key={pageNumber}
                        variant={page === pageNumber ? 'default' : 'outline'}
                        size='icon'
                        className='size-8 tabular-nums'
                        onClick={() => setPage(Number(pageNumber))}
                        aria-label={t('Go to page {{page}}', {
                          page: pageNumber,
                        })}
                        aria-current={page === pageNumber ? 'page' : undefined}
                      >
                        {pageNumber}
                      </Button>
                    )
                  )}
                  <Button
                    variant='outline'
                    size='icon'
                    className='size-8'
                    disabled={page >= totalPages}
                    onClick={() =>
                      setPage((value) => Math.min(totalPages, value + 1))
                    }
                    aria-label={t('Go to next page')}
                  >
                    <ChevronRight />
                  </Button>
                  <Button
                    variant='outline'
                    size='icon'
                    className='size-8'
                    disabled={page >= totalPages}
                    onClick={() => setPage(totalPages)}
                    aria-label={t('Go to last page')}
                  >
                    <ChevronsRight />
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className='flex max-h-[90vh] flex-col overflow-hidden sm:max-w-2xl'>
          <DialogHeader className='shrink-0'>
            <DialogTitle>{t(editingId ? 'Edit' : 'Add upstream')}</DialogTitle>
            <DialogDescription>
              {t('Create an upstream candidate for benchmark review.')}
            </DialogDescription>
          </DialogHeader>
          <div className='min-h-0 flex-1 overflow-y-auto overscroll-contain pr-1'>
            <div className='grid gap-4 sm:grid-cols-2'>
              <div className='grid gap-2'>
                <Label htmlFor='upstream-type'>{t('Type *')}</Label>
                <div className='relative'>
                  <span className='pointer-events-none absolute top-1/2 left-3 z-10 flex -translate-y-1/2'>
                    {getLobeIcon(`${getChannelTypeIcon(input.type)}.Color`, 18)}
                  </span>
                  <Combobox
                    id='upstream-type'
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
                <Label htmlFor='upstream-name'>{t('Name *')}</Label>
                <Input
                  id='upstream-name'
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
                <Label htmlFor='upstream-contact'>{t('Contact')}</Label>
                <Input
                  id='upstream-contact'
                  value={input.contact}
                  onChange={(event) =>
                    setInput((currentInput) => ({
                      ...currentInput,
                      contact: event.target.value,
                    }))
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
                    <Label htmlFor='upstream-url'>{t('API Base URL *')}</Label>
                    <Input
                      id='upstream-url'
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
                    <Label htmlFor='upstream-key'>{t('API Key *')}</Label>
                    <Textarea
                      id='upstream-key'
                      rows={3}
                      value={input.api_key}
                      onChange={(event) =>
                        setInput((currentInput) => ({
                          ...currentInput,
                          api_key: event.target.value,
                        }))
                      }
                      placeholder={t(
                        editingId
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
                    id='upstream-models'
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
                <Label htmlFor='upstream-remark'>{t('Remark')}</Label>
                <Textarea
                  id='upstream-remark'
                  value={input.remark}
                  onChange={(event) =>
                    setInput((currentInput) => ({
                      ...currentInput,
                      remark: event.target.value,
                    }))
                  }
                />
              </div>
            </div>
          </div>
          <DialogFooter className='shrink-0'>
            <Button variant='outline' onClick={() => setDialogOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={
                !input.name ||
                !input.base_url ||
                !input.models.trim() ||
                (!editingId && !input.api_key) ||
                createMutation.isPending
              }
              onClick={() => createMutation.mutate(input)}
            >
              {createMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {t(editingId ? 'Save' : 'Create')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={profileOpen} onOpenChange={setProfileOpen}>
        <DialogContent className='flex max-h-[90vh] flex-col overflow-hidden sm:max-w-4xl'>
          <DialogHeader className='shrink-0'>
            <DialogTitle>{t('Benchmark profile')}</DialogTitle>
            <DialogDescription>
              {t(
                'Configure admission and full-score thresholds for benchmark metrics.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='min-h-0 flex-1 overflow-auto overscroll-contain pr-1'>
            {!hasProfileData ? (
              <div className='text-muted-foreground flex min-h-48 items-center justify-center rounded-md border text-sm'>
                {profileQuery.isLoading ? (
                  <span className='animate-spin'>
                    <LoaderCircle aria-hidden='true' />
                  </span>
                ) : (
                  t('No data')
                )}
              </div>
            ) : (
              <div className='overflow-hidden rounded-md border'>
                <Table className='min-w-[640px]'>
                  <TableHeader className='bg-muted/60'>
                    <TableRow>
                      <TableHead className='w-[28%] text-center'>
                        {t('Metric')}
                      </TableHead>
                      <TableHead className='text-center'>
                        {t('Admission threshold')}
                      </TableHead>
                      <TableHead className='text-center'>
                        {t('Full-score threshold')}
                      </TableHead>
                      <TableHead className='text-center'>
                        {t('Weight')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {Object.entries(profile?.metrics ?? {})
                      .sort(
                        ([a], [b]) =>
                          benchmarkMetricOrder.indexOf(a) -
                          benchmarkMetricOrder.indexOf(b)
                      )
                      .map(([name, metric]) => (
                        <TableRow key={name}>
                          <TableCell className='text-center font-medium'>
                            {name}
                          </TableCell>
                          <TableCell>
                            <div className='mx-auto flex max-w-44 items-center justify-center gap-1'>
                              {editingProfileCell?.name === name &&
                              editingProfileCell.field === 'admission' ? (
                                <Input
                                  autoFocus
                                  type='number'
                                  step='any'
                                  min={0}
                                  value={metric.admission || ''}
                                  onChange={(event) => {
                                    const value = Number(event.target.value)
                                    if (!profile) return
                                    setProfileDraft({
                                      ...profile,
                                      metrics: {
                                        ...profile.metrics,
                                        [name]: { ...metric, admission: value },
                                      },
                                    })
                                  }}
                                  aria-label={`${name} ${t('Admission threshold')}`}
                                  className='h-8 text-center'
                                />
                              ) : (
                                <span className='min-w-20 text-center text-sm'>
                                  {metric.admission}
                                </span>
                              )}
                              <Button
                                type='button'
                                variant='ghost'
                                size='icon'
                                className='size-7 shrink-0'
                                title={
                                  editingProfileCell?.name === name &&
                                  editingProfileCell.field === 'admission'
                                    ? t('Done')
                                    : t('Edit')
                                }
                                aria-label={`${name} ${
                                  editingProfileCell?.name === name &&
                                  editingProfileCell.field === 'admission'
                                    ? t('Done')
                                    : t('Edit')
                                }`}
                                onClick={() =>
                                  setEditingProfileCell(
                                    editingProfileCell?.name === name &&
                                      editingProfileCell.field === 'admission'
                                      ? null
                                      : { name, field: 'admission' }
                                  )
                                }
                              >
                                {editingProfileCell?.name === name &&
                                editingProfileCell.field === 'admission' ? (
                                  <Check />
                                ) : (
                                  <Pencil />
                                )}
                              </Button>
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className='mx-auto flex max-w-44 items-center justify-center gap-1'>
                              {editingProfileCell?.name === name &&
                              editingProfileCell.field === 'full' ? (
                                <Input
                                  autoFocus
                                  type='number'
                                  step='any'
                                  min={0}
                                  value={metric.full || ''}
                                  onChange={(event) => {
                                    const value = Number(event.target.value)
                                    if (!profile) return
                                    setProfileDraft({
                                      ...profile,
                                      metrics: {
                                        ...profile.metrics,
                                        [name]: { ...metric, full: value },
                                      },
                                    })
                                  }}
                                  aria-label={`${name} ${t('Full-score threshold')}`}
                                  className='h-8 text-center'
                                />
                              ) : (
                                <span className='min-w-20 text-center text-sm'>
                                  {metric.full}
                                </span>
                              )}
                              <Button
                                type='button'
                                variant='ghost'
                                size='icon'
                                className='size-7 shrink-0'
                                title={
                                  editingProfileCell?.name === name &&
                                  editingProfileCell.field === 'full'
                                    ? t('Done')
                                    : t('Edit')
                                }
                                aria-label={`${name} ${
                                  editingProfileCell?.name === name &&
                                  editingProfileCell.field === 'full'
                                    ? t('Done')
                                    : t('Edit')
                                }`}
                                onClick={() =>
                                  setEditingProfileCell(
                                    editingProfileCell?.name === name &&
                                      editingProfileCell.field === 'full'
                                      ? null
                                      : { name, field: 'full' }
                                  )
                                }
                              >
                                {editingProfileCell?.name === name &&
                                editingProfileCell.field === 'full' ? (
                                  <Check />
                                ) : (
                                  <Pencil />
                                )}
                              </Button>
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className='mx-auto flex max-w-44 items-center justify-center gap-1'>
                              {editingProfileCell?.name === name &&
                              editingProfileCell.field === 'weight' ? (
                                <Input
                                  autoFocus
                                  type='number'
                                  step='any'
                                  min={0}
                                  value={metric.weight || ''}
                                  onChange={(event) => {
                                    const value = Number(event.target.value)
                                    if (!profile) return
                                    setProfileDraft({
                                      ...profile,
                                      metrics: {
                                        ...profile.metrics,
                                        [name]: { ...metric, weight: value },
                                      },
                                    })
                                  }}
                                  aria-label={`${name} ${t('Weight')}`}
                                  className='h-8 text-center'
                                />
                              ) : (
                                <span className='min-w-20 text-center text-sm'>
                                  {metric.weight}
                                </span>
                              )}
                              <Button
                                type='button'
                                variant='ghost'
                                size='icon'
                                className='size-7 shrink-0'
                                title={
                                  editingProfileCell?.name === name &&
                                  editingProfileCell.field === 'weight'
                                    ? t('Done')
                                    : t('Edit')
                                }
                                aria-label={`${name} ${
                                  editingProfileCell?.name === name &&
                                  editingProfileCell.field === 'weight'
                                    ? t('Done')
                                    : t('Edit')
                                }`}
                                onClick={() =>
                                  setEditingProfileCell(
                                    editingProfileCell?.name === name &&
                                      editingProfileCell.field === 'weight'
                                      ? null
                                      : { name, field: 'weight' }
                                  )
                                }
                              >
                                {editingProfileCell?.name === name &&
                                editingProfileCell.field === 'weight' ? (
                                  <Check />
                                ) : (
                                  <Pencil />
                                )}
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
          <DialogFooter className='shrink-0'>
            <Button variant='outline' onClick={() => setProfileOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={
                profileMutation.isPending ||
                !hasProfileData ||
                !profile ||
                !profileChanged
              }
              onClick={() => {
                if (profile) profileMutation.mutate(profile)
              }}
            >
              {profileMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={autoSyncOpen} onOpenChange={setAutoSyncOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('Automatic sync')}</DialogTitle>
            <DialogDescription>
              {t(
                'Automatically synchronize completed upstreams that meet the score threshold.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-4'>
            <label className='flex items-center gap-2 text-sm'>
              <Checkbox
                checked={autoSyncDraft.enabled}
                onCheckedChange={(checked) =>
                  setAutoSyncDraft({
                    ...autoSyncDraft,
                    enabled: checked === true,
                  })
                }
              />
              {t('Enable automatic sync')}
            </label>
            <div className='grid gap-2'>
              <Label htmlFor='auto-sync-tag'>{t('Channel tag')}</Label>
              <Input
                id='auto-sync-tag'
                value={autoSyncDraft.channel_tag}
                onChange={(event) =>
                  setAutoSyncDraft({
                    ...autoSyncDraft,
                    channel_tag: event.target.value,
                  })
                }
              />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='auto-sync-score'>{t('Minimum score')}</Label>
              <Input
                id='auto-sync-score'
                type='number'
                min={0}
                max={100}
                value={autoSyncDraft.min_score}
                onChange={(event) =>
                  setAutoSyncDraft({
                    ...autoSyncDraft,
                    min_score: Number(event.target.value),
                  })
                }
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setAutoSyncOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={autoSyncMutation.isPending}
              onClick={() => autoSyncMutation.mutate(autoSyncDraft)}
            >
              {autoSyncMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={benchmarkOpen} onOpenChange={setBenchmarkOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Run benchmark')}</DialogTitle>
            <DialogDescription>
              {t(
                'Select a model returned by the upstream before starting the benchmark.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-4'>
            <div className='grid gap-2'>
              <Label>{t('Model to use for testing')}</Label>
              <Select
                value={benchmarkModel}
                onValueChange={(value) => setBenchmarkModel(value ?? '')}
              >
                <SelectTrigger>
                  <SelectValue
                    placeholder={
                      modelsQuery.isLoading ? t('Loading') : t('Select Model')
                    }
                  />
                </SelectTrigger>
                <SelectContent>
                  {(modelsQuery.data?.data?.models ?? []).map((model) => (
                    <SelectItem key={model} value={model}>
                      {model}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='benchmark-concurrency'>
                {t('Parallel requests')}
              </Label>
              <Input
                id='benchmark-concurrency'
                type='number'
                min={1}
                max={100}
                value={benchmarkConcurrency}
                onChange={(event) =>
                  setBenchmarkConcurrency(Number(event.target.value))
                }
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setBenchmarkOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={!benchmarkModel || benchmarkMutation.isPending}
              onClick={() => {
                if (benchmarkUpstreamId !== null) {
                  benchmarkMutation.mutate({
                    id: benchmarkUpstreamId,
                    model: benchmarkModel,
                    concurrency: benchmarkConcurrency,
                  })
                }
              }}
            >
              {benchmarkMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {t('Run benchmark')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={syncOpen} onOpenChange={setSyncOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Sync to channels')}</DialogTitle>
            <DialogDescription>
              {t('Add a new channel by providing the necessary information.')}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-2'>
            <Label htmlFor='sync-tag'>{t('Channel tag')}</Label>
            <Input
              id='sync-tag'
              value={syncTag}
              onChange={(event) => setSyncTag(event.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setSyncOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={selectedIds.length === 0 || syncMutation.isPending}
              onClick={() =>
                syncMutation.mutate({ ids: selectedIds, tag: syncTag })
              }
            >
              {syncMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {t('Sync to channels')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={rejectOpen} onOpenChange={setRejectOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Mark as benchmark failed')}</DialogTitle>
            <DialogDescription>
              {t('Enter the reason this benchmark did not pass.')}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-2'>
            <Label htmlFor='benchmark-reject-reason'>
              {t('Benchmark rejection reason')}
            </Label>
            <Textarea
              id='benchmark-reject-reason'
              value={rejectReason}
              onChange={(event) => setRejectReason(event.target.value)}
              placeholder={t('Benchmark rejection reason')}
              maxLength={1024}
            />
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setRejectOpen(false)}
              disabled={rejectMutation.isPending}
            >
              {t('Cancel')}
            </Button>
            <Button
              variant='destructive'
              disabled={!rejectReason.trim() || rejectMutation.isPending}
              onClick={() => {
                if (rejectUpstreamId !== null) {
                  rejectMutation.mutate({
                    id: rejectUpstreamId,
                    reason: rejectReason.trim(),
                  })
                }
              }}
            >
              {rejectMutation.isPending && (
                <LoaderCircle className='animate-spin' />
              )}
              {t('Mark as benchmark failed')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <BenchmarkHistoryDialog
        open={historyOpen}
        upstreamId={historyUpstreamId}
        upstreamName={historyUpstreamName}
        onOpenChange={setHistoryOpen}
      />
    </>
  )
}
