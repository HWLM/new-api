package dto

const SeedanceV3ModelName = "dreamina-seedance-2-0-hc"

const (
	SeedanceV3DoubaoFilterOffModel = "doubao-seedance-2-0-filter-off"
	SeedanceV3DoubaoModel          = "doubao-seedance-2-0"
	SeedanceV3DoubaoFastModel      = "doubao-seedance-2-0-fast"
)

var SeedanceV3UnifiedModels = []string{
	SeedanceV3ModelName,
	SeedanceV3DoubaoFilterOffModel,
	SeedanceV3DoubaoModel,
	SeedanceV3DoubaoFastModel,
}

func IsSeedanceV3UnifiedModel(model string) bool {
	for _, supported := range SeedanceV3UnifiedModels {
		if model == supported {
			return true
		}
	}
	return false
}

type SeedanceV3MediaURL struct {
	URL string `json:"url"`
}

type SeedanceV3ContentItem struct {
	Type     string              `json:"type"`
	Text     string              `json:"text,omitempty"`
	ImageURL *SeedanceV3MediaURL `json:"image_url,omitempty"`
	VideoURL *SeedanceV3MediaURL `json:"video_url,omitempty"`
	Role     string              `json:"role,omitempty"`
}

type SeedanceV3VideoRequest struct {
	Model         string                  `json:"model"`
	Content       []SeedanceV3ContentItem `json:"content"`
	Duration      *int                    `json:"duration,omitempty"`
	Resolution    string                  `json:"resolution,omitempty"`
	Ratio         string                  `json:"ratio,omitempty"`
	GenerateAudio *bool                   `json:"generate_audio,omitempty"`
	Watermark     *bool                   `json:"watermark,omitempty"`
}

type SeedanceV3AssetRequest struct {
	URL       string `json:"URL"`
	Name      string `json:"Name"`
	AssetType string `json:"AssetType"`
}

type SeedanceV3BaseResponse struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type SeedanceV3AssetData struct {
	ID         string                  `json:"Id"`
	Status     string                  `json:"Status,omitempty"`
	AssetType  string                  `json:"AssetType,omitempty"`
	Name       string                  `json:"Name,omitempty"`
	URL        string                  `json:"URL,omitempty"`
	GroupID    string                  `json:"GroupId,omitempty"`
	CreateTime string                  `json:"CreateTime,omitempty"`
	UpdateTime string                  `json:"UpdateTime,omitempty"`
	BaseResp   *SeedanceV3BaseResponse `json:"base_resp"`
}

type SeedanceV3AssetResponse struct {
	Success bool                `json:"success"`
	Data    SeedanceV3AssetData `json:"data"`
}

type SeedanceV3UnifiedCreateAssetRequest struct {
	Model     string `json:"model"`
	URL       string `json:"url"`
	Name      string `json:"name"`
	AssetType string `json:"AssetType"`
}

type SeedanceV3UnifiedGetAssetRequest struct {
	Model string `json:"model"`
	ID    string `json:"Id"`
}

type SeedanceV3Usage struct {
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type SeedanceV3Error struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type SeedanceV3VideoTask struct {
	ID              string           `json:"id"`
	Status          string           `json:"status"`
	Model           string           `json:"model"`
	DurationSeconds int              `json:"duration_seconds,omitempty"`
	Outputs         []string         `json:"outputs"`
	Error           *SeedanceV3Error `json:"error"`
	CreatedAt       string           `json:"created_at,omitempty"`
	CompletedAt     string           `json:"completed_at,omitempty"`
	Usage           *SeedanceV3Usage `json:"usage,omitempty"`
	LastFrameURL    string           `json:"last_frame_url,omitempty"`
}

type SeedanceV3VideoTaskResponse struct {
	Task SeedanceV3VideoTask `json:"task"`
}

type SeedanceV3PublicContent struct {
	VideoURL string `json:"video_url,omitempty"`
}

type SeedanceV3PublicTask struct {
	ID              string                  `json:"id"`
	Model           string                  `json:"model"`
	Status          string                  `json:"status"`
	Content         SeedanceV3PublicContent `json:"content"`
	DurationSeconds int                     `json:"duration_seconds,omitempty"`
	Outputs         []string                `json:"outputs,omitempty"`
	Usage           *SeedanceV3Usage        `json:"usage,omitempty"`
	Error           *SeedanceV3Error        `json:"error,omitempty"`
	CreatedAt       int64                   `json:"created_at,omitempty"`
	UpdatedAt       int64                   `json:"updated_at,omitempty"`
	CompletedAt     int64                   `json:"completed_at,omitempty"`
	LastFrameURL    string                  `json:"last_frame_url,omitempty"`
}

// WetokenV3 素材接口的强类型 DTO 曾用于 sdrealmax adapter 侧的自动上传：
// 当前逻辑不再由网关代做上传，客户端应通过 /v3/open/CreateAsset controller 自行调用。
// 那条 controller 用 map[string]any 透传，因此不再保留这里的 wetoken V3 DTO。
