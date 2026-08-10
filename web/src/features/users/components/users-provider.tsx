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
import React, { useState } from 'react'

import useDialogState from '@/hooks/use-dialog'

import { type User, type UsersDialogType } from '../types'

type AgentApikeyState = {
  username: string
  apikey: string
} | null

type UsersContextType = {
  open: UsersDialogType | null
  setOpen: (str: UsersDialogType | null) => void
  currentRow: User | null
  setCurrentRow: React.Dispatch<React.SetStateAction<User | null>>
  refreshTrigger: number
  triggerRefresh: () => void
  agentApikey: AgentApikeyState
  openAgentApikey: (username: string, apikey: string) => void
  closeAgentApikey: () => void
}

const UsersContext = React.createContext<UsersContextType | null>(null)

export function UsersProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useDialogState<UsersDialogType>(null)
  const [currentRow, setCurrentRow] = useState<User | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [agentApikey, setAgentApikey] = useState<AgentApikeyState>(null)

  const triggerRefresh = () => setRefreshTrigger((prev) => prev + 1)
  const openAgentApikey = (username: string, apikey: string) =>
    setAgentApikey({ username, apikey })
  const closeAgentApikey = () => setAgentApikey(null)

  return (
    <UsersContext
      value={{
        open,
        setOpen,
        currentRow,
        setCurrentRow,
        refreshTrigger,
        triggerRefresh,
        agentApikey,
        openAgentApikey,
        closeAgentApikey,
      }}
    >
      {children}
    </UsersContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useUsers = () => {
  const usersContext = React.useContext(UsersContext)

  if (!usersContext) {
    throw new Error('useUsers has to be used within <UsersContext>')
  }

  return usersContext
}
