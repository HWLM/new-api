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
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

type AgentApikeyDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  username: string
  apikey: string
}

export function AgentApikeyDialog(props: AgentApikeyDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>{t('Agent apikey')}</DialogTitle>
        </DialogHeader>

        <div className='flex min-w-0 flex-col gap-3 py-2'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Agent apikey for user [{{username}}] generated. Please associate it in your agent site.',
              { username: props.username }
            )}
          </p>
          <div className='flex min-w-0 items-center gap-2 text-sm'>
            <span className='text-muted-foreground shrink-0'>
              {t('Agent apikey')}:
            </span>
            <code
              className='flex-1 min-w-0 truncate rounded bg-muted px-2 py-1 font-mono text-xs text-foreground'
              title={props.apikey}
            >
              {props.apikey}
            </code>
            <CopyButton
              value={props.apikey}
              variant='outline'
              size='sm'
              className='shrink-0'
              tooltip={t('Copy')}
              successTooltip={t('Copied!')}
            >
              {t('Copy')}
            </CopyButton>
          </div>
        </div>

        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
