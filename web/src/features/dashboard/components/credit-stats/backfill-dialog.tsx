/*
Copyright (C) 2023-2026 QuantumNous

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { ErrorLogAlertTokenUserPicker } from '@/features/usage-logs/components/dialogs/error-log-alert-token-user-picker'
import { createCreditBackfill } from './api'

// 与后端 maxCreditRepaymentAmount 对齐
const MAX_AMOUNT = 1e9

// 1=授信 2=回款，与 model.CreditRecordType* 对齐
const TYPE_CREDIT = '1'
const TYPE_REPAYMENT = '2'

export function BackfillDialog({
  open,
  onOpenChange,
  onSuccess,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const [userId, setUserId] = useState<number | null>(null)
  const [username, setUsername] = useState('')
  const [recordType, setRecordType] = useState(TYPE_CREDIT)
  const [amount, setAmount] = useState('')
  const [occurredAt, setOccurredAt] = useState('')
  const [remark, setRemark] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (open) {
      setUserId(null)
      setUsername('')
      setRecordType(TYPE_CREDIT)
      setAmount('')
      setOccurredAt(dayjs().format('YYYY-MM-DD'))
      setRemark('')
    }
  }, [open])

  const typeItems = [
    { value: TYPE_CREDIT, label: t('Credit') },
    { value: TYPE_REPAYMENT, label: t('Repayment') },
  ]

  const submit = async () => {
    if (!userId) {
      toast.error(t('Please select a user'))
      return
    }
    const value = Number.parseFloat(amount)
    if (!Number.isFinite(value) || value <= 0) {
      toast.error(t('Please enter a valid amount'))
      return
    }
    if (value > MAX_AMOUNT) {
      toast.error(t('Amount is too large'))
      return
    }
    if (!occurredAt) {
      toast.error(t('Please select the date it occurred'))
      return
    }
    if (occurredAt > dayjs().format('YYYY-MM-DD')) {
      toast.error(t('Date cannot be in the future'))
      return
    }

    setLoading(true)
    try {
      await createCreditBackfill({
        user_id: userId,
        type: Number(recordType),
        amount: value,
        occurred_at: occurredAt,
        remark: remark.trim(),
      })
      toast.success(t('Historical record added'))
      onSuccess()
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to add record'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Add Historical Record')}</DialogTitle>
          <DialogDescription>
            {t(
              'For credit and repayment that happened before this feature went live. Only records the entry — user quota is not changed.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          <div className='space-y-2'>
            <Label>
              <span className='text-destructive'>*</span>
              {t('User')}
            </Label>
            <ErrorLogAlertTokenUserPicker
              value={userId}
              valueLabel={username}
              onChange={(id, name) => {
                setUserId(id)
                setUsername(name)
              }}
            />
          </div>

          <div className='space-y-2'>
            <Label>
              <span className='text-destructive'>*</span>
              {t('Type')}
            </Label>
            <Select
              items={typeItems}
              value={recordType}
              onValueChange={(v) => setRecordType(v as string)}
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {typeItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className='space-y-2'>
            <Label htmlFor='backfill-amount'>
              <span className='text-destructive'>*</span>
              {t('Amount (¥)')}
            </Label>
            <Input
              id='backfill-amount'
              type='number'
              min='0'
              step='0.01'
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder={t('Please enter')}
            />
          </div>

          <div className='space-y-2'>
            <Label htmlFor='backfill-date'>
              <span className='text-destructive'>*</span>
              {t('Date Occurred')}
            </Label>
            <Input
              id='backfill-date'
              type='date'
              max={dayjs().format('YYYY-MM-DD')}
              value={occurredAt}
              onChange={(e) => setOccurredAt(e.target.value)}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Use the date the credit or repayment actually happened, otherwise the monthly report will be wrong.'
              )}
            </p>
          </div>

          <div className='space-y-2'>
            <Label htmlFor='backfill-remark'>{t('Remark')}</Label>
            <Textarea
              id='backfill-remark'
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              placeholder={t('Please enter')}
              rows={2}
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
