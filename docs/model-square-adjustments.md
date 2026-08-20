# 模型廣場調整說明

本文把目前「模型廣場」相關功能整理成一份開發與評審基線。這裡的模型廣場對應前台 `/rankings` 頁面；模型價格頁 `/pricing` 的模型卡片與模型詳情頁則提供同一套模型的性能指標。兩者目前是兩條獨立資料鏈，不能把排行榜用量直接當成性能資料。

## 1. 調整目標

模型廣場需要同時回答三個問題：

1. 哪些模型在指定期間被使用最多？
2. 各模型供應商的用量占比與排名變化如何？
3. 模型目前的延遲、成功率和吞吐表現如何？

目前第 1、2 項由 Rankings 完成，第 3 項由 Performance Metrics 完成。後續調整應先維持兩套資料的職責邊界，再決定是否在同一個頁面匯總展示。

## 2. 現有功能基線

### 2.1 導航與權限

- 前台路由：`/rankings`。
- 後端接口：`GET /api/rankings`。
- 導航模組名稱：`rankings`。
- 預設狀態：啟用、免登入。
- 管理員可在維護設定中調整：是否啟用、是否要求登入。
- 前端路由在載入前重新讀取模組權限；模組關閉時重定向首頁，要求登入時重定向登入頁。

相關位置：

| 功能 | 代碼 |
| --- | --- |
| 前台路由與權限 | `web/src/routes/rankings/index.tsx` |
| 頁面組裝 | `web/src/features/rankings/index.tsx` |
| 導航模組設定 | `web/src/lib/nav-modules.ts`、`web/src/features/system-settings/maintenance/header-navigation-section.tsx` |
| 後端路由 | `router/api-router.go` |
| 後端控制器 | `controller/rankings.go` |

### 2.2 排行榜資料來源

排行榜從 `quota_data` 表讀取 `token_used`，不是從請求日誌即時計算：

```text
relay 成功請求
  -> LogQuotaData / CacheQuotaData
  -> 定時寫入 quota_data
  -> /api/rankings 聚合 token_used
  -> 前端排行榜、供應商占比、趨勢圖
```

`quota_data` 的聚合維度包含：`model_name`、使用者、分組、Token、渠道、節點及小時桶。排行榜只使用 `model_name`、`created_at` 和 `token_used`，因此不會暴露使用者或 Token 維度。

### 2.3 支援期間

`period` 只允許以下值：

| period | 查詢區間 | 趨勢桶 |
| --- | --- | --- |
| `today` | 最近 24 小時 | 1 小時 |
| `week` | 最近 7 天 | 1 天 |
| `month` | 最近 30 天 | 1 天 |
| `year` | 最近 365 天 | 7 天 |

未傳值時使用 `week`；非法值由後端返回 `400`。前後端都使用同一組枚舉，切換期間只更新 URL query `?period=`。

### 2.4 返回內容

`GET /api/rankings?period=week` 返回：

```json
{
  "success": true,
  "data": {
    "models": [],
    "vendors": [],
    "top_movers": [],
    "top_droppers": [],
    "models_history": {"points": [], "models": [], "buckets": 0},
    "vendor_share_history": {"points": [], "vendors": [], "buckets": 0}
  }
}
```

模型排名欄位：

- `rank`：當期排名；
- `previous_rank`：上一個等長期間的排名，首次出現時省略；
- `model_name`、`vendor`、`vendor_icon`；
- `total_tokens`：當期 token 用量；
- `share`：占當期全部 token 的比例，四位小數；
- `growth_pct`：相對上一期間的用量增長百分比。

供應商排名會把同一供應商下的模型彙總，並提供 `models_count` 和 `top_model`。模型趨勢只保留前 10 個模型，其餘合併為 `Others`；供應商占比只保留前 5 個，其餘合併為 `Others`。上升/下降榜各保留 6 筆。

### 2.5 前端展示

頁面目前由四個區塊組成：

1. **Hero/期間切換**：標題、說明、Today/Week/Month/Year tabs。
2. **Top Models**：模型 token 用量堆疊柱狀圖 + LLM Leaderboard。
3. **Market Share**：供應商 token 占比堆疊圖 + 供應商列表。
4. **Trending up / Trending down**：排名上升與下降模型。

模型名連到 `/pricing/{model}`，供應商名連到 `/pricing?vendor={vendor}`。沒有資料時顯示空狀態；查詢期間顯示骨架屏；接口錯誤顯示錯誤狀態。

## 2.6 前端展示調整細則

### 使用者與主要任務

- **訪客/普通使用者**：快速找到熱門模型，查看供應商占比，進入價格頁了解模型詳情。
- **運營/管理員**：觀察模型用量變化和性能異常，決定模型排序、供應商展示及後續渠道調整。
- **主要成功操作**：切換期間、掃描排行、點擊模型或供應商進入價格詳情。

頁面不應把排行榜做成管理表格；第一屏應先突出熱門模型和期間切換，性能指標作為輔助資訊，不應搶過模型名稱、排名和用量。

### 建議頁面層級

```text
頁面標題 + 資料說明 + 期間 tabs
  -> Top Models（用量趨勢 + 模型排行榜）
  -> Market Share（供應商占比 + 供應商列表）
  -> Trending up / Trending down
  -> 可選：模型性能摘要
```

調整時保留目前的閱讀順序，但將資料新鮮度放在標題說明附近，例如「資料截至 2026-08-14 09:20，約 5 分鐘延遲」。不要把 `generated_at`、快取 TTL 或資料庫欄位名稱直接暴露給一般使用者。

### Top Models 展示規則

- 桌面端：趨勢圖在上，排行榜在下；排行榜使用兩欄等高列表。
- 移動端：圖表高度固定在可滾動區域內，排行榜切換為單欄，模型名最多兩行或使用尾端截斷。
- 每行固定欄位：排名、供應商圖標、模型名/供應商、token 用量、增長率。
- 排名和用量使用等寬數字，保證不同數字長度不造成欄位跳動。
- 增長率不可只依賴顏色：上升/下降需同時使用箭頭、符號或文字提示。
- 圖表 tooltip 顯示當期總量，超過 10 個模型時合併為 `Others`，與後端歷史資料保持一致。

### Market Share 展示規則

- 使用 100% 堆疊圖表展示供應商占比，左側刻度固定為 0% 至 100%。
- 圖例或列表顯示供應商名稱、占比、token 總量；未知供應商統一顯示 `Unknown`。
- 供應商顏色必須穩定，同一供應商在圖表、列表和價格頁保持一致；不能依當次返回順序重新分配顏色。
- 移動端列表由雙欄改為單欄，長供應商名可截斷，但 tooltip/無障礙標籤要保留完整名稱。
- `Others` 只代表被合併的供應商，不應讓使用者誤以為是實際供應商。

### 期間切換與交互

- tabs 使用 URL query（`?period=today|week|month|year`），可分享、刷新和使用瀏覽器返回。
- 切換期間時顯示局部 loading 狀態，保留頁面骨架結構，避免整頁跳動。
- 新查詢開始後應取消或忽略舊查詢結果，避免慢返回覆蓋最新期間。
- 非法期間由路由 schema 回退到預設值；接口 `400` 應顯示可恢復的錯誤提示和重新載入操作。
- 模型/供應商連結應保留來源期間，返回價格頁後使用者返回瀏覽器可以回到原來的廣場位置。

### Loading、空資料與錯誤狀態

| 狀態 | 展示要求 |
| --- | --- |
| 初次載入 | 顯示標題、tabs 和與正式內容等高的骨架屏 |
| 無排行榜資料 | 顯示「此期間暫無用量資料」，不要顯示 0 名模型的空圖例 |
| 只有模型無供應商 | 模型排行榜正常展示，供應商區顯示 `Unknown` 或空狀態 |
| 性能資料缺失 | 排行榜仍可使用，性能徽章顯示「暫無性能資料」或直接隱藏 |
| 400/500 錯誤 | 說明資料載入失敗，提供重試；不要只呈現原始後端錯誤字串 |
| 模組被關閉 | 路由層重定向，不在頁面內渲染半成品內容 |

### 性能指標在模型廣場中的放置

性能指標建議作為模型行的次要徽章，順序為「延遲 / TPS / 成功率」，只在有真實樣本時出現。模型詳情頁才展示完整的分組表格與趨勢圖，避免廣場同時渲染大量小圖表。

推薦接口流程：

```text
GET /api/rankings?period=week
  -> 取得排行主資料
GET /api/perf-metrics/summary?hours=24
  -> 一次取得所有模型的性能摘要
前端按 model_name 建立 Map
  -> 在排行榜行按需渲染徽章
```

不要對每一個模型單獨請求性能接口；排行榜前 20 筆模型最多只應觸發一次批量摘要查詢。性能接口失敗不能阻塞排行榜主接口。

### 響應式與無障礙要求

- 主要內容最大寬度與價格頁一致，桌面端維持舒適閱讀寬度，手機端使用左右內距而非橫向溢出。
- 所有期間 tabs、模型連結、供應商連結可用鍵盤操作，焦點狀態清晰可見。
- 圖表不能是唯一資訊來源；排行榜和供應商列表必須提供同等數據的可讀文本。
- 顏色只作為輔助，成功率、增長率、異常狀態同時使用文字、圖標或 aria-label。
- 供應商圖標失敗時使用中性 fallback，不影響名稱和數值讀取。
- 翻譯後的長文案、阿拉伯數字和供應商名稱不能造成卡片高度跳變或重疊。

### 視覺調整方向

- 保留目前卡片、圖表、列表的分區結構，但降低裝飾性背景對數據的干擾；現有頁面頂部的多層 radial gradient 應評估移除或改為低對比單色背景。
- 以排名、模型名、token 用量建立主次對比，供應商和性能指標使用較弱文字層級。
- 卡片圓角、邊框、間距沿用既有設計系統，不新增與價格頁不一致的視覺語言。
- 動效只用於期間切換、圖表首次出現和成功率狀態變化，避免持續動畫造成數據閱讀干擾。

## 2.7 視頻模型分組展示

### 分組原則

視頻模型應按「能力/接口類型」分組，而不是按模型名稱關鍵字硬編碼。推薦優先級：

1. `supported_endpoint_types` 包含 `openai-video`：歸入視頻模型；
2. 後端提供 `output_modalities` 包含 `video`：歸入視頻模型；
3. 只有歷史資料沒有端點或 modality 時，才允許前端暫時使用模型名兼容規則，並標記為 legacy fallback；
4. 分組內再按 `vendor_name` 分段，段內沿用熱門度/名稱/價格排序。

不能把 `enable_groups` 當成視頻模型分組。`enable_groups` 是計費和渠道可用分組，例如 `default`、代理商分組，不代表模型的媒體能力。

### 建議頁面結構

在模型廣場或價格頁增加能力分段 tabs/segmented control：

```text
全部模型
  ├─ 對話/文本
  ├─ 圖像
  ├─ 視頻
  │    ├─ 文生視頻
  │    ├─ 圖生視頻
  │    ├─ 參考視頻/視頻編輯（有資料時才顯示）
  │    └─ 供應商分組
  ├─ 音頻
  └─ Embeddings / Rerank
```

第一階段只需要穩定呈現「視頻」主分組和供應商子分組；文生視頻、圖生視頻等更細分的標籤必須由後端能力字段或明確 metadata 支持，不能只依賴 `sora`、`veo`、`kling` 等名稱猜測。

### 視頻模型卡片

視頻卡片在現有模型卡片基礎上增加以下信息，並保持固定高度：

- 模型名、供應商圖標和供應商名；
- `Video` 能力徽章；
- 計費方式：按次或動態計費；
- 主要能力：文生視頻、圖生視頻、視頻輸入、音頻生成（只有接口明確提供時才展示）；
- 支持的分辨率/時長範圍（需要後端返回 metadata，沒有資料時不顯示空標籤）；
- 按次價格或動態計費提示；
- 最近 24 小時的延遲、TPS、成功率摘要（性能資料缺失時不占位空欄）。

視頻模型不應把 token 單價當成唯一主信息，因為目前視頻模型大量採用按次、時長、分辨率或 tiered_expr 計費。卡片主價格應直接顯示「每次」或「按時長/分辨率計算」，避免使用者將聊天模型的每 1K token 價格套用到視頻模型。

### 數據字段要求

目前前端 `PricingModel` 已預留 `input_modalities`、`output_modalities`、`capabilities` 和 `supported_endpoint_types`，但後端 `model.Pricing` 實際穩定提供的是 `supported_endpoint_types`。要做可靠分組，後端至少應補齊：

```json
{
  "model_name": "example-video",
  "vendor_name": "Example Vendor",
  "supported_endpoint_types": ["openai-video"],
  "input_modalities": ["text", "image", "video"],
  "output_modalities": ["video"],
  "capabilities": ["streaming"]
}
```

視頻專用的 `duration_range`、`resolutions`、`supports_audio` 等字段應由模型 metadata 或渠道能力聚合生成，不能在前端根據模型名拼接。若同一模型在多個渠道能力不同，返回值應取可用能力的並集，並在詳情頁按渠道/分組展示差異。

### 篩選、排序和 URL

- 視頻分組應映射到現有 endpoint filter `openai-video`，避免新建與後端不一致的 `video` 枚舉。
- 選中視頻分組後，搜索、供應商、計費方式、用戶分組、tag 等篩選仍可疊加。
- 分組和篩選狀態寫入 URL，例如 `?endpointType=openai-video&vendor=...`，刷新後保持狀態。
- 默認排序建議為用量/熱門度；沒有用量資料時按供應商、模型名稱穩定排序。
- 搜索結果為空時，要說明是「沒有符合條件的視頻模型」，並提供清除篩選操作。

### 視頻模型性能展示

視頻模型性能必須和普通文本模型分開定義：

- 普通模型：延遲、TTFT、TPS、成功率；
- 異步視頻：提交耗時、排隊耗時、生成耗時、完成率、失敗率、結果可用率。

在異步視頻性能接口完成前，視頻模型卡片不要顯示普通模型的 TTFT/TPS，避免把提交接口延遲誤當成視頻生成速度。可先展示「性能資料收集中」或只展示已確認的提交成功率。

### 視頻分組驗收

- [ ] 端點為 `openai-video` 的模型全部進入視頻分組，沒有視頻端點的模型不被誤收錄。
- [ ] 同一模型多渠道能力合併後只展示一張卡片，不出現重複模型。
- [ ] 供應商子分組、供應商圖標和 Unknown fallback 穩定。
- [ ] 文生視頻/圖生視頻等細分標籤只使用後端能力字段，不使用不可驗證的名稱猜測。
- [ ] 視頻按次、時長、分辨率和 tiered_expr 價格展示不套用聊天 token 價格文案。
- [ ] 沒有性能樣本時不顯示假的 TTFT、TPS 或成功率。
- [ ] 桌面雙欄、手機單欄、長模型名、長供應商名和多語言文案均不重疊。

## 3. 性能指標資料鏈

性能指標不是從 `quota_data` 推導，而是由 relay 請求完成時直接採樣：

```text
RelayInfo.StartTime / FirstResponseTime
  -> perfmetrics.RecordRelaySample
  -> hotBuckets（記憶體原子桶）
  -> Redis 活躍桶（可選）
  -> perf_metrics 表
  -> /api/perf-metrics 或 /api/perf-metrics/summary
```

採樣欄位：

- 請求數、成功數；
- 總延遲、TTFT（首 Token 時間）；
- 輸出 token 數、生成耗時。

可計算：平均延遲、平均 TTFT、成功率、平均 TPS。

### 3.1 性能接口

| 接口 | 用途 | 主要參數 |
| --- | --- | --- |
| `GET /api/perf-metrics/summary` | 模型卡片批量徽章 | `hours`，最大 30 天 |
| `GET /api/perf-metrics` | 單一模型詳情 | `model` 必填、可選 `group`、`hours` |

兩個接口都受 `pricing` 模組權限控制，並只返回當前有效分組的資料。模型卡片目前查詢最近 24 小時；模型詳情頁展示 TPS、平均延遲、成功率、分組表格、TTFT 趨勢和可用性趨勢。

### 3.2 性能採樣範圍

目前成功採樣接在普通文字 relay 的 quota 記錄流程，失敗採樣接在普通 relay 錯誤流程。異步視頻任務的提交/輪詢路徑沒有直接調用 `RecordRelaySample`，因此視頻任務的耗時和成功率不會自然出現在性能指標中。若模型廣場要展示視頻模型性能，必須定義任務耗時口徑後另行接入。

## 4. 配置與保留策略

性能指標配置鍵為 `perf_metrics_setting`：

| 配置 | 預設 | 說明 |
| --- | --- | --- |
| `enabled` | `true` | 是否採集指標 |
| `flush_interval` | 5 分鐘 | 記憶體桶刷入資料庫週期，最小 1 分鐘 |
| `bucket_time` | `hour` | `minute`、`5min`、`hour` |
| `retention_days` | 0 | 0 表示永久保留 |

記憶體桶只在完整桶結束後刷庫；刷庫失敗會把計數加回記憶體桶。Redis 只用於活躍桶補讀，資料庫仍是歷史查詢來源。排行榜自身另有 5 分鐘進程內快取，因此 `quota_data` 刷新週期和排行榜快取會共同造成展示延遲。

## 5. 本次調整建議

### P0：先明確產品口徑

- 將頁面對外名稱統一為「模型廣場」，技術路由可保留 `/rankings`，避免接口和翻譯 key 大規模改名。
- 明確排行榜排序依據是 `token_used`，不是請求次數、扣費 quota 或收入。
- 明確性能指標只代表普通 relay 請求；若要涵蓋視頻、圖片和異步任務，分別定義成功、開始、完成和失敗時間。
- 在頁面說明資料延遲來源：quota_data 定時寫入 + 5 分鐘排行榜快取。

### P1：資料與接口調整

- 為 `/api/rankings` 補充響應時間、資料截止時間或 `generated_at`，讓前端能展示資料新鮮度。
- 將排名常量（前 20 模型、前 5 供應商、前 6 movers、前 10 趨勢模型）集中為可配置或接口元資料，避免前後端各自硬編碼。
- 如果要支持「分類」篩選，後端必須提供真實分類欄位或分類映射；目前 `category` 固定為 `all`，前端類型雖保留多個分類值但沒有可用的分類查詢。
- 對 `Unknown` 供應商增加管理員可修正的映射策略，避免模型元資料缺失時長期聚合到同一類。
- 對 `quota_data`、`perf_metrics` 增加資料庫索引和跨 SQLite/MySQL/PostgreSQL 的查詢驗證，特別是時間範圍、分組和桶聚合。

### P1：性能指標整合

- 在排行榜模型行增加可選性能徽章，優先重用 `/api/perf-metrics/summary`，不要對每個模型發獨立請求。
- 性能徽章至少標示平均延遲、TPS、成功率，並在資料不足時顯示「暫無資料」，不要用模擬數據冒充即時數據。
- 若性能指標採樣關閉或沒有有效分組，前端應隱藏徽章或顯示不可用狀態，不影響排行榜主資料。
- 统一模型名匹配策略：排行榜使用原始 `model_name`，性能接口也必須使用同一模型名，模型映射後的上游名稱不得覆蓋展示名稱。

### P2：體驗與可運維性

- 期間切換保留在 URL，支持分享和瀏覽器返回；切換時取消舊查詢，避免慢請求覆蓋新期間。
- 為模型和供應商建立穩定的排序 tie-break 規則；目前供應商同量時按名稱排序，模型排序依賴資料庫 `ORDER BY total_tokens DESC`，應補充同量時的名稱排序。
- 增加空資料、非法 period、資料庫錯誤、快取過期和模型元資料缺失的測試。
- 監控性能採樣丟失率、刷庫失敗次數、`quota_data` 延遲和排行榜接口耗時。

## 6. 建議接口演進

在不破壞現有客戶端的前提下，可將返回包裝為：

```json
{
  "success": true,
  "data": { "models": [], "vendors": [], "top_movers": [], "top_droppers": [], "models_history": {}, "vendor_share_history": {} },
  "meta": {
    "period": "week",
    "generated_at": 0,
    "data_until": 0,
    "ranking_basis": "token_used",
    "cache_ttl_seconds": 300
  }
}
```

`meta` 建議新增而不是改名現有欄位。若需加入性能資料，建議使用可選的 `performance` 欄位或單獨接口，避免排行榜因性能採樣資料庫不可用而整體失敗。

## 7. 驗收清單

### 後端

- [ ] `today/week/month/year` 的時間範圍和上一期間計算正確。
- [ ] `token_used <= 0`、空模型名不進入排行榜。
- [ ] 模型、供應商、趨勢和歷史資料排序穩定。
- [ ] `Others` 合併後占比與總量一致。
- [ ] 未配置供應商的模型歸入 `Unknown`，不會請求失敗。
- [ ] SQLite、MySQL、PostgreSQL 均可執行桶聚合查詢。
- [ ] 排行榜快取按 period 隔離，快取過期後能重新查詢。
- [ ] 性能接口限制最大查詢時長，並過濾失效分組。

### 前端

- [ ] 模組關閉、要求登入、接口 400/500 和空資料狀態可用。
- [ ] 期間切換同步 URL，刷新頁面後狀態保持。
- [ ] 模型、供應商連結可正確跳轉價格頁。
- [ ] 桌面兩欄與手機單欄不重疊，長模型名可截斷且不破壞數值欄。
- [ ] 性能徽章只使用真實接口資料，不回退到 mock stats。
- [ ] 新增或調整的 UI 文案同步 `en/zh/zh-TW/fr/ja/ru/vi`。

### 數據與監控

- [ ] `quota_data` 定時刷庫和排行榜快取延遲有可觀測指標。
- [ ] `perf_metrics` 刷庫失敗會恢復記憶體計數，不丟失整桶資料。
- [ ] 明確記錄視頻/圖片/流式/非流式請求是否納入性能統計。
- [ ] 保留策略生效時不刪除仍在查詢窗口內的資料。

## 8. 相關代碼索引

- 排行榜計算：`service/rankings.go`
- 排行榜 SQL：`model/usedata_rankings.go`
- 用量資料寫入：`model/usedata.go`
- 排行榜接口：`controller/rankings.go`、`router/api-router.go`
- 排行榜前端：`web/src/features/rankings/`
- 性能指標模型：`model/perf_metric.go`
- 性能採樣與刷庫：`pkg/perf_metrics/metrics.go`、`pkg/perf_metrics/flush.go`
- 性能接口：`controller/perf_metrics.go`
- 模型卡片/詳情性能展示：`web/src/features/pricing/components/model-card-grid.tsx`、`model-details-performance.tsx`
- 性能配置：`setting/perf_metrics_setting/config.go`、`web/src/features/system-settings/integrations/monitoring-settings-section.tsx`
