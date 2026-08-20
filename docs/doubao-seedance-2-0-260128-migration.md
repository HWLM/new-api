# `doubao-seedance-2-0-260128` 開發與遷移文檔

> 本文針對版本化模型 `doubao-seedance-2-0-260128`，整理目前 new-api 的請求、上游調用、異步任務、資金鏈、資產接口與前端模型廣場展示。新系統應以本文的「遷移要求」為實作契約；通用預扣、結算、退款規則參見 [`video-task-billing-chain.md`](video-task-billing-chain.md)，模型廣場規則參見 [`model-square-adjustments.md`](model-square-adjustments.md)。

## 1. 模型與適用範圍

| 項目 | 現行值 |
| --- | --- |
| 公開模型名 | `doubao-seedance-2-0-260128` |
| 統一模型集合 | `dto.SeedanceV3UnifiedModels` |
| Doubao task adapter | `relay/channel/task/doubao` |
| 後端模型清單 | `relay/channel/task/doubao/constants.go` 的 `ModelList` |
| 主要頻道類型 | `ChannelTypeDoubaoVideo (54)`；`ChannelTypeVolcEngine (45)` 亦使用同一 adapter |
| 相近版本 | `doubao-seedance-2-0-fast-260128`、`doubao-seedance-2-0`、`doubao-seedance-2-0-fast`、`doubao-seedance-2-0-filter-off` |
| 預設上游建立接口 | `POST {baseURL}/api/v3/contents/generations/tasks` |

此版本屬於普通 Doubao 分支，不是 `dreamina-seedance-2-0-hc` 的 HC 分支。HC 使用 Byteplus/SDRealMax 的額外轉換邏輯，遷移時不可把兩者合併成同一個 request schema。

## 2. 端到端調用流程

```text
客戶端 POST
  ├─ /api/v3/contents/generations/tasks       (原生 Seedance V3)
  ├─ /v1/video/generations                    (相容接口)
  └─ /v1/videos                               (OpenAI 相容接口)
        |
        v
SeedanceV3RequestConvert
  - 讀取 model、content.text、content.image_url
  - 生成 TaskSubmitReq
  - 非 HC 將完整原始 body 放入 Metadata
        |
        v
TokenAuth + Distribute
  - 依 model 選擇頻道
  - POST 設為 RelayModeVideoSubmit
        |
        v
controller.RelayTask -> relay.RelayTaskSubmit
  - 驗證與模型映射
  - EstimateBilling
  - 強制全額預扣 wallet/subscription + token quota
  - 呼叫 Doubao adapter
  - 成功後建立 Task，保存上游 task id 與 BillingContext
        |
        v
Doubao upstream
  POST /api/v3/contents/generations/tasks
  GET  /api/v3/contents/generations/tasks/{upstream_task_id}
        |
        v
async_task_poll -> ParseTaskResult
  pending/queued -> QUEUED
  processing/running -> IN_PROGRESS
  succeeded -> SUCCESS + URL + usage + 實際 resolution/duration
  failed/expired -> FAILURE + error
        |
        v
SUCCESS: 依實際 token/分辨率補扣或退款
FAILURE/超時: 以 Task.Quota 為基準只退款一次
```

### 2.1 路由與公開任務 ID

- `POST /api/v3/contents/generations/tasks`、`GET /api/v3/contents/generations/tasks/:task_id`：原生 Seedance 回應，公開欄位為平台生成的 `task_<random>`。
- `POST /v1/video/generations`、`GET /v1/video/generations/:task_id`：相容格式；建立接口回傳 OpenAI video task 結構。
- `POST /v1/videos`、`GET /v1/videos/:task_id`：OpenAI video 路由。
- `GET /v1/videos/:task_id/content`：結果 URL 代理，避免直接暴露上游 URL（若系統啟用代理）。

平台 task ID 與上游 ID 必須分離：

```text
Task.TaskID                         = 公開 task_<random>
Task.PrivateData.UpstreamTaskID    = Doubao 回傳的 id
Task.PrivateData.BillingContext    = 提交時的計費快照
Task.PrivateData.ResultURL/代理 URL = 成功後的結果
```

GET 輪詢使用任務建立時保存的 channel/model，不重新分配頻道；新系統也必須遵守此規則，否則可能以錯誤 API key 查詢任務。

## 3. 請求轉換、驗證與上游 payload

### 3.1 `SeedanceV3RequestConvert` 行為

只處理 `POST /api/v3/contents/generations/tasks`：

1. 從 `content` 陣列提取所有非空 `text`，以換行合併為 `TaskSubmitReq.Prompt`。
2. 提取 `image_url.url` 為 `TaskSubmitReq.Images`。
3. 解析 `duration`；允許 `-1` 作為 Doubao「由模型決定」哨兵值。對 Doubao，`-1` 只保留在 `Metadata`，不寫入通用 duration，避免被當作時長乘數。
4. 非 HC 模型把完整原始 JSON 存入 `TaskSubmitReq.Metadata`，因此 `video_url`、`audio_url`、`tools`、`resolution`、`ratio`、`generate_audio`、`watermark`、`draft`、`frames`、`camera_fixed` 等欄位不會遺失。
5. `duration` 必須是整數，範圍為 `-1..MaxTaskDurationSeconds`（目前上限 3600）；非法值直接返回 400。

通用 task 驗證仍需檢查 prompt、輸入數量與 metadata 乘數。新系統不可只驗證頂層欄位，必須同時限制 metadata/pass-through 內的 duration、batch 等可計費數值。

### 3.2 Doubao adapter payload

一般情況由 `convertToRequestPayload` 重建：

- `model`：模型映射後的上游模型名。
- `content`：保留圖片與 metadata 中的影音項，再追加一個標準 `text` 項。
- `duration`、`resolution`、`ratio`、`generate_audio`、`watermark`、`audio_url`、`video_url`、`tools`、`service_tier` 等從 metadata overlay 還原。
- `/v1/video/generations` 的相容預設 duration=5 會被省略，避免把預設值誤當成顯式參數。

若頻道設定 `video_request_format_by_model=<model>:openai`，且路徑是 `/v1/video/generations`，adapter 會直接轉發原始 body，只調整 `model`；新系統若提供此開關，必須對每個模型做白名單配置。

上游請求頭：

```text
Content-Type: application/json
Accept: application/json
Authorization: Bearer <channel key>
```

可使用 `seedance_v3_routes.task_create/task_get` 覆寫建立、查詢路徑、HTTP method、參數注入與回應映射。未配置時使用上述預設路徑。

## 4. 上游回應與輪詢狀態

建立成功只保存上游 `id`，對客戶立即回傳 queued 狀態；不把上游 ID 當成公開 ID。

| Doubao status | 內部 Task status | 處理 |
| --- | --- | --- |
| `pending`、`queued` | `QUEUED` | 進度約 10% |
| `processing`、`running` | `IN_PROGRESS` | 進度約 50% |
| `succeeded` | `SUCCESS` | 讀取 URL、usage、實際 resolution/duration |
| `failed`、`expired` | `FAILURE` | 讀取 `error.message` |
| 未知 | `IN_PROGRESS` | 保守等待下一輪 |

成功 URL 取值順序：`content.video_url`，再取 `outputs[0]`。`usage.total_tokens` 用於按 token 結算，`usage.completion_tokens` 用於審計。輪詢狀態轉移必須使用 CAS；只有贏得終態轉移的 worker 可以觸發結算或退款。

## 5. 計費與資金鏈

完整資金來源、預扣、結算、退款、超時與日誌欄位參見 [`video-task-billing-chain.md`](video-task-billing-chain.md)。本模型的差異如下。

### 5.1 Legacy ratio（目前預設）

除非精確模型配置 `tiered_expr`，否則走 Doubao adapter 的 legacy ratio：

```text
baseQuota    = 模型基礎 ratio/單價 × group ratio
submitQuota  = baseQuota × video_input × duration_estimate
```

`RelayTaskSubmit` 固定 `ForcePreConsume=true`，每次任務提交都先完整預扣；重試換頻道不得重複預扣。

#### 版本模型價格表

以下是上游單價（元/百萬 token），基準為 480p/720p 且無視頻輸入的 46：

| 輸出分辨率 | 無視頻輸入 | 有視頻輸入 | 相對 `video_input`（無視頻基準） |
| --- | ---: | ---: | ---: |
| 480p/720p | 46 | 28 | 1.000 / 0.6087 |
| 1080p | 51 | 31 | 1.1087 / 0.6739 |
| 4K | 26 | 16 | 0.5652 / 0.3478 |

`GetVideoInputRatio(model, resolution, hasVideo)` 會把 resolution 轉小寫；未配置組合回退 1.0。`hasVideo` 由 content 中是否存在 `video_url` 判定，並在提交成功時凍結到 `BillingContext.HasVideoInput`。

#### 時長預扣倍數

- 基準時長 5 秒：`duration_estimate=1`。
- 正數 N 秒：`duration_estimate=N/5`。
- 缺失、非法或 `-1`：使用基準倍數 1（不把 `-1` 變成負費用）。
- 先限制在 `MaxTaskDurationSeconds=3600`，再進行 quota 飽和轉換。

### 5.2 成功結算

Doubao 實作 `TaskPollingRatiosAdjuster`：

1. 移除只供預扣的 `duration_estimate`，因 `total_tokens` 已反映實際時長。
2. 以凍結的 `HasVideoInput` 和上游實際 resolution 重新計算 `video_input`。
3. 若 `total_tokens>0`，按凍結的模型 ratio、group ratio、OtherRatios 計算實際 quota：

```text
actualQuota = totalTokens
             × frozenModelRatio
             × frozenGroupRatio
             × product(frozenOtherRatios)
```

4. `actualQuota - Task.Quota > 0` 時補扣；小於 0 時退差額；同步 wallet/subscription 與 token quota，更新 `Task.Quota` 並寫 consume/refund 日誌。
5. 沒有可靠 token 用量時保留預扣，不猜測退款。

所有 float/decimal 到 quota 的轉換必須使用 `common.Quota*Checked`，發生 int32 飽和要記錄 `admin_info.quota_saturation`。任何非正、NaN、Inf 的 multiplier 必須在進入 `OtherRatios` 前拒絕。

### 5.3 失敗、過期與超時

- 提交階段失敗：退款本次 BillingSession 的完整預扣。
- 輪詢 `failed/expired`：以 `Task.Quota` 為基準，透過 claim/CAS 確保只退款一次，成功後將 quota 清零。
- 超時掃描：新任務標記失敗並全額退款；舊任務是否退款取決於 legacy cutoff，遷移時應明確定義 cutoff。
- 退款失敗不可直接清空 quota；需保留可重試狀態和審計記錄。

資金來源遵循使用者 `BillingPreference`：wallet、subscription、wallet-first 或 subscription-first。token quota 與錢包/訂閱必須同時預扣、補扣與退款。

### 5.4 `tiered_expr` 選項

若新系統希望以公式定價，應對**精確版本模型名**配置 `billing_mode=tiered_expr`：

- `ModelPriceHelperPerCall` 以 `billingexpr` 為唯一計費來源，跳過 legacy `EstimateBilling` 與價格表乘法。
- 快照保存 expression、version、命中 tier、group ratio、請求變數與原始 body。
- 結算時只替換凍結快照中的實際 resolution/duration/tokens，不讀取最新配置。
- expression 執行失敗或無 tier 命中時 fail-closed，保留預扣並告警。

前端預設公式目前只以 `doubao-seedance-2-0` 為 key；`doubao-seedance-2-0-260128` 不會自動繼承。若採用 tiered_expr，需複製並審核版本模型的 expression、版本號與單價，不能假定名稱前綴匹配。

## 6. 資產接口

路由：

- `POST /api/v3/open/CreateAsset`
- `POST /api/v3/open/GetAsset`
- `POST /v3/open/GetAsset`（舊路徑相容）

middleware 只從 body 讀 `model` 用於頻道分發，完整 body 透傳至 `{asset_base_url 或 baseURL}/v3/open/{CreateAsset|GetAsset}`。資產調用不計入視頻生成費用；CreateAsset 只寫獨立資產日誌。adapter 不會自動上傳或輪詢資產，客戶端應先創建/查詢資產，再按上游支持使用 `asset://<id>`，也可直接傳公開 URL。新系統需在 API 文檔明確兩種輸入的支持範圍與權限。

## 7. 前端模型廣場與定價展示

相關檔案：

- `web/src/features/pricing/lib/seedance-v3-api.ts`
- `web/src/features/pricing/components/model-details-api.tsx`
- `web/src/features/pricing/components/model-card.tsx`
- `web/src/features/pricing/components/model-details.tsx`
- `web/src/features/channels/lib/channel-type-config.ts`

現行展示行為：

1. `isSeedanceV3ModelName` / `isDoubaoSeedanceV3ModelName` 以正則識別版本模型。
2. 模型詳情 API tab 對 Seedance 顯示虛擬 endpoint：`seedance-v3`，`POST /api/v3/contents/generations/tasks`。
3. 提供 cURL、Python、TypeScript、JavaScript 範例；範例會帶入當前模型名。
4. 參數展示：`content`、duration（預設 5，`-1 或 4~15`）、`480p/720p/1080p/4K`、比例、音訊、水印，以及 Doubao 的 tools/service_tier/draft/frames/camera_fixed。
5. 模型卡支援按次/動態價格、Video 能力標籤、性能徽章和 Overview/Performance/API tabs。

### 7.1 必須修正的展示與篩選缺口

- 前端虛擬 endpoint 是 `seedance-v3`，但後端 `GetEndpointTypesByChannelType` 對 channel type 54 沒有專用分支，可能回退為 OpenAI endpoint。新系統應新增穩定的後端能力元資料（例如 `supported_endpoint_types:["seedance-v3"]`），不要只靠模型名正則。
- `channel-type-config.ts` 的 Byteplus type 81 清單目前漏掉 `doubao-seedance-2-0-260128` 和 `doubao-seedance-2-0-fast-260128`；遷移時補入，並確認 channel type 54 有明確的 DoubaoVideo 配置。
- 模型廣場應按「模型能力」分組，而不是只按供應商或名稱前綴：文字生視頻、圖片生視頻、視頻編輯、音訊生成、支援分辨率、時長範圍、資產輸入應由後端 metadata 提供。
- 定價展示需區分「按次」與「按 token/時長/分辨率動態計價」。legacy 價格表的 46/28/51/31/26/16 是上游相對單價，不應直接當作使用者 quota 顯示。

建議模型目錄最少返回：

```json
{
  "model_name": "doubao-seedance-2-0-260128",
  "vendor_name": "Doubao",
  "supported_endpoint_types": ["seedance-v3", "openai-video"],
  "input_modalities": ["text", "image", "video", "audio"],
  "output_modalities": ["video"],
  "capabilities": ["t2v", "i2v", "v2v", "audio_generation", "async_task"],
  "metadata": {
    "duration_range": {"min": 4, "max": 15, "sentinel": -1},
    "resolutions": ["480p", "720p", "1080p", "4k"],
    "ratios": ["21:9", "16:9", "4:3", "1:1", "3:4", "9:16", "adaptive"],
    "supports_assets": true,
    "billing_modes": ["legacy_ratio", "tiered_expr"]
  }
}
```

## 8. 頻道與配置遷移

新系統至少需要以下配置資料：

```text
channel.type                 = DoubaoVideo (54)
channel.base_url              = https://model.service-inference.ai（或實際代理地址）
channel.key                   = 上游 Bearer key
channel.models                = doubao-seedance-2-0-260128
channel.model_mapping         = 可選，公開模型 -> 上游模型
channel.other.seedance_v3_routes.task_create/task_get = 可選
channel.other.asset_base_url  = 可選
channel.other.video_request_format_by_model = 可選
pricing.model_ratio           = 必填，為版本模型配置基礎 ratio/單價
pricing.billing_mode          = legacy_ratio 或 tiered_expr
```

目前 `setting/ratio_setting/model_ratio.go` 對未版本化 Doubao 有預設 ratio 7，但沒有明確列出 `doubao-seedance-2-0-260128`。遷移時必須二選一：

1. 在資料庫/管理后台新增該版本的明確 model ratio；或
2. 在程式預設值中加入等價配置，並用配置覆蓋機制允許調整。

若模型沒有可用 ratio 且系統不允許 unset price，請求應在預扣前返回「模型未定價」，不能免費放行或以 0 quota 建立任務。

## 9. 遷移實作順序

1. **資料模型**：建立 Task、PrivateData、BillingContext、公開/上游 task ID、CAS 終態欄位。
2. **請求層**：實作三組視頻路由、Seedance body normalization、duration 上限與 metadata 保留。
3. **頻道 adapter**：實作建立/查詢 URL、Bearer header、payload overlay、回應狀態映射與結果 URL 代理。
4. **預扣**：接入 wallet/subscription/token quota，固定任務全額預扣並支援提交重試冪等。
5. **輪詢結算**：接入 success 補扣/退款、failure/expired/timeout 全額退款、CAS claim 與審計日誌。
6. **價格模式**：先上線 legacy ratio；如採 tiered_expr，再加入版本模型專用 expression 與快照。
7. **資產接口**：獨立接入 CreateAsset/GetAsset，不與視頻生成費用耦合。
8. **前端**：模型目錄 metadata、分組篩選、參數表、API 範例、動態價格說明與頻道表單。
9. **可觀測性**：記錄 request id、公開/上游 task id、預扣/實際/退款 quota、token、expression version 與 quota saturation。

## 10. 驗收與回歸矩陣

### 請求與上游

- 原生 endpoint、`/v1/video/generations`、`/v1/videos` 均能建立任務。
- text/image/video/audio/tools 等欄位透傳；模型映射後上游 model 正確。
- duration 缺失、5、-1、15、3600、超上限和非整數的行為符合契約。
- 自定義 task route 的 method、參數和 response mapping 生效。
- 公開 task ID 與上游 task ID 不相同且可正確輪詢。

### 計費

- 480p/720p、1080p、4K；有/無視頻輸入的六種價格組合。
- duration ratio 正確；`-1` 不造成負或零乘數。
- 提交失敗完整退款；換頻道重試只預扣一次。
- 成功有 token 時按實際 resolution/duration 結算，能補扣及退差額。
- failed、expired、超時只退款一次；退款失敗可重試且不遺失 Task.Quota。
- wallet、subscription、token quota 三者在預扣/補扣/退款後一致。
- quota 溢位、NaN、Inf、超大 unsigned 輸入被拒絕並留下審計標記。
- tiered_expr 版本模型使用凍結快照，expression 失敗時 fail-closed。

### 前端與資產

- 模型廣場能在 Video/Seedance 分組看到版本模型，endpoint filter 不依賴名稱猜測。
- 詳情頁 endpoint、參數、範例與實際路由一致；價格標示 legacy/tiered 模式。
- channel type 54/81 的模型清單包含版本模型。
- CreateAsset/GetAsset 不產生視頻扣費，資產 ID 可在後續任務中使用。

## 11. 目前已知差異與風險

1. 版本模型的後端 adapter 與價格表已存在，但 model ratio 預設配置未明確包含它；這是上線前的阻斷項。
2. 前端 `seedance-v3` 與後端 channel type 54 的 endpoint 推導可能不一致，會導致模型廣場篩選或 API tab 顯示錯誤。
3. Byteplus type 81 前端模型清單漏列兩個版本模型，管理員可能無法從頻道表單選取。
4. 批量輪詢的某些異常路徑若只把任務標記 FAILURE 而未呼叫退款，遷移時必須統一走退款服務並補對帳任務。
5. 上游 URL、原始 body、API key 屬敏感資料；Task.PrivateData 和日誌必須脫敏，對外只返回代理 URL 或已授權的結果。

