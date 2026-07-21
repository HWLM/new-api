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
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getUserAvatarFallback, getUserAvatarStyle } from '@/lib/avatar'
import { cn } from '@/lib/utils'

import type { UsageLog } from '../data/schema'
import { useUsageLogsContext } from './usage-logs-provider'

interface UsageLogUserCellProps {
  log: UsageLog
  isAdmin: boolean
}

export function UsageLogUserCell(props: UsageLogUserCellProps) {
  const { sensitiveVisible, setSelectedUserId, setUserInfoDialogOpen } =
    useUsageLogsContext()

  if (!props.log.username) return null

  const content = (
    <>
      <Avatar className='ring-border/60 size-6 ring-1 max-sm:hidden'>
        <AvatarFallback
          className={cn(
            'text-[11px] font-semibold',
            !sensitiveVisible && 'bg-muted text-muted-foreground'
          )}
          style={
            sensitiveVisible
              ? getUserAvatarStyle(props.log.username)
              : undefined
          }
        >
          {sensitiveVisible ? getUserAvatarFallback(props.log.username) : '•'}
        </AvatarFallback>
      </Avatar>
      <TooltipProvider delay={300}>
        <Tooltip>
          <TooltipTrigger
            render={
              <span
                className={cn(
                  'text-muted-foreground max-w-[100px] truncate text-sm',
                  props.isAdmin && 'hover:underline'
                )}
              />
            }
          >
            {sensitiveVisible ? props.log.username : '••••'}
          </TooltipTrigger>
          {sensitiveVisible && props.log.username.length > 12 && (
            <TooltipContent side='top'>{props.log.username}</TooltipContent>
          )}
        </Tooltip>
      </TooltipProvider>
    </>
  )

  if (!props.isAdmin) {
    return <div className='flex items-center gap-1.5'>{content}</div>
  }

  return (
    <button
      type='button'
      className='flex items-center gap-1.5 text-left'
      onClick={(event) => {
        event.stopPropagation()
        setSelectedUserId(props.log.user_id)
        setUserInfoDialogOpen(true)
      }}
    >
      {content}
    </button>
  )
}
