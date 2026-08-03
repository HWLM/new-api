# Seedance V3 统一视频 API

版本：2026-08-03

本文档描述 new-api 对外提供的 Seedance 2.0 统一接口。客户端只需要使用本站 API Key，网关会根据 `model` 选择对应的视频和素材上游。

OpenAPI 文档：[`openapi/relay.json`](./openapi/relay.json)

## 基础约定

```http
Authorization: Bearer <NEW_API_KEY>
Content-Type: application/json
```

Base URL 示例：

```text
https://<your-new-api-host>
```

内置支持模型示例（素材接口不做静态模型白名单校验，实际可用模型由渠道配置决定）：

| 模型 | 视频上游 | 素材处理 |
| --- | --- | --- |
| `dreamina-seedance-2-0-hc` | Byteplus `/v1/video/generate` | 客户端通过 `/v1/sd/assets` 管理素材 |
| `doubao-seedance-2-0-filter-off` | Doubao `/api/v3/contents/generations/tasks` | `contains_face=true` 时自动上传并等待 `Active` |
| `doubao-seedance-2-0` | Doubao `/api/v3/contents/generations/tasks` | `contains_face=true` 时自动上传并等待 `Active` |
| `doubao-seedance-2-0-fast` | Doubao `/api/v3/contents/generations/tasks` | `contains_face=true` 时自动上传并等待 `Active` |
| `doubao-seedance-2-0-260128` | Doubao `/api/v3/contents/generations/tasks` | `contains_face=true` 时自动上传并等待 `Active` |
| `doubao-seedance-2-0-fast-260128` | Doubao `/api/v3/contents/generations/tasks` | `contains_face=true` 时自动上传并等待 `Active` |

## 1. 创建素材

```http
POST /api/v3/open/CreateAsset
```

请求：

```json
{
  "model": "doubao-seedance-2-0-filter-off",
  "url": "https://example.com/first-frame.jpg",
  "name": "first-frame.jpg",
  "AssetType": "Image"
}
```

字段说明：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 必须使用支持的模型 |
| `url` | string | 是 | 公网可访问的素材 URL |
| `name` | string | 是 | 素材名称，建议包含扩展名 |
| `AssetType` | string | 否 | `Image`、`Video` 或 `Audio`，默认 `Image` |

成功响应：

```json
{
  "id": "asset-20260705003737-njxmg"
}
```

素材接口的 `model` 必须与视频任务使用的 `model` 一致。

## 2. 查询素材

```http
POST /api/v3/open/GetAsset
```

兼容路径 `POST /v3/open/GetAsset` 仍然可用，新接入应使用 `/api/v3/open/GetAsset`。

请求：

```json
{
  "model": "doubao-seedance-2-0-filter-off",
  "Id": "asset-20260705003737-njxmg"
}
```

字段说明：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 创建素材时使用的模型，必须与后续视频任务使用的模型一致 |
| `Id` | string | 是 | `CreateAsset` 返回的素材 ID；字段名区分大小写 |

成功响应：

```json
{
  "Id": "asset-20260705003737-njxmg",
  "Status": "Active",
  "AssetType": "Image",
  "Name": "first-frame.jpg",
  "URL": "https://example.com/first-frame.jpg",
  "GroupId": "",
  "CreateTime": "2026-07-04T12:15:34Z",
  "UpdateTime": "2026-07-04T12:15:36Z"
}
```

响应字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Id` | string | 素材 ID |
| `Status` | string | 素材处理状态，例如 `Processing` 或 `Active` |
| `AssetType` | string | 素材类型，例如 `Image`、`Video` 或 `Audio` |
| `Name` | string | 创建素材时指定的名称 |
| `URL` | string | 素材来源 URL |
| `GroupId` | string | 上游素材分组 ID；未分组时可能为空 |
| `CreateTime` | string | 素材创建时间，由上游返回 |
| `UpdateTime` | string | 素材最后更新时间，由上游返回 |
| `base_resp` | object | 部分上游返回的状态详情，包含 `status_code` 和 `status_msg` |

除 `Id` 外的响应字段可能因上游而省略。该接口只执行一次状态查询，不会在网关内等待状态变化。Doubao 素材创建后可能处于 `Processing`，客户端应继续轮询，直到状态变为 `Active` 后再通过 `asset://<Id>` 引用。查询素材不参与视频任务计费。

请求缺少 `model` 时由网关返回 `400`；缺少 `Id` 时，HC 模型返回 `400`，其他模型按上游校验结果返回。素材不存在时通常返回 `404`；上游鉴权、限流或服务异常会按实际状态返回。

## 3. 创建视频任务

```http
POST /api/v3/contents/generations/tasks
```

请求示例：

```json
{
  "model": "doubao-seedance-2-0-filter-off",
  "content": [
    {
      "type": "text",
      "text": "让画面中的人物转身并向镜头挥手"
    },
    {
      "type": "image_url",
      "role": "first_frame",
      "contains_face": true,
      "image_url": {
        "url": "https://example.com/first-frame.jpg"
      }
    }
  ],
  "duration": 5,
  "resolution": "720p",
  "ratio": "adaptive",
  "generate_audio": true,
  "watermark": false
}
```

顶层字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `model` | string | 支持模型列表中的模型之一 |
| `content` | array | `text`、`image_url`、`video_url`、`audio_url` |
| `duration` | integer | 通常为 4-15；Doubao 允许 `-1` 由模型决定 |
| `resolution` | string | `480p`、`720p`、`1080p`；Doubao 还支持 `4K` |
| `ratio` | string | `21:9`、`16:9`、`4:3`、`1:1`、`3:4`、`9:16`；Doubao 还支持 `adaptive` |
| `generate_audio` | boolean | 是否生成同步音频 |
| `watermark` | boolean | 是否添加水印 |
| `tools` | array | Doubao 工具，例如 `[{'type':'web_search'}]` |
| `service_tier` | string | Doubao 参数，透传上游 |
| `draft` | boolean | Doubao 参数，透传上游 |
| `frames` | integer | Doubao 参数，透传上游 |
| `camera_fixed` | boolean | Doubao 参数，透传上游 |

媒体项可使用公网 URL 或 `asset://<asset-id>`。Doubao 图片和音频也可以使用 Base64；HC 模型不支持 Base64。

`contains_face` 是网关扩展字段，不会发送给上游。当它为 `true` 且媒体 URL 不是 `asset://` 时，网关会创建素材、等待素材变为 `Active`，然后替换为 `asset://<id>`。

成功响应使用本站任务 ID：

```json
{
  "id": "task_2b7f10d34b6449ada4e53d76e31a5290",
  "model": "doubao-seedance-2-0-filter-off",
  "status": "queued",
  "content": {},
  "created_at": 1784217600
}
```

## 4. 查询视频任务

```http
GET /api/v3/contents/generations/tasks/{task_id}
```

成功响应：

```json
{
  "id": "task_2b7f10d34b6449ada4e53d76e31a5290",
  "model": "doubao-seedance-2-0-filter-off",
  "status": "succeeded",
  "content": {
    "video_url": "https://example.com/generated-video.mp4"
  },
  "duration_seconds": 5,
  "outputs": [
    "https://example.com/generated-video.mp4"
  ],
  "usage": {
    "completion_tokens": 40594,
    "total_tokens": 40594
  },
  "created_at": 1784217600,
  "updated_at": 1784218200
}
```

任务状态：

| 状态 | 含义 |
| --- | --- |
| `queued` | 排队中 |
| `running` | 生成中 |
| `succeeded` | 生成成功 |
| `failed` | 生成失败 |
| `expired` | Doubao 任务已过期 |

## 错误响应

```json
{
  "code": 400,
  "message": "错误描述"
}
```

常见状态码：`400` 参数错误，`401` API Key 无效，`404` 素材或任务不存在，`429` 上游限流，`5xx` 上游或网关异常。

## cURL 示例

```bash
curl -X POST "$BASE_URL/api/v3/open/GetAsset" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "doubao-seedance-2-0-filter-off",
    "Id": "asset-20260705003737-njxmg"
  }'

curl -X POST "$BASE_URL/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "dreamina-seedance-2-0-hc",
    "content": [
      {"type":"text","text":"让人物向镜头挥手"},
      {"type":"image_url","image_url":{"url":"https://example.com/input.jpg"}}
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9"
  }'

curl -X GET "$BASE_URL/api/v3/contents/generations/tasks/$TASK_ID" \
  -H "Authorization: Bearer $API_KEY"
```
