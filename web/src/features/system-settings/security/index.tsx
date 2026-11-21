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
import { SettingsPage } from '../components/settings-page'
import type { SecuritySettings } from '../types'
import {
  SECURITY_DEFAULT_SECTION,
  getSecuritySectionContent,
  getSecuritySectionMeta,
} from './section-registry.tsx'

const defaultSecuritySettings: SecuritySettings = {
  ModelRequestRateLimitEnabled: false,
  ModelRequestRateLimitCount: 0,
  ModelRequestRateLimitSuccessCount: 1000,
  ModelRequestRateLimitDurationMinutes: 1,
  ModelRequestRateLimitGroup: '',
  MaxConcurrentRequestsPerUserEnabled: false,
  MaxConcurrentRequestsPerUser: 10,
  'refusal_demote_setting.enabled': false,
  'refusal_demote_setting.window_size': 100,
  'refusal_demote_setting.min_samples': 20,
  'refusal_demote_setting.threshold_pct': 30,
  'refusal_demote_setting.demote_seconds': 3600,
  'refusal_demote_setting.window_ttl_seconds': 86400,
  CheckSensitiveEnabled: false,
  CheckSensitiveOnPromptEnabled: false,
  SensitiveWords: '',
  'fetch_setting.enable_ssrf_protection': true,
  'fetch_setting.allow_private_ip': false,
  'fetch_setting.domain_filter_mode': false,
  'fetch_setting.ip_filter_mode': false,
  'fetch_setting.domain_list': [],
  'fetch_setting.ip_list': [],
  'fetch_setting.allowed_ports': [],
  'fetch_setting.apply_ip_filter_for_domain': false,
  'token_setting.max_user_tokens': 1000,
}

export function SecuritySettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/security/$section'
      defaultSettings={defaultSecuritySettings}
      defaultSection={SECURITY_DEFAULT_SECTION}
      getSectionContent={getSecuritySectionContent}
      getSectionMeta={getSecuritySectionMeta}
    />
  )
}
