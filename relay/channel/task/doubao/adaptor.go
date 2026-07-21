package doubao

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/lo"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type,omitempty"`
	Text     string    `json:"text,omitempty"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

type requestPayload struct {
	Model                 string         `json:"model"`
	Content               []ContentItem  `json:"content,omitempty"`
	CallbackURL           string         `json:"callback_url,omitempty"`
	ReturnLastFrame       *dto.BoolValue `json:"return_last_frame,omitempty"`
	ServiceTier           string         `json:"service_tier,omitempty"`
	ExecutionExpiresAfter *dto.IntValue  `json:"execution_expires_after,omitempty"`
	GenerateAudio         *dto.BoolValue `json:"generate_audio,omitempty"`
	Draft                 *dto.BoolValue `json:"draft,omitempty"`
	Tools                 []struct {
		Type string `json:"type,omitempty"`
	} `json:"tools,omitempty"`
	SafetyIdentifier string         `json:"safety_identifier,omitempty"`
	Priority         *dto.IntValue  `json:"priority,omitempty"`
	Resolution       string         `json:"resolution,omitempty"`
	Ratio            string         `json:"ratio,omitempty"`
	Duration         *dto.IntValue  `json:"duration,omitempty"`
	Frames           *dto.IntValue  `json:"frames,omitempty"`
	Seed             *dto.IntValue  `json:"seed,omitempty"`
	CameraFixed      *dto.BoolValue `json:"camera_fixed,omitempty"`
	Watermark        *dto.BoolValue `json:"watermark,omitempty"`
}

type responsePayload struct {
	ID string `json:"id"` // task_id
}

type responseTask struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Status  string   `json:"status"`
	Outputs []string `json:"outputs,omitempty"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Seed            int    `json:"seed"`
	Resolution      string `json:"resolution"`
	Duration        int    `json:"duration"`
	Ratio           string `json:"ratio"`
	FramesPerSecond int    `json:"framespersecond"`
	ServiceTier     string `json:"service_tier"`
	Tools           []struct {
		Type string `json:"type"`
	} `json:"tools"`
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		ToolUsage        struct {
			WebSearch int `json:"web_search"`
		} `json:"tool_usage"`
	} `json:"usage"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

// ValidateRequestAndSetAction parses body, validates fields and sets default action.
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// Accept only POST /v1/video/generations as "generate" action.
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v3/contents/generations/tasks", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// EstimateBilling 根据请求 metadata 中的输出分辨率、是否包含视频输入、以及请求时长，
// 返回相对基准价的计费 OtherRatio。
//
// 两个 key 语义不同：
//   - "video_input": 上游价格档差（tokens×modelRatio 之上的乘数），结算时会被
//     AdjustBillingRatiosOnComplete 用上游返回的实际 resolution 覆盖。
//   - "duration_estimate": 仅预扣阶段的时长估算乘数（相对 5s 基准），保证长视频
//     预扣够扣。上游 total_tokens 已反映实际时长，AdjustBillingRatiosOnComplete
//     结算时会剥掉该 key，避免重复计费。
//
// 若探测到 video_url 输入，同时把 hasVideo 写到 info.HasVideoInput，
// 控制器会在提交成功时冻结到 BillingContext.HasVideoInput，供结算重算使用。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	hasVideo := hasVideoInMetadata(req.Metadata)
	if hasVideo && info != nil {
		info.HasVideoInput = true
	}
	resolution, _ := req.Metadata["resolution"].(string)
	ratios := map[string]float64{}
	if r, ok := GetVideoInputRatio(info.OriginModelName, resolution, hasVideo); ok && r != 1.0 {
		ratios["video_input"] = r
	}
	if dr := estimateDurationRatio(&req); dr != 1.0 {
		ratios["duration_estimate"] = dr
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

// estimateDurationRatio 计算 duration_estimate 倍率（相对 5s 基准）。
//   - 请求 5s → 1.0
//   - 请求 N s → N/5
//   - `-1` 或缺失 / 非法 → 用 MaxTaskDurationSeconds/5 作上限估算（宁可超扣，
//     结算会按真实 tokens 精算并退回）
//   - 剪到 [1, MaxTaskDurationSeconds] 后除 5
func estimateDurationRatio(req *relaycommon.TaskSubmitReq) float64 {
	const baseSeconds = 5.0
	fallback := float64(relaycommon.MaxTaskDurationSeconds) / baseSeconds
	if req == nil {
		return fallback
	}
	seconds, ok := resolveTaskDurationSeconds(req)
	if !ok || seconds <= 0 { // 包含 -1（模型自决）
		return fallback
	}
	if seconds > float64(relaycommon.MaxTaskDurationSeconds) {
		seconds = float64(relaycommon.MaxTaskDurationSeconds)
	}
	return seconds / baseSeconds
}

func resolveTaskDurationSeconds(req *relaycommon.TaskSubmitReq) (float64, bool) {
	if req == nil {
		return 0, false
	}
	if req.Duration > 0 {
		return float64(req.Duration), true
	}
	if req.Seconds != "" {
		if seconds, err := strconv.ParseFloat(strings.TrimSpace(req.Seconds), 64); err == nil {
			return seconds, true
		}
	}
	if req.Metadata == nil {
		return 0, false
	}
	raw, ok := req.Metadata["duration"]
	if !ok {
		return 0, false
	}
	return durationToFloat(raw)
}

// durationToFloat 把 metadata["duration"] 常见类型转为 float，无法转换返回 false。
func durationToFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// AdjustBillingRatiosOnComplete 用上游返回的实际参数覆盖 BillingContext.OtherRatios。
//   - video_input：用上游返回的实际 resolution 重查价格表；若上游未返回则保留原冻结值
//   - duration_estimate：必然剥掉（tokens 已反映实际时长，再乘会重复计费）
//   - 其它 key：原样保留
//
// 返回的 map 会被 settleTaskBillingOnComplete 全量替换到 BillingContext.OtherRatios。
func (a *TaskAdaptor) AdjustBillingRatiosOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) map[string]float64 {
	bc := task.PrivateData.BillingContext
	if bc == nil {
		return nil
	}
	newRatios := make(map[string]float64)
	for k, v := range bc.OtherRatios {
		if k == "video_input" || k == "duration_estimate" {
			continue
		}
		newRatios[k] = v
	}
	resolution := ""
	if taskResult != nil {
		resolution = taskResult.Resolution
	}
	if resolution == "" {
		// 上游没返回 → 保持原 video_input（若有）
		if orig, ok := bc.OtherRatios["video_input"]; ok && orig > 0 {
			newRatios["video_input"] = orig
		}
	} else if r, ok := GetVideoInputRatio(bc.OriginModelName, resolution, bc.HasVideoInput); ok && r != 1.0 {
		newRatios["video_input"] = r
	}
	return newRatios
}

// hasVideoInMetadata 直接检查 metadata 的 content 数组是否包含 video_url 条目，
// 避免构建完整的上游 requestPayload。
func hasVideoInMetadata(metadata map[string]interface{}) bool {
	if metadata == nil {
		return false
	}
	contentRaw, ok := metadata["content"]
	if !ok {
		return false
	}
	contentSlice, ok := contentRaw.([]interface{})
	if !ok {
		return false
	}
	for _, item := range contentSlice {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemMap["type"] == "video_url" {
			return true
		}
		if _, has := itemMap["video_url"]; has {
			return true
		}
	}
	return false
}

// BuildRequestBody converts request into Doubao specific format.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	body, err := a.convertToRequestPayload(&req)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}
	if info.IsModelMapped {
		body.Model = info.UpstreamModelName
	} else {
		info.UpstreamModelName = body.Model
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// Parse Doubao response
	var dResp responsePayload
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if dResp.ID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	if strings.HasPrefix(c.Request.URL.Path, "/api/v3/contents/generations/tasks") {
		c.JSON(http.StatusOK, dto.SeedanceV3PublicTask{
			ID:        info.PublicTaskID,
			Model:     info.OriginModelName,
			Status:    "queued",
			Content:   dto.SeedanceV3PublicContent{},
			CreatedAt: time.Now().Unix(),
		})
	} else {
		ov := dto.NewOpenAIVideo()
		ov.ID = info.PublicTaskID
		ov.TaskID = info.PublicTaskID
		ov.CreatedAt = time.Now().Unix()
		ov.Model = info.OriginModelName
		c.JSON(http.StatusOK, ov)
	}
	return dResp.ID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/api/v3/contents/generations/tasks/%s", baseUrl, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq) (*requestPayload, error) {
	r := requestPayload{
		Model:   req.Model,
		Content: []ContentItem{},
	}

	// Add images if present
	if req.HasImage() {
		for _, imgURL := range req.Images {
			r.Content = append(r.Content, ContentItem{
				Type: "image_url",
				ImageURL: &MediaURL{
					URL: imgURL,
				},
			})
		}
	}

	metadata := req.Metadata
	if err := taskcommon.UnmarshalMetadata(metadata, &r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if sec, ok := resolveTaskDurationSeconds(req); ok && sec > 0 {
		r.Duration = lo.ToPtr(dto.IntValue(sec))
	}

	r.Content = lo.Reject(r.Content, func(c ContentItem, _ int) bool { return c.Type == "text" })
	r.Content = append(r.Content, ContentItem{
		Type: "text",
		Text: req.Prompt,
	})

	return &r, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	// Map Doubao status to internal status
	switch resTask.Status {
	case "pending", "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = "10%"
	case "processing", "running":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "50%"
	case "succeeded":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = resTask.Content.VideoURL
		if taskResult.Url == "" && len(resTask.Outputs) > 0 {
			taskResult.Url = resTask.Outputs[0]
		}
		// 解析 usage 信息用于按倍率计费
		taskResult.CompletionTokens = resTask.Usage.CompletionTokens
		taskResult.TotalTokens = resTask.Usage.TotalTokens
		// 上游返回的实际 resolution/duration，供 AdjustBillingRatiosOnComplete 用来
		// 覆盖预扣阶段的估算倍率（例如 adaptive → 1080p、duration=-1 → 8s 等场景）
		taskResult.Resolution = resTask.Resolution
		taskResult.DurationSeconds = resTask.Duration
	case "failed", "expired":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		taskResult.Reason = resTask.Error.Message
	default:
		// Unknown status, treat as processing
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var dResp responseTask
	if err := common.Unmarshal(originTask.Data, &dResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal doubao task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.SetMetadata("url", dResp.Content.VideoURL)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	if dResp.Status == "failed" {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: dResp.Error.Message,
			Code:    dResp.Error.Code,
		}
	}

	return common.Marshal(openAIVideo)
}

func (a *TaskAdaptor) ConvertToSeedanceV3Video(originTask *model.Task) ([]byte, error) {
	var upstream responseTask
	if len(originTask.Data) > 0 {
		if err := common.Unmarshal(originTask.Data, &upstream); err != nil {
			return nil, errors.Wrap(err, "unmarshal doubao task data failed")
		}
	}

	status := "queued"
	switch originTask.Status {
	case model.TaskStatusInProgress:
		status = "running"
	case model.TaskStatusSuccess:
		status = "succeeded"
	case model.TaskStatusFailure:
		status = "failed"
	}
	if strings.EqualFold(upstream.Status, "expired") {
		status = "expired"
	}

	result := dto.SeedanceV3PublicTask{
		ID:              originTask.TaskID,
		Model:           originTask.Properties.OriginModelName,
		Status:          status,
		Content:         dto.SeedanceV3PublicContent{VideoURL: originTask.GetResultURL()},
		DurationSeconds: upstream.Duration,
		Outputs:         upstream.Outputs,
		CreatedAt:       originTask.CreatedAt,
		UpdatedAt:       originTask.UpdatedAt,
	}
	if result.Content.VideoURL == "" {
		result.Content.VideoURL = upstream.Content.VideoURL
	}
	if len(result.Outputs) == 0 && result.Content.VideoURL != "" {
		result.Outputs = []string{result.Content.VideoURL}
	}
	if upstream.Usage.CompletionTokens != 0 || upstream.Usage.TotalTokens != 0 {
		result.Usage = &dto.SeedanceV3Usage{
			CompletionTokens: upstream.Usage.CompletionTokens,
			TotalTokens:      upstream.Usage.TotalTokens,
		}
	}
	if originTask.Status == model.TaskStatusFailure {
		result.Error = &dto.SeedanceV3Error{
			Code:    upstream.Error.Code,
			Message: upstream.Error.Message,
		}
		if result.Error.Message == "" {
			result.Error.Message = originTask.FailReason
		}
	}
	return common.Marshal(result)
}
