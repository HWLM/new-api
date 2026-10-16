/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import type { CreditSummary } from './types'

// 骨架卡片用固定 key，避免用数组下标做 key
const SKELETON_CARD_KEYS = ['c1', 'c2', 'c3', 'c4', 'c5', 'c6']

function formatAmount(n: number): string {
  return n.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

type CardItem = {
  label: string
  value: number
  // 副标题：用于标注该卡片不随上方时间筛选变化
  subtitle?: string
  // 「查看明细」入口：回款与未回款两张卡暂不提供
  onViewDetails?: () => void
}

function CardCell({ item, detailsLabel }: { item: CardItem; detailsLabel: string }) {
  return (
    <Card className='gap-1 py-3 sm:py-4'>
      <CardContent className='px-4 sm:px-5'>
        <div className='flex items-start justify-between gap-2'>
          <div className='text-muted-foreground text-sm'>{item.label}</div>
          {item.onViewDetails && (
            <Button
              variant='ghost'
              size='sm'
              className='text-muted-foreground h-auto px-1 py-0 text-xs'
              onClick={item.onViewDetails}
            >
              {detailsLabel}
            </Button>
          )}
        </div>
        <div className='mt-1 text-2xl font-semibold tabular-nums'>
          {formatAmount(item.value)}
        </div>
        {item.subtitle && (
          <div className='text-muted-foreground mt-1 text-xs'>
            {item.subtitle}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

export function SummaryCards({
  data,
  isLoading,
  onViewDetails,
}: {
  data: CreditSummary | undefined
  isLoading: boolean
  // 点卡片上的「查看明细」时回传该卡片对应的金额维度，由父组件决定如何展开
  onViewDetails?: (
    dimension: 'recharge' | 'gift' | 'credit' | 'repayment'
  ) => void
}) {
  const { t } = useTranslation()

  if (isLoading || !data) {
    return (
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'>
        {SKELETON_CARD_KEYS.map((key) => (
          <Card key={key} className='gap-1 py-3 sm:py-4'>
            <CardContent className='px-4 sm:px-5'>
              <Skeleton className='h-4 w-24' />
              <Skeleton className='mt-2 h-7 w-28' />
            </CardContent>
          </Card>
        ))}
      </div>
    )
  }

  const items: CardItem[] = [
    {
      label: t('Recharge Amount (¥)'),
      value: data.recharge_amount,
      onViewDetails: onViewDetails ? () => onViewDetails('recharge') : undefined,
    },
    {
      label: t('Gift Amount (¥)'),
      value: data.gift_amount,
      onViewDetails: onViewDetails ? () => onViewDetails('gift') : undefined,
    },
    {
      label: t('Credit Amount (¥)'),
      value: data.credit_amount,
      onViewDetails: onViewDetails ? () => onViewDetails('credit') : undefined,
    },
    {
      label: t('Repayment Amount (¥)'),
      value: data.repayment_amount,
      onViewDetails: onViewDetails
        ? () => onViewDetails('repayment')
        : undefined,
    },
    {
      // 全时间总额口径，与上方时间筛选无关，单独标注避免误读。
      // 不带币种后缀：消耗按 quota/QuotaPerUnit 折算，与「新用户统计」的总消耗额同口径。
      label: t('Receivable'),
      value: data.receivable,
      subtitle: t('Total consumption of credit customers - total repayment (all time)'),
    },
    {
      // 全时间总额口径，与上方时间筛选无关，单独标注避免误读
      label: t('Outstanding Credit (¥)'),
      value: data.outstanding_credit,
      subtitle: t('Total credit - total repayment (all time)'),
    },
  ]

  return (
    <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6'>
      {items.map((item) => (
        <CardCell key={item.label} item={item} detailsLabel={t('View Details')} />
      ))}
    </div>
  )
}
