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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { DistillDetectSection } from './distill-detect-section'
import { DistillJevCallsSection } from './distill-jev-calls-section'

/**
 * 系统设置 → 蒸馏检测：两个标签页。
 * 「设置」是原有卡片；「Jev 调用记录」仅管理员可见（与设置页同一套权限，
 * 后端 GET /api/distill_detect/jev_calls 走 AdminAuth）。
 * 调用记录只在切到该页时才挂载，避免打开设置页就去拉流水。
 */
export function DistillDetectTabs() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('settings')

  return (
    <Tabs value={tab} onValueChange={setTab} className='flex flex-col gap-4'>
      <TabsList>
        <TabsTrigger value='settings'>{t('Settings')}</TabsTrigger>
        <TabsTrigger value='jev-calls'>{t('Jev call records')}</TabsTrigger>
      </TabsList>
      <TabsContent value='settings'>
        <DistillDetectSection />
      </TabsContent>
      <TabsContent value='jev-calls'>
        {tab === 'jev-calls' ? <DistillJevCallsSection /> : null}
      </TabsContent>
    </Tabs>
  )
}
