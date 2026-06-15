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
import { Loader2 } from 'lucide-react'
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
import { setUserBusinessChannel } from '../../api'

type BusinessChannelDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 用于回显当前值（变更场景）；标记场景传空串 */
  initialChannel: string
  userId: number
  username: string
  /** "标记为商务账号" or "变更商务渠道" */
  mode: 'mark' | 'change'
  onSuccess: () => void
}

export function BusinessChannelDialog(props: BusinessChannelDialogProps) {
  const { t } = useTranslation()
  const [channel, setChannel] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)

  // 弹窗打开时回显
  useEffect(() => {
    if (props.open) {
      setChannel(props.initialChannel)
    }
  }, [props.open, props.initialChannel])

  const title =
    props.mode === 'mark'
      ? t('Mark as Business Account')
      : t('Change Business Channel')

  const handleSubmit = async () => {
    const trimmed = channel.trim()
    if (!trimmed) {
      toast.error(t('Business channel is required'))
      return
    }
    setIsSubmitting(true)
    try {
      const res = await setUserBusinessChannel(props.userId, trimmed)
      if (res.success) {
        toast.success(
          props.mode === 'mark'
            ? t('Marked as business account')
            : t('Business channel updated')
        )
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        toast.error(res.message || t('Operation failed'))
      }
    } catch {
      toast.error(t('Operation failed'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-w-md'>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>

        <div className='flex flex-col gap-2 py-2'>
          <Label htmlFor='business-channel-input'>
            {t('Business Channel')}
          </Label>
          <Input
            id='business-channel-input'
            autoFocus
            value={channel}
            onChange={(e) => setChannel(e.target.value)}
            disabled={isSubmitting}
            maxLength={255}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !isSubmitting) handleSubmit()
            }}
          />
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={isSubmitting}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={handleSubmit} disabled={isSubmitting}>
            {isSubmitting && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
