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
import { Switch } from '@/components/ui/switch'
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
  const [overloadChatId, setOverloadChatId] = useState('')
  const [notifyConcurrency, setNotifyConcurrency] = useState(false)
  const [notifyRefusalDemote, setNotifyRefusalDemote] = useState(false)
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
          setOverloadChatId(res.data?.overload_chat_id ?? '')
          setNotifyConcurrency(res.data?.notify_concurrency_enabled ?? false)
          setNotifyRefusalDemote(
            res.data?.notify_refusal_demote_enabled ?? false
          )
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
    // chatId / overloadChatId 均允许为空：留空即表示停用对应的播报渠道。
    // bot token 仍必填——没有 token 任何消息都发不出去。
    if (!botToken.trim()) {
      toast.error(t('Bot Token is required'))
      return
    }
    setIsSaving(true)
    try {
      const res = await updateTgNotifySettings({
        bot_token: botToken.trim(),
        chat_id: chatId.trim(),
        overload_chat_id: overloadChatId.trim(),
        notify_concurrency_enabled: notifyConcurrency,
        notify_refusal_demote_enabled: notifyRefusalDemote,
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
      <DialogContent className='max-w-lg sm:max-w-xl'>
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
              className='break-all'
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
            <p className='text-xs text-muted-foreground'>
              {t(
                'Used for VIP daily report only. Leave empty to disable the VIP report.'
              )}
            </p>
          </div>

          <div className='flex flex-col gap-2'>
            <Label htmlFor='tg-overload-chat-id'>
              {t('Overload Alert chatId')}
            </Label>
            <Input
              id='tg-overload-chat-id'
              value={overloadChatId}
              onChange={(e) => setOverloadChatId(e.target.value)}
              placeholder='-1009876543210'
              disabled={isLoading || isSaving}
            />
            <p className='text-xs text-muted-foreground'>
              {t(
                'Target group for concurrency / refusal-demote alerts. Leave empty to disable overload alerts entirely.'
              )}
            </p>
          </div>

          <div className='flex items-center justify-between rounded-md border px-3 py-2'>
            <div className='pr-3'>
              <Label htmlFor='tg-notify-concurrency' className='text-sm'>
                {t('Notify on concurrency limit exceeded')}
              </Label>
              <p className='text-xs text-muted-foreground'>
                {t(
                  'Send TG alert when a user hits the per-user concurrency ceiling. Throttled to one message per user every 5 minutes.'
                )}
              </p>
            </div>
            <Switch
              id='tg-notify-concurrency'
              checked={notifyConcurrency}
              onCheckedChange={setNotifyConcurrency}
              disabled={isLoading || isSaving}
            />
          </div>

          <div className='flex items-center justify-between rounded-md border px-3 py-2'>
            <div className='pr-3'>
              <Label htmlFor='tg-notify-refusal-demote' className='text-sm'>
                {t('Notify on refusal-rate demotion triggered')}
              </Label>
              <p className='text-xs text-muted-foreground'>
                {t(
                  'Send TG alert when a user is demoted to the fallback channel because the refusal ratio crossed the threshold. One message per demotion lifecycle.'
                )}
              </p>
            </div>
            <Switch
              id='tg-notify-refusal-demote'
              checked={notifyRefusalDemote}
              onCheckedChange={setNotifyRefusalDemote}
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
