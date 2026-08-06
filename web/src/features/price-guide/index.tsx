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
import {
  BarChart3,
  CreditCard,
  Info,
  RefreshCcw,
  ReceiptText,
  Sparkles,
  Tag,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { PageTransition } from "@/components/page-transition";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";

import { DynamicPricingBreakdown } from "../pricing/components/dynamic-pricing-breakdown";
import { usePricingData } from "../pricing/hooks/use-pricing-data";
import { stripTrailingZeros } from "../pricing/lib/price";
import type { PricingModel } from "../pricing/types";
import {
  getActualUsdPrice,
  getBestPriceGuideGroup,
  filterModelsByGroup,
  getGroupRatioSavingPercent,
  getPriceGuidePricingSource,
  getPriceGuideSavingPercent,
  getPriceGuideGroupOptions,
  getRequestBaseUsdPrice,
  getSelectedGroupRatio,
  getTokenBaseUsdPrice,
  isDynamicPricingGuideModel,
} from "./lib";

const GUIDE_SKELETON_KEYS = ["overview", "compare", "tokens", "requests"];

function formatUsdMoney(amountUsd: number | null): string {
  if (amountUsd == null || Number.isNaN(amountUsd)) return "-";
  return stripTrailingZeros(
    new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: "USD",
      currencyDisplay: "narrowSymbol",
      minimumFractionDigits: 0,
      maximumFractionDigits: Math.abs(amountUsd) >= 1 ? 4 : 6,
    }).format(amountUsd),
  );
}

function formatCnyMoney(amountCny: number | null): string {
  if (amountCny == null || Number.isNaN(amountCny)) return "-";
  return stripTrailingZeros(
    new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: "CNY",
      currencyDisplay: "narrowSymbol",
      minimumFractionDigits: 0,
      maximumFractionDigits: Math.abs(amountCny) >= 1 ? 4 : 6,
    }).format(amountCny),
  );
}

function formatPercent(value: number | null): string {
  if (value == null || !Number.isFinite(value)) return "-";
  return `${Math.round(value)}%`;
}

function hasPositiveSaving(value: number | null): boolean {
  return value != null && Number.isFinite(value) && value > 0;
}

function formatExchangeRate(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "-";
  return stripTrailingZeros(value.toFixed(2));
}

function PriceGuideTopPanel(props: {
  onRefresh: () => void;
  groupCount: number;
  groupOptions: Array<{
    value: string;
    label: string;
    description: string;
    ratio: number;
  }>;
  selectedGroup: string;
  selectedGroupOption: {
    value: string;
    label: string;
    description: string;
    ratio: number;
  } | null;
  selectedGroupRatio: number;
  selectedGroupSavingsPercent: number | null;
  onSelectedGroupChange: (value: string) => void;
  priceRate: number;
  usdExchangeRate: number;
}) {
  const { t } = useTranslation();
  const exchangeRate = formatExchangeRate(props.usdExchangeRate);

  const selectedGroupNote =
    props.selectedGroupOption?.description ||
    props.selectedGroupOption?.label ||
    "-";
  return (
    <section className="relative overflow-hidden bg-gradient-to-br from-primary/10 via-background to-background mb-0 px-4 py-5 sm:px-6">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-x-0 top-0 h-24 bg-gradient-to-b from-primary/10 to-transparent"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -top-16 right-0 h-48 w-48 rounded-full bg-primary/10 blur-3xl"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -bottom-20 left-0 h-56 w-56 rounded-full bg-primary/5 blur-3xl"
      />
      <div className="relative flex flex-col gap-5">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0 flex-1 space-y-2">
            <div className="text-sm font-medium text-muted-foreground">
              {t("Price Guide")}
            </div>
            <h1 className="text-2xl font-bold tracking-tight sm:text-3xl">
              {t("Model prices at a glance")}
            </h1>
            <p className="text-muted-foreground max-w-3xl text-sm leading-relaxed">
              {t(
                "By default, we show the lowest-priced group available to you. Switch groups anytime to compare. Each model directly shows “how much you actually pay in CNY”, so it is easy to understand.",
              )}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className="gap-1 rounded-full">
              <span className="size-2 rounded-full bg-muted-foreground/60" />
              {t("Default cheapest, compare anytime")}
            </Badge>
            <Button
              onClick={props.onRefresh}
              variant="outline"
              className="shrink-0 gap-1.5 bg-background/80 shadow-sm"
            >
              <RefreshCcw className="size-4" />
              {t("Refresh prices")}
            </Button>
          </div>
        </div>

        <div className="rounded-xl border border-primary/20 bg-card/80 p-4 shadow-sm">
          <div className="flex flex-col lg:flex-row lg:items-start">
            <div className="min-w-0 space-y-3">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Sparkles className="size-4 text-primary" />
                {t("Select group")}
              </div>
              <div className="flex flex-wrap items-center">
                <div className="w-full lg:max-w-[680px]">
                  <Select
                    value={props.selectedGroup}
                    onValueChange={(value) =>
                      props.onSelectedGroupChange(value ?? "")
                    }
                  >
                    <SelectTrigger className="h-14 w-full rounded-xl border-primary/30 bg-background px-4 text-sm shadow-sm [&_svg]:text-primary">
                      <SelectValue className="min-w-0">
                        <span className="flex min-w-0 items-center gap-2">
                          <span className="truncate font-medium">
                            {props.selectedGroupOption
                              ? [
                                  props.selectedGroupOption.label,
                                  `${props.selectedGroupRatio.toFixed(2)}x`,
                                  hasPositiveSaving(
                                    props.selectedGroupSavingsPercent,
                                  )
                                    ? `${t("Price savings label")}${formatPercent(
                                        props.selectedGroupSavingsPercent,
                                      )}`
                                    : "",
                                ]
                                  .filter(Boolean)
                                  .join(" · ")
                              : t("Select group")}
                          </span>
                        </span>
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {props.groupOptions.map((option) => {
                          const optionSavingsPercent =
                            getGroupRatioSavingPercent(option.ratio);

                          return (
                            <SelectItem key={option.value} value={option.value}>
                              <span className="flex min-w-0 items-center gap-2">
                                <span className="truncate font-medium">
                                  {option.label}
                                </span>
                                <span className="text-muted-foreground/70 font-mono text-xs">
                                  {option.ratio.toFixed(2)}x
                                </span>
                                {hasPositiveSaving(optionSavingsPercent) && (
                                  <span className="text-primary font-medium text-xs">
                                    {t("Price savings label")}
                                    {formatPercent(optionSavingsPercent)}
                                  </span>
                                )}
                              </span>
                            </SelectItem>
                          );
                        })}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-wrap items-center gap-2 lg:shrink-0  ml-3">
                  <Badge
                    variant="outline"
                    className="gap-1.5 border-primary/30 bg-primary/10 font-mono text-primary"
                  >
                    {t("Multiplier")}: {props.selectedGroupRatio.toFixed(2)}x
                  </Badge>
                  {hasPositiveSaving(props.selectedGroupSavingsPercent) && (
                    <Badge
                      variant="secondary"
                      className="gap-1.5 border-primary/20 bg-primary/10 font-medium text-primary"
                    >
                      {t("Price savings label")}:{" "}
                      {formatPercent(props.selectedGroupSavingsPercent)} 🔥
                    </Badge>
                  )}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1.5">
                  <Info className="size-3.5" />
                  {t(
                    "{{count}} groups available. Use the dropdown above to switch; prices below update instantly.",
                    { count: props.groupCount },
                  )}
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <CreditCard className="size-3.5" />
                  {t("Recharge 1 CNY = $1 quota")} (1:1)
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <ReceiptText className="size-3.5" />
                  {t("The “You pay” column below is the actual CNY charge.")}
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <BarChart3 className="size-3.5" />
                  {t(
                    "Official prices are converted at the live exchange rate ≈ ¥{{rate}}/$ for comparison only.",
                    { rate: exchangeRate },
                  )}
                </span>
              </div>
            </div>
          </div>
          <div className="mt-3 text-xs text-muted-foreground/80">
            {selectedGroupNote}
          </div>
        </div>
      </div>
    </section>
  );
}

function PriceRow(props: {
  label: string;
  officialUsd: number | null;
  actualUsd: number | null;
  usdExchangeRate: number;
  unitLabel: string;
  officialCny?: number | null;
  actualCny?: number | null;
}) {
  const officialCny =
    props.officialCny ??
    (props.officialUsd == null
      ? null
      : props.officialUsd * props.usdExchangeRate);
  const actualCny =
    props.actualCny ??
    (props.actualUsd == null ? null : props.actualUsd * props.usdExchangeRate);

  if (officialCny == null || actualCny == null) {
    return null;
  }

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3 py-3">
      <div className="min-w-0">
        <span className="text-muted-foreground text-xs font-medium">
          {props.label}
        </span>
      </div>
      <div className="ml-auto flex min-w-0 flex-wrap items-baseline justify-end gap-x-3 gap-y-1 text-right">
        {props.officialUsd != null && (
          <span className="text-muted-foreground/50 font-mono text-xs line-through tabular-nums">
            {formatUsdMoney(props.officialUsd)}
          </span>
        )}
        <span className="text-muted-foreground/50 font-mono text-xs line-through tabular-nums">
          {formatCnyMoney(officialCny)}
        </span>
        <span className="font-mono text-sm font-bold text-primary tabular-nums">
          {formatCnyMoney(actualCny)}
        </span>
        <span className="text-muted-foreground/50 text-[10px]">
          {props.unitLabel}
        </span>
      </div>
    </div>
  );
}

function DynamicModelCard(props: {
  model: PricingModel;
  selectedGroupRatio: number;
  priceRate: number;
  usdExchangeRate: number;
}) {
  const { t } = useTranslation();
  const isDynamic = isDynamicPricingGuideModel(props.model);
  const badgeLabel = isDynamic ? t("Dynamic Pricing") : t("Model");
  const savingPercent = getPriceGuideSavingPercent({
    model: props.model,
    selectedGroupRatio: props.selectedGroupRatio,
    priceRate: props.priceRate,
    usdExchangeRate: props.usdExchangeRate,
  });
  const pricingSource = getPriceGuidePricingSource(props.model);
  const isConvertedSource = pricingSource === "converted";
  const currencyRate = isConvertedSource ? 1 : props.priceRate;
  const officialCurrencyRate = isConvertedSource ? 1 : props.usdExchangeRate;

  return (
    <Card className="border-dashed">
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="truncate font-mono text-base">
              {props.model.model_name}
            </CardTitle>
            <CardDescription className="mt-1 flex flex-wrap items-center gap-2">
              <Badge variant="outline" className="gap-1">
                <Tag className="size-3" />
                {badgeLabel}
              </Badge>
              {hasPositiveSaving(savingPercent) && (
                <Badge variant="secondary" className="font-medium">
                  {t("Price savings label")}: {formatPercent(savingPercent)}
                </Badge>
              )}
            </CardDescription>
          </div>
          <Badge variant="secondary" className="shrink-0">
            {t("Dynamic Pricing")}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-2 pt-4">
        {props.model.billing_expr ? (
          <DynamicPricingBreakdown
            billingExpr={props.model.billing_expr}
            compact
            currencySymbol="¥"
            currencyRate={currencyRate}
            officialCurrencyRate={officialCurrencyRate}
            showComparison
            groupRatioMultiplier={props.selectedGroupRatio}
          />
        ) : (
          <p className="text-muted-foreground text-sm">
            {t("Special billing expression")}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function StaticModelCard(props: {
  model: PricingModel;
  selectedGroupRatio: number;
  priceRate: number;
  usdExchangeRate: number;
}) {
  const { t } = useTranslation();
  const tokenBased = props.model.quota_type === 0;
  const tokenInputUsd = getTokenBaseUsdPrice(props.model, "input");
  const tokenOutputUsd = getTokenBaseUsdPrice(props.model, "output");
  const tokenCacheUsd = getTokenBaseUsdPrice(props.model, "cache");
  const tokenCreateCacheUsd =
    tokenInputUsd != null && props.model.create_cache_ratio != null
      ? tokenInputUsd * props.model.create_cache_ratio
      : null;
  const requestUsd = getRequestBaseUsdPrice(props.model);
  const pricingSource = getPriceGuidePricingSource(props.model);
  const isConvertedSource = pricingSource === "converted";

  const savingPercent = getPriceGuideSavingPercent({
    model: props.model,
    selectedGroupRatio: props.selectedGroupRatio,
    priceRate: props.priceRate,
    usdExchangeRate: props.usdExchangeRate,
  });

  const requestUnit = ` / ${t("request")}`;
  const tokenUnit = "M token";
  const actualPriceBadge = tokenBased ? t("Token-based") : t("Per Request");
  let cacheReadCny: number | null = null;
  if (tokenCacheUsd != null) {
    cacheReadCny = isConvertedSource
      ? tokenCacheUsd * props.selectedGroupRatio
      : tokenCacheUsd * props.selectedGroupRatio * props.priceRate;
  }
  let cacheWriteCny: number | null = null;
  if (tokenCreateCacheUsd != null) {
    cacheWriteCny = isConvertedSource
      ? tokenCreateCacheUsd * props.selectedGroupRatio
      : tokenCreateCacheUsd * props.selectedGroupRatio * props.priceRate;
  }

  return (
    <Card>
      <CardHeader className="border-b">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="truncate font-mono text-base">
              {props.model.model_name}
            </CardTitle>
            <CardDescription className="mt-1 flex flex-wrap items-center gap-2">
              <Badge variant="outline">{actualPriceBadge}</Badge>
            </CardDescription>
          </div>
          {/* <Badge variant="secondary" className="shrink-0">
            {props.selectedGroupRatio.toFixed(2)}x
          </Badge> */}
          {hasPositiveSaving(savingPercent) && (
            <Badge variant="secondary" className="font-medium">
              {t("Price savings label")}: {formatPercent(savingPercent)}
            </Badge>
          )}
        </div>
      </CardHeader>

      <CardContent className="space-y-2 pt-4">
        {tokenBased ? (
          <>
            <PriceRow
              label={t("Input")}
              officialUsd={tokenInputUsd}
              actualUsd={getActualUsdPrice(
                tokenInputUsd ?? 0,
                props.selectedGroupRatio,
                props.priceRate,
                props.usdExchangeRate,
              )}
              officialCny={isConvertedSource ? tokenInputUsd : undefined}
              actualCny={
                isConvertedSource && tokenInputUsd != null
                  ? tokenInputUsd * props.selectedGroupRatio
                  : undefined
              }
              usdExchangeRate={props.usdExchangeRate}
              unitLabel={tokenUnit}
            />
            <PriceRow
              label={t("Output")}
              officialUsd={tokenOutputUsd}
              actualUsd={getActualUsdPrice(
                tokenOutputUsd ?? 0,
                props.selectedGroupRatio,
                props.priceRate,
                props.usdExchangeRate,
              )}
              officialCny={isConvertedSource ? tokenOutputUsd : undefined}
              actualCny={
                isConvertedSource && tokenOutputUsd != null
                  ? tokenOutputUsd * props.selectedGroupRatio
                  : undefined
              }
              usdExchangeRate={props.usdExchangeRate}
              unitLabel={tokenUnit}
            />
            {(cacheReadCny != null || cacheWriteCny != null) && (
              <div className="rounded-lg border px-3 py-3">
                <p className="flex flex-wrap items-baseline gap-x-1 gap-y-1 text-xs leading-relaxed">
                  <span className="text-muted-foreground font-medium">
                    {t("Price savings label")}
                  </span>
                  {cacheReadCny != null && (
                    <>
                      <span className="text-foreground font-mono font-semibold tabular-nums">
                        {formatCnyMoney(cacheReadCny)}
                      </span>
                      <span className="text-muted-foreground/60">
                        /百万token
                      </span>
                    </>
                  )}
                  {cacheReadCny != null && cacheWriteCny != null && (
                    <span className="text-muted-foreground/60">-</span>
                  )}
                  {cacheWriteCny != null && (
                    <>
                      <span className="text-foreground font-mono font-semibold tabular-nums">
                        {formatCnyMoney(cacheWriteCny)}
                      </span>
                      <span className="text-muted-foreground/60">
                        /百万token
                      </span>
                    </>
                  )}
                </p>
              </div>
            )}
          </>
        ) : (
          <PriceRow
            label={t("Price")}
            officialUsd={requestUsd}
            actualUsd={getActualUsdPrice(
              requestUsd ?? 0,
              props.selectedGroupRatio,
              props.priceRate,
              props.usdExchangeRate,
            )}
            officialCny={isConvertedSource ? requestUsd : undefined}
            actualCny={
              isConvertedSource && requestUsd != null
                ? requestUsd * props.selectedGroupRatio
                : undefined
            }
            usdExchangeRate={props.usdExchangeRate}
            unitLabel={requestUnit}
          />
        )}
      </CardContent>
    </Card>
  );
}

function GuideSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-10 w-40" />
        <Skeleton className="h-5 w-full max-w-xl" />
      </div>
      <Skeleton className="h-24 w-full rounded-xl" />
      <div className="grid gap-4 xl:grid-cols-2">
        {GUIDE_SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className="h-40 w-full rounded-xl" />
        ))}
      </div>
    </div>
  );
}

export function PriceGuide() {
  const { t } = useTranslation();
  const {
    models,
    usableGroup,
    groupRatio,
    isLoading,
    refetch,
    priceRate,
    usdExchangeRate,
  } = usePricingData();
  const [selectedGroup, setSelectedGroup] = useState("");

  const groupOptions = useMemo(
    () => getPriceGuideGroupOptions(usableGroup || {}, groupRatio || {}),
    [groupRatio, usableGroup],
  );

  const bestGroup = useMemo(
    () => getBestPriceGuideGroup(groupOptions),
    [groupOptions],
  );

  useEffect(() => {
    if (groupOptions.length === 0) {
      setSelectedGroup("");
      return;
    }

    const hasCurrent = groupOptions.some(
      (option) => option.value === selectedGroup,
    );
    if (!hasCurrent) {
      setSelectedGroup(bestGroup);
    }
  }, [bestGroup, groupOptions, selectedGroup]);

  const selectedGroupOption = useMemo(
    () => groupOptions.find((option) => option.value === selectedGroup) ?? null,
    [groupOptions, selectedGroup],
  );

  const selectedGroupRatio = useMemo(
    () => getSelectedGroupRatio(groupRatio || {}, selectedGroup),
    [groupRatio, selectedGroup],
  );

  const selectedGroupSavingsPercent = useMemo(
    () => getGroupRatioSavingPercent(selectedGroupRatio),
    [selectedGroupRatio],
  );

  const visibleModels = useMemo(
    () => filterModelsByGroup(models || [], selectedGroup),
    [models, selectedGroup],
  );

  const displayModels = useMemo(
    () =>
      [...visibleModels]
        .sort((left, right) =>
          (left.model_name || "").localeCompare(right.model_name || ""),
        ),
    [visibleModels],
  );

  const handleRefresh = useCallback(() => {
    void refetch();
  }, [refetch]);

  if (isLoading) {
    return (
      <PageTransition className="mx-auto w-full">
        <GuideSkeleton />
      </PageTransition>
    );
  }

  if (!models || models.length === 0) {
    return (
      <PageTransition className="mx-auto w-full overflow-auto">
        <div className="flex flex-col gap-6">
          <PriceGuideTopPanel
            onRefresh={handleRefresh}
            groupCount={groupOptions.length}
            groupOptions={groupOptions}
            selectedGroup={selectedGroup}
            selectedGroupOption={selectedGroupOption}
            selectedGroupRatio={selectedGroupRatio}
            selectedGroupSavingsPercent={selectedGroupSavingsPercent}
            onSelectedGroupChange={setSelectedGroup}
            priceRate={priceRate}
            usdExchangeRate={usdExchangeRate}
          />
          <div className="flex min-h-[360px] flex-col items-center justify-center rounded-xl border border-dashed px-6 text-center">
            <Sparkles className="text-muted-foreground/40 mb-3 size-10" />
            <h1 className="text-xl font-semibold">{t("Price Guide")}</h1>
            <p className="text-muted-foreground mt-2 max-w-xl text-sm">
              {t("No pricing data available.")}
            </p>
            <Button
              className="mt-5 gap-1.5"
              variant="outline"
              onClick={handleRefresh}
            >
              <RefreshCcw className="size-4" />
              {t("Refresh prices")}
            </Button>
          </div>
        </div>
      </PageTransition>
    );
  }

  return (
    <PageTransition className="mx-auto w-full overflow-auto">
      <div className="space-y-6">
        <PriceGuideTopPanel
          onRefresh={handleRefresh}
          groupCount={groupOptions.length}
          groupOptions={groupOptions}
          selectedGroup={selectedGroup}
          selectedGroupOption={selectedGroupOption}
          selectedGroupRatio={selectedGroupRatio}
          selectedGroupSavingsPercent={selectedGroupSavingsPercent}
          onSelectedGroupChange={setSelectedGroup}
          priceRate={priceRate}
          usdExchangeRate={usdExchangeRate}
        />

        <section className="space-y-4 rounded-xl bg-background/30 p-4">
          {displayModels.length > 0 ? (
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {displayModels.map((model) =>
                isDynamicPricingGuideModel(model) ? (
                  <DynamicModelCard
                    key={model.id ?? model.model_name}
                    model={model}
                    selectedGroupRatio={selectedGroupRatio}
                    priceRate={priceRate}
                    usdExchangeRate={usdExchangeRate}
                  />
                ) : (
                  <StaticModelCard
                    key={model.id ?? model.model_name}
                    model={model}
                    selectedGroupRatio={selectedGroupRatio}
                    priceRate={priceRate}
                    usdExchangeRate={usdExchangeRate}
                  />
                ),
              )}
            </div>
          ) : (
            <Card>
              <CardContent className="py-10 text-center">
                <p className="text-muted-foreground text-sm">
                  {t("No models available in this group.")}
                </p>
              </CardContent>
            </Card>
          )}

        </section>
      </div>
    </PageTransition>
  );
}
