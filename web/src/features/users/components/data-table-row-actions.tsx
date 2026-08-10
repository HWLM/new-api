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
import type { Row } from '@tanstack/react-table'
import {
  MoreHorizontal,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  ArrowUp,
  ArrowDown,
  KeyRound,
  ShieldAlert,
  Link2,
  CreditCard,
  Briefcase,
  BriefcaseBusiness,
  Repeat,
  History,
  UserCheck,
  UserX,
  Copy,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { UserSubscriptionsDialog } from '@/features/subscriptions/components/dialogs/user-subscriptions-dialog'

import {
  manageUser,
  markUserAsAgent,
  resetUserPasskey,
  resetUserTwoFA,
  setUserBusinessChannel,
  unmarkUserAsAgent,
  getAgentApikey,
} from '../api'
import {
  USER_STATUS,
  USER_ROLE,
  ERROR_MESSAGES,
  isUserDeleted,
} from '../constants'
import { getUserActionMessage } from '../lib'
import type { ManageUserAction, User } from '../types'
import { BusinessChannelDialog } from './dialogs/business-channel-dialog'
import { UserBindingDialog } from './dialogs/user-binding-dialog'
import { UserQuotaHistoryDialog } from './dialogs/user-quota-history-dialog'
import { useUsers } from './users-provider'

interface DataTableRowActionsProps {
  row: Row<User>
}

export function DataTableRowActions({ row }: DataTableRowActionsProps) {
  const { t } = useTranslation()
  const user = row.original
  const { setOpen, setCurrentRow, triggerRefresh, openAgentApikey } =
    useUsers()
  const [resetPasskeyOpen, setResetPasskeyOpen] = useState(false)
  const [resetTwoFAOpen, setResetTwoFAOpen] = useState(false)
  const [bindingDialogOpen, setBindingDialogOpen] = useState(false)
  const [subscriptionsDialogOpen, setSubscriptionsDialogOpen] = useState(false)
  const [businessDialogOpen, setBusinessDialogOpen] = useState(false)
  const [quotaHistoryOpen, setQuotaHistoryOpen] = useState(false)
  const [businessDialogMode, setBusinessDialogMode] = useState<
    'mark' | 'change'
  >('mark')
  const [unmarkBusinessOpen, setUnmarkBusinessOpen] = useState(false)
  const [markAgentOpen, setMarkAgentOpen] = useState(false)
  const [unmarkAgentOpen, setUnmarkAgentOpen] = useState(false)
  const [isAgentSubmitting, setIsAgentSubmitting] = useState(false)

  const handleEdit = () => {
    setCurrentRow(user)
    setOpen('update')
  }

  const handleDelete = () => {
    setCurrentRow(user)
    setOpen('delete')
  }

  const handleManage = async (action: Exclude<ManageUserAction, 'delete'>) => {
    try {
      const result = await manageUser(user.id, action)
      if (result.success) {
        toast.success(t(getUserActionMessage(action)))
        triggerRefresh()
      } else {
        toast.error(
          result.message || t('Failed to {{action}} user', { action })
        )
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    }
  }

  const handleResetPasskey = async () => {
    try {
      const result = await resetUserPasskey(user.id)
      if (result.success) {
        toast.success(t('Passkey reset successfully'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Failed to reset Passkey'))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setResetPasskeyOpen(false)
    }
  }

  const handleUnmarkBusiness = async () => {
    try {
      const result = await setUserBusinessChannel(user.id, '')
      if (result.success) {
        toast.success(t('Business account removed'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setUnmarkBusinessOpen(false)
    }
  }

  const handleMarkAgent = async () => {
    setIsAgentSubmitting(true)
    try {
      const result = await markUserAsAgent(user.id)
      if (result.success && result.data?.key) {
        toast.success(t('Marked as agent'))
        setMarkAgentOpen(false)
        openAgentApikey(user.username, result.data.key)
        triggerRefresh()
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsAgentSubmitting(false)
    }
  }

  const handleUnmarkAgent = async () => {
    setIsAgentSubmitting(true)
    try {
      const result = await unmarkUserAsAgent(user.id)
      if (result.success) {
        toast.success(t('Agent identity removed'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsAgentSubmitting(false)
      setUnmarkAgentOpen(false)
    }
  }

  const handleCopyAgentApikey = async () => {
    try {
      const result = await getAgentApikey(user.id)
      if (result.success && result.data?.key) {
        openAgentApikey(user.username, result.data.key)
      } else {
        toast.error(result.message || t('Failed to fetch agent apikey'))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    }
  }

  const handleResetTwoFA = async () => {
    try {
      const result = await resetUserTwoFA(user.id)
      if (result.success) {
        toast.success(t('Two-factor authentication reset'))
        triggerRefresh()
      } else {
        toast.error(result.message || t('Failed to reset 2FA'))
      }
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setResetTwoFAOpen(false)
    }
  }

  const isDisabled = user.status === USER_STATUS.DISABLED
  const isAdmin = user.role >= USER_ROLE.ADMIN
  const isRoot = user.role === USER_ROLE.ROOT

  if (isUserDeleted(user)) {
    return null
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant='ghost'
              className='data-popup-open:bg-muted flex h-8 w-8 p-0'
            />
          }
        >
          <MoreHorizontal className='h-4 w-4' />
          <span className='sr-only'>{t('Open menu')}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-[180px]'>
          <DropdownMenuItem onClick={handleEdit}>
            {t('Edit')}
            <DropdownMenuShortcut>
              <Pencil size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          {isDisabled ? (
            <DropdownMenuItem onClick={() => handleManage('enable')}>
              {t('Enable')}
              <DropdownMenuShortcut>
                <Power size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem
              onClick={() => handleManage('disable')}
              disabled={isRoot}
            >
              {t('Disable')}
              <DropdownMenuShortcut>
                <PowerOff size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {isAdmin && !isRoot && (
            <DropdownMenuItem onClick={() => handleManage('demote')}>
              {t('Demote')}
              <DropdownMenuShortcut>
                <ArrowDown size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {!isAdmin && (
            <DropdownMenuItem onClick={() => handleManage('promote')}>
              {t('Promote')}
              <DropdownMenuShortcut>
                <ArrowUp size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setBindingDialogOpen(true)
            }}
          >
            {t('Manage Bindings')}
            <DropdownMenuShortcut>
              <Link2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setSubscriptionsDialogOpen(true)
            }}
          >
            {t('Manage Subscriptions')}
            <DropdownMenuShortcut>
              <CreditCard size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setQuotaHistoryOpen(true)
            }}
          >
            {t('Quota change history')}
            <DropdownMenuShortcut>
              <History size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          {/* 商务账号：未标记显示"标记"；已标记显示"变更"+"移除" */}
          {!user.business_channel ? (
            <DropdownMenuItem
              onSelect={(event) => {
                event.preventDefault()
                setBusinessDialogMode('mark')
                setBusinessDialogOpen(true)
              }}
            >
              {t('Mark as Business Account')}
              <DropdownMenuShortcut>
                <Briefcase size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          ) : (
            <>
              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  setBusinessDialogMode('change')
                  setBusinessDialogOpen(true)
                }}
              >
                {t('Change Business Channel')}
                <DropdownMenuShortcut>
                  <Repeat size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  setUnmarkBusinessOpen(true)
                }}
              >
                {t('Remove Business Account')}
                <DropdownMenuShortcut>
                  <BriefcaseBusiness size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>
            </>
          )}

          {/* 代理身份：未标记显示"标记为代理商"；已标记显示"复制代理 apikey"+"取消代理身份" */}
          {!user.is_agent ? (
            <DropdownMenuItem
              onSelect={(event) => {
                event.preventDefault()
                setMarkAgentOpen(true)
              }}
              disabled={isRoot}
            >
              {t('Mark as Agent')}
              <DropdownMenuShortcut>
                <UserCheck size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          ) : (
            <>
              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  handleCopyAgentApikey()
                }}
              >
                {t('Copy agent apikey')}
                <DropdownMenuShortcut>
                  <Copy size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={(event) => {
                  event.preventDefault()
                  setUnmarkAgentOpen(true)
                }}
                className='text-destructive focus:text-destructive'
              >
                {t('Remove Agent Identity')}
                <DropdownMenuShortcut>
                  <UserX size={16} />
                </DropdownMenuShortcut>
              </DropdownMenuItem>
            </>
          )}

          <DropdownMenuSeparator />

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setResetPasskeyOpen(true)
            }}
            disabled={isRoot}
          >
            {t('Reset Passkey')}
            <DropdownMenuShortcut>
              <KeyRound size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuItem
            onSelect={(event) => {
              event.preventDefault()
              setResetTwoFAOpen(true)
            }}
            disabled={isRoot}
          >
            {t('Reset 2FA')}
            <DropdownMenuShortcut>
              <ShieldAlert size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          <DropdownMenuItem
            onClick={handleDelete}
            className='text-destructive focus:text-destructive'
            disabled={isRoot}
          >
            {t('Delete')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <ConfirmDialog
        open={resetPasskeyOpen}
        onOpenChange={setResetPasskeyOpen}
        title={t('Reset Passkey')}
        desc={`Reset Passkey for ${user.username}? The user will need to register a new Passkey before using passwordless login.`}
        confirmText='Reset Passkey'
        handleConfirm={handleResetPasskey}
      />

      <ConfirmDialog
        open={resetTwoFAOpen}
        onOpenChange={setResetTwoFAOpen}
        title={t('Reset Two-Factor Authentication')}
        desc={`Reset 2FA for ${user.username}? The user must set up 2FA again to continue using it.`}
        confirmText='Reset 2FA'
        handleConfirm={handleResetTwoFA}
      />

      <UserBindingDialog
        open={bindingDialogOpen}
        onOpenChange={setBindingDialogOpen}
        userId={user.id}
        onUnbindSuccess={triggerRefresh}
      />

      <UserSubscriptionsDialog
        open={subscriptionsDialogOpen}
        onOpenChange={setSubscriptionsDialogOpen}
        user={{ id: user.id, username: user.username }}
        onSuccess={triggerRefresh}
      />

      {quotaHistoryOpen ? (
        <UserQuotaHistoryDialog
          open
          onOpenChange={setQuotaHistoryOpen}
          user={{ id: user.id, username: user.username, quota: user.quota }}
        />
      ) : null}

      <BusinessChannelDialog
        open={businessDialogOpen}
        onOpenChange={setBusinessDialogOpen}
        userId={user.id}
        username={user.username}
        initialChannel={user.business_channel ?? ''}
        mode={businessDialogMode}
        onSuccess={triggerRefresh}
      />

      <ConfirmDialog
        open={unmarkBusinessOpen}
        onOpenChange={setUnmarkBusinessOpen}
        title={t('Remove Business Account')}
        desc={t('Remove business account marker for {{username}}?', {
          username: user.username,
        })}
        confirmText={t('Remove Business Account')}
        handleConfirm={handleUnmarkBusiness}
      />

      <ConfirmDialog
        open={markAgentOpen}
        onOpenChange={setMarkAgentOpen}
        title={t('Mark as Agent')}
        desc={t('Mark user {{username}} as an agent?', {
          username: user.username,
        })}
        confirmText={t('Mark as Agent')}
        handleConfirm={handleMarkAgent}
        isLoading={isAgentSubmitting}
      />

      <ConfirmDialog
        open={unmarkAgentOpen}
        onOpenChange={setUnmarkAgentOpen}
        title={t('Remove Agent Identity')}
        desc={t('After removing the agent identity, users on the associated site will no longer be able to log in. Confirm removal?')}
        confirmText={t('Remove Agent Identity')}
        destructive
        handleConfirm={handleUnmarkAgent}
        isLoading={isAgentSubmitting}
      />
    </>
  )
}
