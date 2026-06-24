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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useStatus } from '@/hooks/use-status'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
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
import { adjustUserQuota } from '../api'
import type {
  ManageUserQuotaPayload,
  QuotaAdjustMode,
  QuotaType,
} from '../types'

const RATIO_MIN = 0.1
const RATIO_MAX = 100
const QUOTA_PER_USD = 500_000 // 与后端 common.QuotaPerUnit 对齐，仅用于预览展示

interface UserQuotaDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  currentQuota: number
  /** 用户所在分组的充值比例，来自 GET /api/user/:id 的 topup_group_ratio */
  topupGroupRatio?: number
  onSuccess: () => void
}

export function UserQuotaDialog(props: UserQuotaDialogProps) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [mode, setMode] = useState<QuotaAdjustMode>('add')
  const [amount, setAmount] = useState('') // subtract / override 用
  const [quotaType, setQuotaType] = useState<QuotaType>('充值')
  const [rechargeAmount, setRechargeAmount] = useState('') // add 模式：USD
  const [ratio, setRatio] = useState('1')
  const [loading, setLoading] = useState(false)

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  // 支付网关「价格（本地货币/美元）」，与后端 operation_setting.Price 对齐。
  // 充值类型需要再除以这个价格；赠送类型不受影响。
  const localPrice = Number(status?.price) || 0

  // 弹窗打开 / 切换用户 时，将比例回显为分组充值比例
  useEffect(() => {
    if (props.open) {
      setRatio(String(props.topupGroupRatio ?? 1))
    }
  }, [props.open, props.topupGroupRatio])

  const isGift = quotaType === '赠送'
  const rechargeNum = parseFloat(rechargeAmount) || 0
  const ratioNum = parseFloat(ratio) || 0
  // 赠送：1:1 直接按 USD 计入，不应用比例与价格；
  // 充值：金额 ÷ 比例 ÷ 价格
  const actualUsd = isGift
    ? rechargeNum
    : ratioNum > 0 && localPrice > 0
      ? rechargeNum / ratioNum / localPrice
      : 0
  // 预览用：把 USD → quota 单位 → 配置的展示单位
  const actualQuotaUnits = Math.round(actualUsd * QUOTA_PER_USD)

  const amountValue = parseFloat(amount) || 0
  const subtractQuotaValue = parseQuotaFromDollars(Math.abs(amountValue))
  const overrideQuotaValue = parseQuotaFromDollars(amountValue)

  const getPreviewText = () => {
    const current = props.currentQuota
    switch (mode) {
      case 'add':
        return `${t('Current quota')}: ${formatQuota(current)}  +${formatQuota(actualQuotaUnits)} = ${formatQuota(current + actualQuotaUnits)}`
      case 'subtract':
        return `${t('Current quota')}: ${formatQuota(current)}  -${formatQuota(subtractQuotaValue)} = ${formatQuota(current - subtractQuotaValue)}`
      case 'override':
        return `${t('Current quota')}: ${formatQuota(current)} → ${formatQuota(overrideQuotaValue)}`
      default:
        return ''
    }
  }

  const resetForm = () => {
    setAmount('')
    setRechargeAmount('')
    setQuotaType('充值')
    setRatio(String(props.topupGroupRatio ?? 1))
    setMode('add')
  }

  const handleConfirm = async () => {
    setLoading(true)
    try {
      let payload: ManageUserQuotaPayload
      if (mode === 'add') {
        if (rechargeNum <= 0) {
          toast.error(t('Please enter recharge amount'))
          return
        }
        if (!isGift && (ratioNum < RATIO_MIN || ratioNum > RATIO_MAX)) {
          toast.error(
            t('Ratio must be between {{min}} and {{max}}', {
              min: RATIO_MIN,
              max: RATIO_MAX,
            })
          )
          return
        }
        if (!isGift && localPrice <= 0) {
          toast.error(
            t(
              'Payment gateway price (local currency / USD) is not configured. Please set it in System Settings → Billing & Payment → Payment Gateway.'
            )
          )
          return
        }
        payload = {
          id: props.userId,
          action: 'add_quota',
          mode: 'add',
          quota_type: quotaType,
          recharge_amount: rechargeNum,
          ratio: isGift ? 1 : ratioNum,
        }
      } else {
        if (!amount) return
        const value =
          mode === 'override' ? overrideQuotaValue : subtractQuotaValue
        if (mode !== 'override' && value <= 0) return
        payload = {
          id: props.userId,
          action: 'add_quota',
          mode,
          value,
        }
      }

      const result = await adjustUserQuota(payload)
      if (result.success) {
        toast.success(t('Quota adjusted successfully'))
        resetForm()
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        toast.error(result.message || t('Failed to adjust quota'))
      }
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : t('Failed to adjust quota'))
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = () => {
    resetForm()
    props.onOpenChange(false)
  }

  const amountPlaceholder = tokensOnly
    ? t('Enter amount in tokens')
    : t('Enter amount in {{currency}}', { currency: currencyLabel })

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('Adjust Quota')}</DialogTitle>
          <DialogDescription>
            {t('Select an operation mode and enter the amount')}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4'>
          <div className='text-muted-foreground text-sm'>
            {getPreviewText()}
          </div>

          <div className='space-y-2'>
            <Label>{t('Mode')}</Label>
            <div className='flex gap-1'>
              {(['add', 'subtract', 'override'] as const).map((m) => (
                <Button
                  key={m}
                  type='button'
                  variant='outline'
                  size='sm'
                  className={cn(
                    mode === m &&
                      'bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground'
                  )}
                  onClick={() => {
                    setMode(m)
                    setAmount('')
                    setRechargeAmount('')
                  }}
                >
                  {m === 'add'
                    ? t('Add')
                    : m === 'subtract'
                      ? t('Subtract')
                      : t('Override')}
                </Button>
              ))}
            </div>
          </div>

          {mode === 'add' ? (
            <>
              <div className='space-y-2'>
                <Label>{t('Type')}</Label>
                <div className='flex gap-1'>
                  {(['充值', '赠送'] as const).map((q) => (
                    <Button
                      key={q}
                      type='button'
                      variant='outline'
                      size='sm'
                      className={cn(
                        quotaType === q &&
                          'bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground'
                      )}
                      onClick={() => setQuotaType(q)}
                    >
                      {q === '充值' ? t('Recharge') : t('Gift')}
                    </Button>
                  ))}
                </div>
              </div>

              <div className='space-y-2'>
                <Label>{t('Recharge Amount (USD)')}</Label>
                <Input
                  type='number'
                  step={0.01}
                  min={0}
                  placeholder={t('Enter recharge amount in USD')}
                  value={rechargeAmount}
                  onChange={(e) => setRechargeAmount(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleConfirm()
                  }}
                />
              </div>

              {!isGift && (
                <div className='space-y-2'>
                  <Label>{t('Recharge ratio')}</Label>
                  <Input
                    type='number'
                    step={0.01}
                    min={RATIO_MIN}
                    max={RATIO_MAX}
                    placeholder={t('Default from user group ratio')}
                    value={ratio}
                    onChange={(e) => setRatio(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handleConfirm()
                    }}
                  />
                </div>
              )}

              <div className='text-muted-foreground text-sm'>
                {isGift ? (
                  <>
                    {t('Actual credit')} = {t('Recharge Amount')} ={' '}
                    <span className='text-foreground font-medium'>
                      {actualUsd.toFixed(4)} USD
                    </span>
                  </>
                ) : (
                  <>
                    {t('Actual credit')} = {t('Recharge Amount')} ÷{' '}
                    {t('Recharge ratio')} ÷ {t('Price (local currency / USD)')}{' '}
                    ({localPrice || '-'}) ={' '}
                    <span className='text-foreground font-medium'>
                      {actualUsd.toFixed(4)} USD
                    </span>
                  </>
                )}
              </div>
            </>
          ) : (
            <div className='space-y-2'>
              <Label>
                {t('Amount')} ({currencyLabel})
              </Label>
              <Input
                type='number'
                step={tokensOnly ? 1 : 0.000001}
                min={mode === 'override' ? undefined : 0}
                placeholder={amountPlaceholder}
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleConfirm()
                }}
              />
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={handleCancel}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={loading}>
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
