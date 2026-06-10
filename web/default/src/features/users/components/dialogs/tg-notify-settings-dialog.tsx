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
import { Textarea } from '@/components/ui/textarea'
import { getTgNotifySettings, updateTgNotifySettings } from '../../api'

type TgNotifySettingsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TgNotifySettingsDialog(props: TgNotifySettingsDialogProps) {
  const { t } = useTranslation()
  const [botToken, setBotToken] = useState('')
  const [chatId, setChatId] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [isSaving, setIsSaving] = useState(false)

  // 打开时拉取已存配置
  useEffect(() => {
    if (!props.open) return
    let aborted = false
    setIsLoading(true)
    getTgNotifySettings()
      .then((res) => {
        if (aborted) return
        if (res.success) {
          setBotToken(res.data?.bot_token ?? '')
          setChatId(res.data?.chat_id ?? '')
        } else {
          toast.error(res.message || t('Failed to load TG settings'))
        }
      })
      .catch(() => {
        if (!aborted) toast.error(t('Failed to load TG settings'))
      })
      .finally(() => {
        if (!aborted) setIsLoading(false)
      })
    return () => {
      aborted = true
    }
  }, [props.open, t])

  const handleSave = async () => {
    if (!botToken.trim() || !chatId.trim()) {
      toast.error(t('Bot Token and chatId are required'))
      return
    }
    setIsSaving(true)
    try {
      const res = await updateTgNotifySettings({
        bot_token: botToken.trim(),
        chat_id: chatId.trim(),
      })
      if (res.success) {
        toast.success(t('TG settings saved'))
        props.onOpenChange(false)
      } else {
        toast.error(res.message || t('Failed to save TG settings'))
      }
    } catch {
      toast.error(t('Failed to save TG settings'))
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('TG Group Settings')}</DialogTitle>
        </DialogHeader>

        <div className='flex flex-col gap-4 py-2'>
          <div className='flex flex-col gap-2'>
            <Label htmlFor='tg-bot-token'>{t('Bot Token')}</Label>
            <Textarea
              id='tg-bot-token'
              value={botToken}
              onChange={(e) => setBotToken(e.target.value)}
              placeholder='123456789:ABC-DEF1234ghIkl-zyx57W2v1u123ew11'
              disabled={isLoading || isSaving}
              rows={3}
            />
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='tg-chat-id'>{t('chatId')}</Label>
            <Input
              id='tg-chat-id'
              value={chatId}
              onChange={(e) => setChatId(e.target.value)}
              placeholder='-1001234567890'
              disabled={isLoading || isSaving}
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={isSaving}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave} disabled={isLoading || isSaving}>
            {isSaving && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
