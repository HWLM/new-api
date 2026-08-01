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
import { useQuery } from "@tanstack/react-query";
import { type ColumnDef } from "@tanstack/react-table";
import { useTranslation } from "react-i18next";

import { GroupBadge } from "@/components/group-badge";
import { StatusBadge } from "@/components/status-badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Progress } from "@/components/ui/progress";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { getUserGroups } from "@/lib/api";
import { formatQuota, formatTimestampToDate } from "@/lib/format";
import { cn } from "@/lib/utils";

import { API_KEY_STATUSES } from "../constants";
import { type ApiKey } from "../types";
import {
  ApiKeyCell,
  ModelLimitsCell,
  IpRestrictionsCell,
} from "./api-keys-cells";
import { DataTableRowActions } from "./data-table-row-actions";

function getQuotaProgressColor(percentage: number): string {
  if (percentage <= 10) return "[&_[data-slot=progress-indicator]]:bg-rose-500";
  if (percentage <= 30)
    return "[&_[data-slot=progress-indicator]]:bg-amber-500";
  return "[&_[data-slot=progress-indicator]]:bg-emerald-500";
}

function getQuotaUsagePercentage(used: number, total: number): number {
  if (total <= 0) return 100;
  return Math.max(0, Math.min(100, (used / total) * 100));
}

function getQuotaRemainingPercentage(used: number, total: number): number {
  if (total <= 0) return 100;
  const remaining = Math.max(total - used, 0);
  return Math.max(0, Math.min(100, (remaining / total) * 100));
}

function QuotaProgressRow({
  label,
  used,
  total,
  unlimitedLabel,
}: {
  label: string;
  used: number;
  total: number;
  unlimitedLabel: string;
}) {
  const isUnlimited = total <= 0;
  const remaining = isUnlimited ? null : Math.max(total - used, 0);
  const percentage =
    isUnlimited || remaining === null
      ? 100
      : getQuotaUsagePercentage(remaining, total);
  const progressClassName = cn("h-1.5", getQuotaProgressColor(percentage));

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between gap-2 text-[11px] leading-none">
        <span className="text-muted-foreground">{label}</span>
        {isUnlimited ? (
          <span className="font-medium tabular-nums">{unlimitedLabel}</span>
        ) : (
          <div className="flex min-w-[130px] items-center justify-between gap-1 font-medium tabular-nums">
            <span className="text-left">{formatQuota(remaining ?? 0)}</span>
            <span className="text-right">{formatQuota(total)}</span>
          </div>
        )}
      </div>
      <Progress value={percentage} className={progressClassName} />
    </div>
  );
}

function useGroupRatios(): Record<string, number> {
  const { data } = useQuery({
    queryKey: ["user-groups"],
    queryFn: getUserGroups,
    staleTime: 0,
    select: (res) => {
      if (!res.success || !res.data) return {};
      const ratios: Record<string, number> = {};
      for (const [group, info] of Object.entries(res.data)) {
        if (typeof info.ratio === "number") {
          ratios[group] = info.ratio;
        }
      }
      return ratios;
    },
  });

  return data ?? {};
}

export function useApiKeysColumns(): ColumnDef<ApiKey>[] {
  const { t } = useTranslation();
  const groupRatios = useGroupRatios();
  return [
    {
      id: "select",
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          indeterminate={table.getIsSomePageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label="Select all"
          className="translate-y-[2px]"
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label="Select row"
          className="translate-y-[2px]"
        />
      ),
      enableSorting: false,
      enableHiding: false,
      size: 40,
    },
    {
      accessorKey: "name",
      header: t("Name"),
      cell: ({ row }) => (
        <span className="font-medium">{row.getValue("name")}</span>
      ),
      size: 180,
      meta: { mobileTitle: true },
    },
    {
      accessorKey: "status",
      header: t("Status"),
      cell: ({ row }) => {
        const statusConfig = API_KEY_STATUSES[row.getValue("status") as number];
        if (!statusConfig) return null;
        return (
          <StatusBadge
            label={t(statusConfig.label)}
            variant={statusConfig.variant}
            copyable={false}
            className="-ml-1.5"
          />
        );
      },
      filterFn: (row, id, value) => value.includes(String(row.getValue(id))),
      size: 120,
      meta: { mobileBadge: true },
    },
    {
      id: "key",
      accessorKey: "key",
      header: t("API Key"),
      cell: ({ row }) => <ApiKeyCell apiKey={row.original} />,
      enableSorting: false,
      size: 260,
    },
    {
      id: "quota",
      accessorKey: "remain_quota",
      header: t("Quota"),
      cell: ({ row }) => {
        const apiKey = row.original;
        if (apiKey.unlimited_quota) {
          return (
            <StatusBadge
              label={t("Unlimited")}
              variant="neutral"
              copyable={false}
              className="-ml-1.5"
            />
          );
        }

        const used = apiKey.used_quota;
        const remaining = apiKey.remain_quota;
        const total = used + remaining;
        const totalRemainingPercentage = getQuotaRemainingPercentage(
          used,
          total,
        );
        const dailyUsed = apiKey.daily_used ?? 0;
        const weeklyUsed = apiKey.weekly_used ?? 0;
        const dailyQuota = apiKey.daily_quota ?? 0;
        const weeklyQuota = apiKey.weekly_quota ?? 0;
        const dailyRemaining = Math.max(dailyQuota - dailyUsed, 0);
        const weeklyRemaining = Math.max(weeklyQuota - weeklyUsed, 0);
        const dailyRemainingPercentage = getQuotaRemainingPercentage(
          dailyUsed,
          dailyQuota,
        );
        const weeklyRemainingPercentage = getQuotaRemainingPercentage(
          weeklyUsed,
          weeklyQuota,
        );

        return (
          <Tooltip>
            <TooltipTrigger render={<div className="space-y-2" />}>
              <QuotaProgressRow
                label={t("Total")}
                used={used}
                total={total}
                unlimitedLabel={t("Unlimited")}
              />
              <QuotaProgressRow
                label={t("Daily")}
                used={dailyUsed}
                total={dailyQuota}
                unlimitedLabel={t("Unlimited")}
              />
              <QuotaProgressRow
                label={t("Weekly")}
                used={weeklyUsed}
                total={weeklyQuota}
                unlimitedLabel={t("Unlimited")}
              />
            </TooltipTrigger>
            <TooltipContent>
              <div className="space-y-2 text-xs">
                <div>
                  {t("Used:")} {formatQuota(used)}
                </div>
                <div>
                  {t("Remaining:")} {formatQuota(remaining)} (
                  {totalRemainingPercentage.toFixed(1)}%)
                </div>
                <div className="grid grid-cols-[auto_1fr] gap-x-1 gap-y-1">
                  <span>{t("Total")}：</span>
                  <span className="font-medium tabular-nums">
                    {formatQuota(total)}
                  </span>
                  <span>{t("Daily")}：</span>
                  <span className="font-medium tabular-nums">
                    {dailyQuota > 0
                      ? `${formatQuota(dailyRemaining)} (${dailyRemainingPercentage.toFixed(1)}%)`
                      : t("Unlimited")}
                  </span>
                  <span>{t("Weekly")}：</span>
                  <span className="font-medium tabular-nums">
                    {weeklyQuota > 0
                      ? `${formatQuota(weeklyRemaining)} (${weeklyRemainingPercentage.toFixed(1)}%)`
                      : t("Unlimited")}
                  </span>
                </div>
              </div>
            </TooltipContent>
          </Tooltip>
        );
      },
      size: 170,
    },
    {
      accessorKey: "group",
      header: t("Group"),
      cell: ({ row }) => {
        const apiKey = row.original;
        const rawGroup = (row.getValue("group") as string) ?? "";
        const groupList = rawGroup
          .split(",")
          .map((g) => g.trim())
          .filter(Boolean);

        if (groupList.length === 0) {
          return <GroupBadge group="" />;
        }

        const isMulti = groupList.length > 1 || groupList.includes("auto");

        if (groupList.length === 1) {
          const only = groupList[0];
          if (only === "auto") {
            return (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <span className="inline-flex items-center gap-1.5 text-xs" />
                  }
                >
                  <GroupBadge group="auto" />
                  {apiKey.cross_group_retry && (
                    <StatusBadge
                      label={t("Cross-group")}
                      variant="info"
                      copyable={false}
                    />
                  )}
                </TooltipTrigger>
                <TooltipContent>
                  <span className="text-xs">
                    {t(
                      "Automatically selects the best available group with circuit breaker mechanism",
                    )}
                  </span>
                </TooltipContent>
              </Tooltip>
            );
          }
          return <GroupBadge group={only} ratio={groupRatios[only]} />;
        }

        return (
          <span className="inline-flex flex-wrap items-center gap-1 text-xs">
            {groupList.map((g, idx) => (
              <span
                key={`${g}-${idx}`}
                className="inline-flex items-center gap-1"
              >
                {idx > 0 && (
                  <span className="text-muted-foreground">{"›"}</span>
                )}
                <GroupBadge
                  group={g}
                  ratio={g === "auto" ? undefined : groupRatios[g]}
                />
              </span>
            ))}
            {isMulti && apiKey.cross_group_retry && (
              <StatusBadge
                label={t("Cross-group")}
                variant="info"
                copyable={false}
              />
            )}
          </span>
        );
      },
      size: 160,
      meta: { mobileHidden: true },
    },
    {
      id: "model_limits",
      accessorKey: "model_limits",
      header: t("Models"),
      cell: ({ row }) => <ModelLimitsCell apiKey={row.original} />,
      enableSorting: false,
      size: 160,
      meta: { mobileHidden: true },
    },
    {
      id: "allow_ips",
      accessorKey: "allow_ips",
      header: t("IP Restriction"),
      cell: ({ row }) => <IpRestrictionsCell apiKey={row.original} />,
      enableSorting: false,
      size: 160,
      meta: { mobileHidden: true },
    },
    {
      accessorKey: "created_time",
      header: t("Created"),
      cell: ({ row }) => (
        <span className="text-muted-foreground block truncate font-mono text-xs tabular-nums">
          {formatTimestampToDate(row.getValue("created_time"))}
        </span>
      ),
      size: 180,
      meta: { mobileHidden: true },
    },
    {
      accessorKey: "accessed_time",
      header: t("Last Used"),
      cell: ({ row }) => {
        const accessedTime = row.getValue("accessed_time") as number;
        if (!accessedTime) {
          return <span className="text-muted-foreground text-xs">-</span>;
        }
        return (
          <span className="text-muted-foreground block truncate font-mono text-xs tabular-nums">
            {formatTimestampToDate(accessedTime)}
          </span>
        );
      },
      size: 180,
      meta: { mobileHidden: true },
    },
    {
      accessorKey: "expired_time",
      header: t("Expires"),
      cell: ({ row }) => {
        const expiredTime = row.getValue("expired_time") as number;
        if (expiredTime === -1) {
          return (
            <StatusBadge
              label={t("Never")}
              variant="neutral"
              copyable={false}
              className="-ml-1.5"
            />
          );
        }
        const isExpired = expiredTime * 1000 < Date.now();
        return (
          <span
            className={cn(
              "block truncate font-mono text-xs tabular-nums",
              isExpired ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {formatTimestampToDate(expiredTime)}
          </span>
        );
      },
      size: 180,
      meta: { mobileHidden: true },
    },
    {
      id: "actions",
      header: () => t("Actions"),
      cell: ({ row }) => <DataTableRowActions row={row} />,
      meta: { pinned: "right" as const },
    },
  ];
}
