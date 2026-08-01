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
import { api } from "@/lib/api";
import type {
  InviterCharts,
  InviterDailyRow,
  InviterStatCards,
  InviterSummaryRow,
  PromotionStats,
  QuotaDataItem,
  TokenDailyDetailFilters,
  TokenDailyDetailItem,
  TokenExhaustingItem,
  TokenStatsSummary,
  TokenStatsTopItem,
  FlowQuotaDataItem,
  UptimeGroupResult,
} from "./types";

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

// Get user quota data within a time range
// Admin users get all users' data by default (matching classic frontend behavior)
export async function getUserQuotaDates(
  params: {
    start_timestamp: number;
    end_timestamp: number;
    default_time?: string;
    username?: string;
  },
  isAdmin = false,
) {
  const endpoint = isAdmin ? "/api/data" : "/api/data/self";
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    endpoint,
    { params },
  );
  return res.data;
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: {
  start_timestamp: number;
  end_timestamp: number;
}) {
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    "/api/data/users",
    { params },
  );
  return res.data;
}

/**
 * 渠道 / 销售 推广情况（admin）。时间窗口同时约束
 *   - 被邀请用户的 created_at
 *   - 这些用户的 logs 消耗 created_at
 * 后端已按 total_consumed 倒序，前端按 topN 截取即可。
 */
export async function getPromotionStats(params: {
  start_timestamp: number;
  end_timestamp: number;
}) {
  const res = await api.get<{ success: boolean; data: PromotionStats }>(
    "/api/data/promotion",
    { params },
  );
  return res.data;
}

// ----------------------------------------------------------------------------
// Inviter Statistics (per-user, common-user only)
// ----------------------------------------------------------------------------

export async function getInviterStatCards() {
  const res = await api.get<{ success: boolean; data: InviterStatCards }>(
    "/api/user/inviter_stats/cards",
  );
  return res.data;
}

export async function getInviterCharts(params: {
  start_timestamp: number;
  end_timestamp: number;
}) {
  const res = await api.get<{ success: boolean; data: InviterCharts }>(
    "/api/user/inviter_stats/charts",
    { params },
  );
  return res.data;
}

export async function getInviterSummary(params: {
  last_consumed_start?: number;
  last_consumed_end?: number;
  remaining_op?: string;
  remaining_value?: number;
  username?: string;
  sort_by?: string;
  sort_order?: "asc" | "desc";
}) {
  const res = await api.get<{ success: boolean; data: InviterSummaryRow[] }>(
    "/api/user/inviter_stats/summary",
    { params },
  );
  return res.data;
}

export async function getInviterDaily(params: {
  start_timestamp?: number;
  end_timestamp?: number;
  username?: string;
  sort_by?: string;
  sort_order?: "asc" | "desc";
}) {
  const res = await api.get<{ success: boolean; data: InviterDailyRow[] }>(
    "/api/user/inviter_stats/daily",
    { params },
  );
  return res.data;
}
export async function getFlowQuotaDates(
  params: {
    start_timestamp: number;
    end_timestamp: number;
    default_time?: string;
    username?: string;
  },
  isAdmin = false,
) {
  const endpoint = isAdmin ? "/api/data/flow" : "/api/data/flow/self";
  const res = await api.get<{
    success: boolean;
    data?: FlowQuotaDataItem[];
    message?: string;
  }>(endpoint, { params });
  return res.data;
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    "/api/uptime/status",
  );
  return res.data;
}

// ----------------------------------------------------------------------------
// Token Statistics
// ----------------------------------------------------------------------------

export async function getTokenStatsSummary() {
  const res = await api.get<{ success: boolean; data: TokenStatsSummary }>(
    "/api/data/token_stats/summary/self",
  );
  return res.data;
}

export async function getTokenStatsTop(limit = 10) {
  const res = await api.get<{ success: boolean; data: TokenStatsTopItem[] }>(
    "/api/data/token_stats/top/self",
    { params: { limit } },
  );
  return res.data;
}

export async function getTokenStatsExhausting(page = 1, pageSize = 10) {
  const res = await api.get<{
    success: boolean;
    data: { items: TokenExhaustingItem[]; total: number };
  }>("/api/data/token_stats/exhausting/self", {
    params: { page, page_size: pageSize },
  });
  return res.data;
}

export async function getTokenStatsDaily(filters: TokenDailyDetailFilters) {
  const res = await api.get<{
    success: boolean;
    data: {
      items: TokenDailyDetailItem[];
      total: number;
      page: number;
      page_size: number;
    };
  }>("/api/data/token_stats/daily/self", { params: filters });
  return res.data;
}
