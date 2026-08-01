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
import { useRef, useState } from 'react'
import { SettingsPageFormActions } from '../components/settings-page-context'
import type { CustomMenuPagesConfig, HeaderNavModulesConfig } from './config'
import { CustomMenuPagesSection } from './custom-menu-pages-section'
import {
  HeaderNavigationSection,
  type SectionHandle,
} from './header-navigation-section'

type Props = {
  headerNavConfig: HeaderNavModulesConfig
  headerNavSerialized: string
  customMenuConfig: CustomMenuPagesConfig
  customMenuSerialized: string
}

export function HeaderNavCombinedSection({
  headerNavConfig,
  headerNavSerialized,
  customMenuConfig,
  customMenuSerialized,
}: Props) {
  const headerRef = useRef<SectionHandle>(null)
  const customRef = useRef<SectionHandle>(null)
  const [headerPending, setHeaderPending] = useState(false)
  const [customPending, setCustomPending] = useState(false)

  const handleSave = async () => {
    // Run both submits in parallel — each handles its own validation/toast.
    // Failures inside RHF's handleSubmit don't reject the promise, so a bad
    // form just no-ops while the good one still saves.
    await Promise.all([
      headerRef.current?.submit(),
      customRef.current?.submit(),
    ])
  }

  const handleReset = () => {
    headerRef.current?.reset()
    customRef.current?.reset()
  }

  return (
    <>
      <SettingsPageFormActions
        onSave={handleSave}
        onReset={handleReset}
        isSaving={headerPending || customPending}
        resetLabel='Reset to default'
        saveLabel='Save navigation'
      />
      <HeaderNavigationSection
        config={headerNavConfig}
        initialSerialized={headerNavSerialized}
        hideActions
        onPendingChange={setHeaderPending}
        actionsRef={headerRef}
      />
      <CustomMenuPagesSection
        config={customMenuConfig}
        initialSerialized={customMenuSerialized}
        hideActions
        onPendingChange={setCustomPending}
        actionsRef={customRef}
      />
    </>
  )
}
