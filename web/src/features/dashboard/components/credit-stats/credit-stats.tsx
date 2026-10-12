/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { fetchUserStatsFilterOptions } from '../new-user-stats/api'
import { AmountDetailsDialog } from './amount-details-dialog'
import {
  fetchCreditDetailsMonthly,
  fetchCreditDetailsSummary,
  fetchCreditSummary,
  fetchCreditTrend,
} from './api'
import { BackfillDialog } from './backfill-dialog'
import { rangeToDates, type CreditDateRange } from './date-range'
import {
  DetailsFilterBar,
  type CreditDetailsFilterValue,
} from './details-filter-bar'
import { DetailsMonthlyTable } from './details-monthly-table'
import { DetailsSummaryTable } from './details-summary-table'
import { FilterBar } from './filter-bar'
import { RecordsDialog, type RecordsTarget } from './records-dialog'
import { RepaymentDialog, type RepaymentTarget } from './repayment-dialog'
import { SummaryCards } from './summary-cards'
import { TrendChart } from './trend-chart'
import type { CreditTrendDimension, CreditTrendGranularity } from './types'

const PAGE_SIZE = 10

// 明细标签的默认月份区间：当月
function currentMonth(): string {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

function buildDefaultDetailsFilter(): CreditDetailsFilterValue {
  const month = currentMonth()
  return {
    username: '',
    channel: [],
    sales: [],
    startMonth: month,
    endMonth: month,
  }
}

export function CreditStats() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  // 卡片明细弹窗的标题用这组维度名，与趋势图下拉的口径保持一致
  const amountDimensionLabels: Record<CreditTrendDimension, string> = {
    recharge: t('Recharge'),
    gift: t('Gift'),
    credit: t('Credit'),
    repayment: t('Repayment'),
  }

  const [dateRange, setDateRange] = useState<CreditDateRange>(() =>
    rangeToDates('7d')
  )
  const [dimension, setDimension] = useState<CreditTrendDimension>('recharge')
  const [granularity, setGranularity] = useState<CreditTrendGranularity>('day')

  const [detailsTab, setDetailsTab] = useState<'summary' | 'monthly'>('summary')
  const [detailsFilter, setDetailsFilter] = useState<CreditDetailsFilterValue>(
    buildDefaultDetailsFilter
  )
  const [summaryPage, setSummaryPage] = useState(1)
  const [monthlyPage, setMonthlyPage] = useState(1)

  const [repaymentTarget, setRepaymentTarget] = useState<RepaymentTarget | null>(
    null
  )
  const [recordsTarget, setRecordsTarget] = useState<RecordsTarget | null>(null)
  // 卡片「查看明细」弹窗：非 null 时展开对应维度
  const [amountDetailsDimension, setAmountDetailsDimension] = useState<
    CreditTrendDimension | null
  >(null)
  // 历史数据补录弹窗
  const [backfillOpen, setBackfillOpen] = useState(false)

  const { data: filterOptions } = useQuery({
    queryKey: ['user-stats-filter-options'],
    queryFn: fetchUserStatsFilterOptions,
  })

  const summaryQuery = useQuery({
    queryKey: ['credit-summary', dateRange.start_date, dateRange.end_date],
    queryFn: () =>
      fetchCreditSummary(dateRange.start_date, dateRange.end_date),
  })

  const trendQuery = useQuery({
    queryKey: [
      'credit-trend',
      dateRange.start_date,
      dateRange.end_date,
      granularity,
      dimension,
    ],
    queryFn: () =>
      fetchCreditTrend(
        dateRange.start_date,
        dateRange.end_date,
        granularity,
        dimension
      ),
  })

  const detailsSummaryQuery = useQuery({
    queryKey: [
      'credit-details-summary',
      detailsFilter.username,
      detailsFilter.channel.join(','),
      detailsFilter.sales.join(','),
      summaryPage,
    ],
    queryFn: () =>
      fetchCreditDetailsSummary({
        username: detailsFilter.username,
        channel: detailsFilter.channel,
        sales: detailsFilter.sales,
        page: summaryPage,
        page_size: PAGE_SIZE,
      }),
    enabled: detailsTab === 'summary',
  })

  const detailsMonthlyQuery = useQuery({
    queryKey: [
      'credit-details-monthly',
      detailsFilter.startMonth,
      detailsFilter.endMonth,
      detailsFilter.username,
      detailsFilter.channel.join(','),
      detailsFilter.sales.join(','),
      monthlyPage,
    ],
    queryFn: () =>
      fetchCreditDetailsMonthly({
        start_month: detailsFilter.startMonth,
        end_month: detailsFilter.endMonth,
        username: detailsFilter.username,
        channel: detailsFilter.channel,
        sales: detailsFilter.sales,
        page: monthlyPage,
        page_size: PAGE_SIZE,
      }),
    enabled:
      detailsTab === 'monthly' &&
      detailsFilter.startMonth !== '' &&
      detailsFilter.endMonth !== '',
  })

  // 回款成功后，顶部卡片、趋势图与两张明细表的数据都变了，一并失效重拉
  const refreshAfterRepayment = () => {
    queryClient.invalidateQueries({ queryKey: ['credit-summary'] })
    queryClient.invalidateQueries({ queryKey: ['credit-trend'] })
    queryClient.invalidateQueries({ queryKey: ['credit-details-summary'] })
    queryClient.invalidateQueries({ queryKey: ['credit-details-monthly'] })
    queryClient.invalidateQueries({ queryKey: ['credit-records'] })
    queryClient.invalidateQueries({ queryKey: ['amount-details'] })
  }

  const applyDetailsFilter = (next: CreditDetailsFilterValue) => {
    setDetailsFilter(next)
    setSummaryPage(1)
    setMonthlyPage(1)
  }

  return (
    <div className='space-y-4'>
      <FilterBar value={dateRange} onChange={setDateRange} />

      <SummaryCards
        data={summaryQuery.data}
        isLoading={summaryQuery.isLoading}
        onViewDetails={setAmountDetailsDimension}
      />

      <TrendChart
        data={trendQuery.data}
        isLoading={trendQuery.isLoading}
        dimension={dimension}
        granularity={granularity}
        onDimensionChange={setDimension}
        onGranularityChange={setGranularity}
      />

      <Card>
        <CardHeader className='flex flex-row items-center justify-between space-y-0'>
          <CardTitle className='text-base'>{t('Credit')}</CardTitle>
          <div className='flex items-center gap-1'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => setBackfillOpen(true)}
            >
              {t('Add Historical Record')}
            </Button>
            <div className='bg-border mx-1 h-5 w-px' />
            <Button
              variant={detailsTab === 'summary' ? 'default' : 'outline'}
              size='sm'
              onClick={() => setDetailsTab('summary')}
            >
              {t('Summary')}
            </Button>
            <Button
              variant={detailsTab === 'monthly' ? 'default' : 'outline'}
              size='sm'
              onClick={() => setDetailsTab('monthly')}
            >
              {t('Details')}
            </Button>
          </div>
        </CardHeader>
        <CardContent className='space-y-4'>
          <DetailsFilterBar
            mode={detailsTab}
            value={detailsFilter}
            options={filterOptions}
            onApply={applyDetailsFilter}
          />

          {detailsTab === 'summary' ? (
            <DetailsSummaryTable
              data={detailsSummaryQuery.data}
              isLoading={detailsSummaryQuery.isLoading}
              page={summaryPage}
              pageSize={PAGE_SIZE}
              onPageChange={setSummaryPage}
              onRepay={(row) =>
                setRepaymentTarget({
                  user_id: row.user_id,
                  username: row.username,
                  display_name: row.display_name,
                  outstanding: row.outstanding,
                })
              }
              onViewRecords={(row) =>
                setRecordsTarget({
                  user_id: row.user_id,
                  username: row.username,
                })
              }
            />
          ) : (
            <DetailsMonthlyTable
              data={detailsMonthlyQuery.data}
              isLoading={detailsMonthlyQuery.isLoading}
              page={monthlyPage}
              pageSize={PAGE_SIZE}
              onPageChange={setMonthlyPage}
              onRepay={(row) =>
                setRepaymentTarget({
                  user_id: row.user_id,
                  username: row.username,
                  display_name: row.display_name,
                  outstanding: row.ending_outstanding,
                })
              }
              onViewRecords={(row) =>
                setRecordsTarget({
                  user_id: row.user_id,
                  username: row.username,
                })
              }
            />
          )}
        </CardContent>
      </Card>

      <RepaymentDialog
        target={repaymentTarget}
        onOpenChange={(open) => {
          if (!open) setRepaymentTarget(null)
        }}
        onSuccess={refreshAfterRepayment}
      />

      <RecordsDialog
        target={recordsTarget}
        onOpenChange={(open) => {
          if (!open) setRecordsTarget(null)
        }}
      />

      {amountDetailsDimension && (
        <AmountDetailsDialog
          open
          onOpenChange={(open) => {
            if (!open) setAmountDetailsDimension(null)
          }}
          dimension={amountDetailsDimension}
          dimensionLabel={amountDimensionLabels[amountDetailsDimension]}
          startDate={dateRange.start_date}
          endDate={dateRange.end_date}
        />
      )}

      <BackfillDialog
        open={backfillOpen}
        onOpenChange={setBackfillOpen}
        onSuccess={refreshAfterRepayment}
      />
    </div>
  )
}
