/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { createCreditRepayment } from './api'

// 单笔回款金额上限，与后端 maxCreditRepaymentAmount 对齐。
const MAX_REPAYMENT_AMOUNT = 1e9

export type RepaymentTarget = {
  user_id: number
  username: string
  display_name: string
  outstanding: number
}

export function RepaymentDialog({
  target,
  onOpenChange,
  onSuccess,
}: {
  target: RepaymentTarget | null
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState('')
  const [remark, setRemark] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (target) {
      setAmount('')
      setRemark('')
    }
  }, [target])

  const submit = async () => {
    if (!target) return
    const value = Number.parseFloat(amount)
    if (!Number.isFinite(value) || value <= 0) {
      toast.error(t('Please enter repayment amount'))
      return
    }
    if (value > MAX_REPAYMENT_AMOUNT) {
      toast.error(t('Repayment amount is too large'))
      return
    }
    setLoading(true)
    try {
      await createCreditRepayment({
        user_id: target.user_id,
        amount: value,
        remark: remark.trim(),
      })
      toast.success(t('Repayment recorded successfully'))
      onSuccess()
      onOpenChange(false)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to record repayment')
      )
    } finally {
      setLoading(false)
    }
  }

  // 客户展示成「用户名（简称）」，简称缺失时只显示用户名
  let userLabel = ''
  if (target) {
    userLabel = target.display_name
      ? `${target.username}（${target.display_name}）`
      : target.username
  }

  return (
    <Dialog open={target !== null} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Repayment')}</DialogTitle>
        </DialogHeader>

        <div className='space-y-4'>
          <div className='text-sm'>
            <span className='text-muted-foreground'>{t('User')}: </span>
            {userLabel}
          </div>
          <div className='text-sm'>
            <span className='text-muted-foreground'>
              {t('Outstanding Credit')}:{' '}
            </span>
            <span className='tabular-nums'>
              {(target?.outstanding ?? 0).toFixed(2)}
            </span>
          </div>

          <div className='space-y-2'>
            <Label htmlFor='repayment-amount'>
              <span className='text-destructive'>*</span>
              {t('Repayment Amount')}
            </Label>
            <Input
              id='repayment-amount'
              type='number'
              min='0'
              step='0.01'
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder={t('Please enter')}
            />
          </div>

          <div className='space-y-2'>
            <Label htmlFor='repayment-remark'>{t('Remark')}</Label>
            <Textarea
              id='repayment-remark'
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              placeholder={t('Please enter')}
              rows={3}
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={submit} disabled={loading}>
            {t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
