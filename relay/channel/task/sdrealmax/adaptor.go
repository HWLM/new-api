package sdrealmax

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
	"github.com/QuantumNous/new-api/relay/channel/task/doubao"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL string
	apiKey  string
	// doubaoDelegate 处理 SD Real Max 渠道下**非** dreamina-seedance-2-0-hc 模型的请求。
	// 这类请求走上游原生的 /api/v3/contents/generations/tasks 路径。
	doubaoDelegate *doubao.TaskAdaptor
}

type doubaoUnifiedTaskResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Model    string `json:"model"`
	Duration int    `json:"duration,omitempty"`
	Content  struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Outputs []string             `json:"outputs,omitempty"`
	Usage   *dto.SeedanceV3Usage `json:"usage,omitempty"`
	Error   *dto.SeedanceV3Error `json:"error,omitempty"`
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	a.doubaoDelegate = &doubao.TaskAdaptor{}
	a.doubaoDelegate.Init(info)
}

// isSeedanceV3Model 判断当前请求应该走 SD Real Max 专属 SeedanceV3 分支（否则委托给 doubao）。
// 请求阶段 (Validate/BuildRequestBody/DoRequest) 优先看上下文里的 ContextKeySeedanceV3Request：
// middleware 只在 model == dreamina-seedance-2-0-hc 时写入该键。
// 无 context 时（响应处理、任务转换）回落到 model 名判断。
func isSeedanceV3Model(modelName string) bool {
	return modelName == dto.SeedanceV3ModelName
}

func (a *TaskAdaptor) shouldUseSeedanceV3(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c != nil {
		if _, ok := c.Get(string(constant.ContextKeySeedanceV3Request)); ok {
			return true
		}
	}
	modelName := ""
	if info != nil && info.ChannelMeta != nil {
		modelName = info.UpstreamModelName
		if modelName == "" {
			modelName = info.OriginModelName
		}
	} else if info != nil {
		modelName = info.OriginModelName
	}
	return isSeedanceV3Model(modelName)
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if !a.shouldUseSeedanceV3(c, info) {
		return a.doubaoDelegate.ValidateRequestAndSetAction(c, info)
	}
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate); taskErr != nil {
		return taskErr
	}

	request, err := a.requestFromContext(c, info)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if err := validateRequest(request); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	hasMedia := false
	for _, item := range request.Content {
		if item.Type == "image_url" || item.Type == "video_url" {
			hasMedia = true
			break
		}
	}
	if hasMedia {
		info.Action = constant.TaskActionGenerate
	} else {
		info.Action = constant.TaskActionTextGenerate
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if !a.shouldUseSeedanceV3(nil, info) {
		return a.doubaoDelegate.BuildRequestURL(info)
	}
	return fmt.Sprintf("%s/v1/video/generate", a.baseURL), nil
}

// hcPricingModelKey 是 hc（dreamina-seedance-2-0-hc）在 doubao 价格表里的映射键。
// hc 计费复用 doubao-seedance-2-0 的三档价格（46/28、51/31 基准），价格表本身
// 不为 hc 单独复制；这里给 GetVideoInputRatio 传入 "doubao-seedance-2-0" 而非
// dto.SeedanceV3ModelName，就能命中同一价格表条目。
const hcPricingModelKey = "doubao-seedance-2-0"

// EstimateBilling 分支：非 hc 委托给 doubao；hc 分支复用 doubao 的
// GetVideoInputRatio（以 hcPricingModelKey 为映射键），
// 同样返回 video_input（按 resolution/hasVideo 差价）+ duration_estimate（时长估算）。
//
// 探测到 video_url 输入时同步写 info.HasVideoInput，供后续冻结到 BillingContext。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if !a.shouldUseSeedanceV3(c, info) {
		return a.doubaoDelegate.EstimateBilling(c, info)
	}
	request, err := a.requestFromContext(c, info)
	if err != nil {
		return nil
	}
	hasVideo := containsVideoInput(request.Content)
	if hasVideo && info != nil {
		info.HasVideoInput = true
	}
	ratios := map[string]float64{}
	if r, ok := doubao.GetVideoInputRatio(hcPricingModelKey, request.Resolution, hasVideo); ok && r != 1.0 {
		ratios["video_input"] = r
	}
	if dr := hcDurationRatio(request.Duration); dr != 1.0 {
		ratios["duration_estimate"] = dr
	}
	if len(ratios) == 0 {
		return nil
	}
	return ratios
}

// containsVideoInput 扫 SeedanceV3 content 数组，判断有无 video_url 输入。
func containsVideoInput(content []dto.SeedanceV3ContentItem) bool {
	for _, item := range content {
		if item.Type == "video_url" || item.VideoURL != nil {
			return true
		}
	}
	return false
}

// hcDurationRatio 计算 hc 的 duration_estimate 倍率（相对 5s 基准）。
// 与 doubao.estimateDurationRatio 语义一致：nil / <=0 → 上限估算；否则 seconds/5。
func hcDurationRatio(duration *int) float64 {
	const baseSeconds = 5.0
	fallback := float64(relaycommon.MaxTaskDurationSeconds) / baseSeconds
	if duration == nil {
		return fallback
	}
	seconds := *duration
	if seconds <= 0 {
		return fallback
	}
	if seconds > relaycommon.MaxTaskDurationSeconds {
		seconds = relaycommon.MaxTaskDurationSeconds
	}
	return float64(seconds) / baseSeconds
}

// AdjustBillingRatiosOnComplete 分支：
//   - 非 hc → 委托给 doubao delegate（用上游返回的实际 resolution 覆盖）
//   - hc → 上游不返回 resolution，请求 resolution 就是最终值；只剥 duration_estimate
//     （tokens 已反映实际时长，剥掉避免重复计费）
func (a *TaskAdaptor) AdjustBillingRatiosOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) map[string]float64 {
	if !isSeedanceV3Model(task.Properties.OriginModelName) {
		return a.doubaoDelegate.AdjustBillingRatiosOnComplete(task, taskResult)
	}
	bc := task.PrivateData.BillingContext
	if bc == nil || bc.OtherRatios == nil {
		return nil
	}
	if _, has := bc.OtherRatios["duration_estimate"]; !has {
		// 没有 duration_estimate → 无需覆盖
		return nil
	}
	newRatios := make(map[string]float64, len(bc.OtherRatios))
	for k, v := range bc.OtherRatios {
		if k == "duration_estimate" {
			continue
		}
		newRatios[k] = v
	}
	return newRatios
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, request *http.Request, _ *relaycommon.RelayInfo) error {
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if !a.shouldUseSeedanceV3(c, info) {
		// 非 hc：直接委托给 doubao adapter，让它按 metadata overlay 重建上游 body。
		// 素材由客户端自己通过 /v3/open/CreateAsset 上传，网关不再代做上传。
		return a.doubaoDelegate.BuildRequestBody(c, info)
	}
	request, err := a.requestFromContext(c, info)
	if err != nil {
		return nil, err
	}
	applyRequestDefaults(&request)
	data, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// DoRequest 对 hc 和非 hc 走同一条透传路径：body 已经在 BuildRequestBody 里准备好，
// URL 由 BuildRequestURL 按模型分发。素材由客户端自行通过 /v3/open/CreateAsset 上传，
// 网关不再扫描 content 里的图片 URL 也不做替换。
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, response *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	if !a.shouldUseSeedanceV3(c, info) {
		if isSeedanceV3Request(c) {
			responseBody, err := io.ReadAll(response.Body)
			if err != nil {
				return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
			}
			_ = response.Body.Close()
			var upstream doubaoUnifiedTaskResponse
			if err := common.Unmarshal(responseBody, &upstream); err != nil {
				return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
			}
			if strings.TrimSpace(upstream.ID) == "" {
				return "", nil, service.TaskErrorWrapperLocal(errors.New("task id is empty"), "invalid_response", http.StatusBadGateway)
			}
			c.JSON(http.StatusOK, dto.SeedanceV3PublicTask{
				ID:        info.PublicTaskID,
				Model:     info.OriginModelName,
				Status:    "queued",
				Content:   dto.SeedanceV3PublicContent{},
				CreatedAt: time.Now().Unix(),
			})
			return upstream.ID, responseBody, nil
		}
		return a.doubaoDelegate.DoResponse(c, response, info)
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = response.Body.Close()

	var upstream dto.SeedanceV3VideoTaskResponse
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if strings.TrimSpace(upstream.Task.ID) == "" {
		return "", nil, service.TaskErrorWrapperLocal(errors.New("task id is empty"), "invalid_response", http.StatusBadGateway)
	}

	if isSeedanceV3Request(c) {
		c.JSON(http.StatusOK, dto.SeedanceV3PublicTask{
			ID:        info.PublicTaskID,
			Model:     info.OriginModelName,
			Status:    "queued",
			Content:   dto.SeedanceV3PublicContent{},
			CreatedAt: time.Now().Unix(),
		})
	} else {
		video := dto.NewOpenAIVideo()
		video.ID = info.PublicTaskID
		video.TaskID = info.PublicTaskID
		video.Model = info.OriginModelName
		video.CreatedAt = time.Now().Unix()
		c.JSON(http.StatusOK, video)
	}

	return upstream.Task.ID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	// task_polling 会在 body 里带 "model"（见 service/task_polling.go），据此选择端点
	if modelName, _ := body["model"].(string); modelName != "" && !isSeedanceV3Model(modelName) {
		return a.doubaoDelegate.FetchTask(baseURL, key, body, proxy)
	}
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid task_id")
	}

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/video/tasks/%s", baseURL, taskID), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(request)
}

func (a *TaskAdaptor) ParseTaskResult(responseBody []byte) (*relaycommon.TaskInfo, error) {
	// SeedanceV3 上游响应形如 {"task":{"id":...,"status":...}}，doubao 是 {"id":...,"status":...,"content":{...}}。
	// 优先按 SeedanceV3 shape 试解，task 字段缺失或 id 为空则委托给 doubao。
	var probe struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
		ID string `json:"id"`
	}
	if err := common.Unmarshal(responseBody, &probe); err == nil && probe.Task.ID != "" {
		return a.parseSeedanceV3Result(responseBody)
	}
	if probe.ID != "" {
		return a.doubaoDelegate.ParseTaskResult(responseBody)
	}
	// 无法识别：仍走 SeedanceV3 解析以便返回原有错误信息
	return a.parseSeedanceV3Result(responseBody)
}

func (a *TaskAdaptor) parseSeedanceV3Result(responseBody []byte) (*relaycommon.TaskInfo, error) {
	var response dto.SeedanceV3VideoTaskResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	result := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(response.Task.Status) {
	case "pending", "queued":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "processing", "running":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "completed", "succeeded":
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		if len(response.Task.Outputs) > 0 {
			result.Url = response.Task.Outputs[0]
		}
		if response.Task.Usage != nil {
			result.CompletionTokens = response.Task.Usage.CompletionTokens
			result.TotalTokens = response.Task.Usage.TotalTokens
			if result.TotalTokens <= 0 {
				result.TotalTokens = result.CompletionTokens
			}
		}
	case "failed":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		if response.Task.Error != nil {
			result.Code = 1
			result.Reason = response.Task.Error.Message
		}
	default:
		return nil, fmt.Errorf("unknown task status: %s", response.Task.Status)
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	if !isSeedanceV3Model(task.Properties.OriginModelName) {
		return a.doubaoDelegate.ConvertToOpenAIVideo(task)
	}
	publicTask, err := a.convertToPublicTask(task)
	if err != nil {
		return nil, err
	}

	video := task.ToOpenAIVideo()
	video.SetMetadata("url", publicTask.Content.VideoURL)
	video.Model = task.Properties.OriginModelName
	if publicTask.Error != nil {
		video.Error = &dto.OpenAIVideoError{
			Code:    publicTask.Error.Code,
			Message: publicTask.Error.Message,
		}
	}
	return common.Marshal(video)
}

func (a *TaskAdaptor) ConvertToSeedanceV3Video(task *model.Task) ([]byte, error) {
	if !isSeedanceV3Model(task.Properties.OriginModelName) {
		var upstream doubaoUnifiedTaskResponse
		if len(task.Data) > 0 {
			if err := common.Unmarshal(task.Data, &upstream); err != nil {
				return nil, errors.Wrap(err, "unmarshal doubao task data failed")
			}
		}

		result := &dto.SeedanceV3PublicTask{
			ID:              task.TaskID,
			Model:           task.Properties.OriginModelName,
			Status:          publicStatus(task.Status),
			Content:         dto.SeedanceV3PublicContent{VideoURL: task.GetResultURL()},
			DurationSeconds: upstream.Duration,
			Outputs:         upstream.Outputs,
			Usage:           upstream.Usage,
			CreatedAt:       task.CreatedAt,
			UpdatedAt:       task.UpdatedAt,
		}
		if result.Content.VideoURL == "" {
			result.Content.VideoURL = upstream.Content.VideoURL
		}
		if len(result.Outputs) == 0 && result.Content.VideoURL != "" {
			result.Outputs = []string{result.Content.VideoURL}
		}
		if strings.EqualFold(upstream.Status, "expired") {
			result.Status = "expired"
		}
		if task.Status == model.TaskStatusFailure {
			result.Error = upstream.Error
			if result.Error == nil {
				result.Error = &dto.SeedanceV3Error{Message: task.FailReason}
			}
		}
		return common.Marshal(result)
	}
	publicTask, err := a.convertToPublicTask(task)
	if err != nil {
		return nil, err
	}
	return common.Marshal(publicTask)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) requestFromContext(c *gin.Context, info *relaycommon.RelayInfo) (dto.SeedanceV3VideoRequest, error) {
	if value, ok := c.Get(string(constant.ContextKeySeedanceV3Request)); ok {
		request, ok := value.(dto.SeedanceV3VideoRequest)
		if !ok {
			return dto.SeedanceV3VideoRequest{}, errors.New("invalid Seedance V3 request in context")
		}
		request.Model = info.UpstreamModelName
		if request.Model == "" {
			request.Model = info.OriginModelName
		}
		return request, nil
	}

	taskRequest, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return dto.SeedanceV3VideoRequest{}, err
	}
	request := dto.SeedanceV3VideoRequest{
		Model: info.UpstreamModelName,
		Content: []dto.SeedanceV3ContentItem{{
			Type: "text",
			Text: taskRequest.Prompt,
		}},
	}
	if request.Model == "" {
		request.Model = taskRequest.Model
	}
	for _, image := range taskRequest.Images {
		request.Content = append(request.Content, dto.SeedanceV3ContentItem{
			Type:     "image_url",
			ImageURL: &dto.SeedanceV3MediaURL{URL: image},
			Role:     "reference_image",
		})
	}
	if err := taskcommon.UnmarshalMetadata(taskRequest.Metadata, &request); err != nil {
		return dto.SeedanceV3VideoRequest{}, err
	}
	if taskRequest.Duration != 0 {
		request.Duration = common.GetPointer(taskRequest.Duration)
	} else if taskRequest.Seconds != "" {
		if seconds, err := strconv.Atoi(taskRequest.Seconds); err == nil {
			request.Duration = common.GetPointer(seconds)
		}
	}
	return request, nil
}

func (a *TaskAdaptor) convertToPublicTask(task *model.Task) (*dto.SeedanceV3PublicTask, error) {
	var upstream dto.SeedanceV3VideoTaskResponse
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &upstream); err != nil {
			return nil, errors.Wrap(err, "unmarshal SD Real Max task data failed")
		}
	}

	result := &dto.SeedanceV3PublicTask{
		ID:              task.TaskID,
		Model:           task.Properties.OriginModelName,
		Status:          publicStatus(task.Status),
		Content:         dto.SeedanceV3PublicContent{VideoURL: task.GetResultURL()},
		DurationSeconds: upstream.Task.DurationSeconds,
		Outputs:         upstream.Task.Outputs,
		Usage:           upstream.Task.Usage,
		CreatedAt:       task.CreatedAt,
		UpdatedAt:       task.UpdatedAt,
		LastFrameURL:    upstream.Task.LastFrameURL,
	}
	if len(result.Outputs) == 0 && result.Content.VideoURL != "" {
		result.Outputs = []string{result.Content.VideoURL}
	}
	if task.Status == model.TaskStatusFailure {
		result.Error = upstream.Task.Error
		if result.Error == nil {
			result.Error = &dto.SeedanceV3Error{Message: task.FailReason}
		}
	}
	return result, nil
}

func validateRequest(request dto.SeedanceV3VideoRequest) error {
	if strings.TrimSpace(request.Model) == "" {
		return errors.New("model is required")
	}
	if len(request.Content) == 0 {
		return errors.New("content is required")
	}
	if request.Duration != nil && (*request.Duration < 4 || *request.Duration > 15) {
		return errors.New("duration must be between 4 and 15 seconds")
	}
	if request.Resolution != "" && !common.StringsContains([]string{"480p", "720p", "1080p"}, strings.ToLower(request.Resolution)) {
		return errors.New("resolution must be one of 480p, 720p, or 1080p")
	}
	if request.Ratio != "" && !common.StringsContains([]string{"16:9", "9:16", "3:4", "1:1", "4:3"}, request.Ratio) {
		return errors.New("ratio is invalid")
	}

	hasText := false
	for _, item := range request.Content {
		switch item.Type {
		case "text":
			hasText = hasText || strings.TrimSpace(item.Text) != ""
		case "image_url":
			if item.ImageURL == nil || strings.TrimSpace(item.ImageURL.URL) == "" {
				return errors.New("image_url.url is required")
			}
			if err := validateImageURL(item.ImageURL.URL); err != nil {
				return err
			}
		case "video_url":
			if item.VideoURL == nil || (!strings.HasPrefix(item.VideoURL.URL, "https://") && !strings.HasPrefix(item.VideoURL.URL, "http://")) {
				return errors.New("video_url.url must be a public HTTP URL")
			}
		default:
			return fmt.Errorf("unsupported content type: %s", item.Type)
		}
	}
	if !hasText {
		return errors.New("content must include a non-empty text item")
	}
	return nil
}

func validateImageURL(url string) error {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return nil
	}
	if strings.HasPrefix(url, "asset://") && strings.TrimSpace(strings.TrimPrefix(url, "asset://")) != "" {
		return nil
	}
	return errors.New("image_url.url must be a public HTTP URL or a non-empty asset URL")
}

func applyRequestDefaults(request *dto.SeedanceV3VideoRequest) {
	if request.Duration == nil {
		request.Duration = common.GetPointer(5)
	}
	if request.Resolution == "" {
		request.Resolution = "480p"
	} else {
		request.Resolution = strings.ToLower(request.Resolution)
	}
	if request.Ratio == "" {
		request.Ratio = "16:9"
	}
	if request.GenerateAudio == nil {
		request.GenerateAudio = common.GetPointer(false)
	}
	if request.Watermark == nil {
		request.Watermark = common.GetPointer(false)
	}
}

func publicStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusInProgress:
		return "running"
	default:
		return "queued"
	}
}

func isSeedanceV3Request(c *gin.Context) bool {
	return strings.HasPrefix(c.Request.URL.Path, "/api/v3/contents/generations/tasks")
}
