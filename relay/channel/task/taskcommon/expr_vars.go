package taskcommon

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/tidwall/gjson"
)

// ExtractTaskExprVars normalizes a TaskSubmitReq into the variable set
// consumed by v2 tiered_expr. The result type lives in the model package so
// TaskBillingContext can persist it without introducing a model → taskcommon
// dependency (helpers.go already imports model).
//
// rawBody carries the original JSON body bytes so we can fall back to top-level
// fields that live outside TaskSubmitReq (e.g. doubao/seedance put
// `resolution`, `content[]` at top level, kling puts `mode` at top level,
// sora puts `size` in a nested `input` object). Pass nil / empty when unknown.
//
// Rules (priority high→low):
//
//	Seconds:    req.Duration > req.Seconds > metadata.duration >
//	            metadata.durationSeconds > body.duration > body.seconds > 0
//	Resolution: metadata.resolution > NormalizeResolutionFromSize(req.Size)
//	            > body.resolution > NormalizeResolutionFromSize(body.size) > ""
//	Size:       req.Size, else body.size
//	HasVideo:   metadata.content[] with video_url, metadata.video_url,
//	            info.HasVideoInput, or body.content[] with video_url
//	HasImage:   req.Image / req.Images / metadata.image / body.image /
//	            body.content[] with image_url
//	N:          metadata.n > metadata.sampleCount > body.n > 1
//	Mode:       metadata.mode > req.Mode > body.mode > ""
//
// Seconds is saturated to [0, relaycommon.MaxTaskDurationSeconds] to stay within
// the project's billing-safety invariants; downstream quota conversion still
// applies its own int32 clamp as defense in depth.
func ExtractTaskExprVars(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo, rawBody []byte) model.TaskExprVars {
	body := gjson.ParseBytes(rawBody)
	vars := model.TaskExprVars{Size: firstNonEmpty(req.Size, body.Get("size").String())}
	metadata := req.Metadata

	vars.Seconds = extractSeconds(req, metadata, body)
	vars.Resolution = extractResolution(metadata, req.Size, body)
	if vars.Resolution == "" && info != nil && strings.Contains(info.OriginModelName, "seedance-2-0") {
		// Seedance defaults omitted/adaptive output resolution to 720p. Use the
		// same tier for pre-consume; settlement replaces it with the upstream's
		// actual resolution before recalculating the final charge.
		vars.Resolution = "720p"
	}
	vars.HasVideo = extractHasVideo(metadata, info, body)
	vars.HasImage = extractHasImage(req, metadata, body)
	vars.N = extractN(metadata, body)
	vars.Mode = extractMode(req, metadata, body)
	return vars
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func extractSeconds(req relaycommon.TaskSubmitReq, metadata map[string]interface{}, body gjson.Result) float64 {
	seconds := 0.0
	if req.Duration > 0 {
		seconds = float64(req.Duration)
	} else if req.Seconds != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(req.Seconds), 64); err == nil && v > 0 {
			seconds = v
		}
	}
	if seconds <= 0 && metadata != nil {
		if v, ok := metadataFloat(metadata["duration"]); ok && v > 0 {
			seconds = v
		} else if v, ok := metadataFloat(metadata["durationSeconds"]); ok && v > 0 {
			seconds = v
		}
	}
	if seconds <= 0 && body.Exists() {
		// Fallback: top-level `duration` / `seconds` in the raw body — many
		// adaptors (doubao/seedance, sora, wan) put these outside metadata.
		if v := body.Get("duration"); v.Exists() && v.Float() > 0 {
			seconds = v.Float()
		} else if v := body.Get("seconds"); v.Exists() && v.Float() > 0 {
			seconds = v.Float()
		}
	}
	if seconds < 0 {
		seconds = 0
	}
	if max := float64(relaycommon.MaxTaskDurationSeconds); seconds > max {
		seconds = max
	}
	return seconds
}

func extractResolution(metadata map[string]interface{}, size string, body gjson.Result) string {
	if metadata != nil {
		if s, ok := metadata["resolution"].(string); ok {
			if norm := NormalizeResolutionFromSize(s); norm != "" {
				return norm
			}
		}
	}
	if norm := NormalizeResolutionFromSize(size); norm != "" {
		return norm
	}
	if body.Exists() {
		// doubao/seedance put resolution at top level, wan/sora may use size.
		if s := body.Get("resolution").String(); s != "" {
			if norm := NormalizeResolutionFromSize(s); norm != "" {
				return norm
			}
		}
		if s := body.Get("size").String(); s != "" {
			if norm := NormalizeResolutionFromSize(s); norm != "" {
				return norm
			}
		}
	}
	return ""
}

func extractHasVideo(metadata map[string]interface{}, info *relaycommon.RelayInfo, body gjson.Result) bool {
	if info != nil && info.HasVideoInput {
		return true
	}
	if metadata != nil {
		if _, has := metadata["video_url"]; has {
			return true
		}
		if contentRaw, ok := metadata["content"]; ok {
			if contentSlice, ok := contentRaw.([]interface{}); ok {
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
			}
		}
	}
	if body.Exists() {
		if body.Get("video_url").Exists() {
			return true
		}
		if contentArr := body.Get("content"); contentArr.IsArray() {
			found := false
			contentArr.ForEach(func(_, item gjson.Result) bool {
				if item.Get("type").String() == "video_url" || item.Get("video_url").Exists() {
					found = true
					return false
				}
				return true
			})
			if found {
				return true
			}
		}
	}
	return false
}

func extractHasImage(req relaycommon.TaskSubmitReq, metadata map[string]interface{}, body gjson.Result) bool {
	if req.Image != "" || len(req.Images) > 0 {
		return true
	}
	if metadata != nil {
		for _, key := range []string{"image", "image_url", "img_url", "first_frame", "first_frame_url", "last_frame_url", "input_image"} {
			if v, ok := metadata[key]; ok && v != nil && v != "" {
				return true
			}
		}
		if contentRaw, ok := metadata["content"]; ok {
			if contentSlice, ok := contentRaw.([]interface{}); ok {
				for _, item := range contentSlice {
					itemMap, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					if itemMap["type"] == "image_url" {
						return true
					}
					if _, has := itemMap["image_url"]; has {
						return true
					}
				}
			}
		}
	}
	if body.Exists() {
		for _, key := range []string{"image", "image_url", "img_url", "first_frame", "first_frame_url", "last_frame_url", "input_image"} {
			if body.Get(key).Exists() && body.Get(key).String() != "" {
				return true
			}
		}
		if contentArr := body.Get("content"); contentArr.IsArray() {
			found := false
			contentArr.ForEach(func(_, item gjson.Result) bool {
				if item.Get("type").String() == "image_url" || item.Get("image_url").Exists() {
					found = true
					return false
				}
				return true
			})
			if found {
				return true
			}
		}
	}
	return false
}

func extractN(metadata map[string]interface{}, body gjson.Result) float64 {
	if metadata != nil {
		if v, ok := metadataFloat(metadata["n"]); ok && v > 0 {
			return v
		}
		if v, ok := metadataFloat(metadata["sampleCount"]); ok && v > 0 {
			return v
		}
	}
	if body.Exists() {
		if v := body.Get("n"); v.Exists() && v.Float() > 0 {
			return v.Float()
		}
		if v := body.Get("sampleCount"); v.Exists() && v.Float() > 0 {
			return v.Float()
		}
	}
	return 1
}

func extractMode(req relaycommon.TaskSubmitReq, metadata map[string]interface{}, body gjson.Result) string {
	if metadata != nil {
		if s, ok := metadata["mode"].(string); ok && s != "" {
			return s
		}
	}
	if req.Mode != "" {
		return req.Mode
	}
	if body.Exists() {
		if s := body.Get("mode").String(); s != "" {
			return s
		}
	}
	return ""
}

// NormalizeResolutionFromSize maps the various resolution/size representations
// used by task adaptors to a canonical short label.
//
//	"1920x1080" / "1080x1920" / "1080P" / "1080p"        → "1080p"
//	"1280x720"  / "720P" / "720p"                        → "720p"
//	"3840x2160" / "2160p" / "4K" / "4k"                  → "4k"
//	"640x480"   / "480P" / "480p"                        → "480p"
//	empty / unknown                                      → ""
func NormalizeResolutionFromSize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Canonical short labels pass through.
	switch s {
	case "480p", "720p", "1080p", "4k":
		return s
	case "2160p":
		return "4k"
	}
	// "WxH" or "W*H" — take the larger dimension.
	sep := ""
	if strings.Contains(s, "x") {
		sep = "x"
	} else if strings.Contains(s, "*") {
		sep = "*"
	}
	if sep != "" {
		parts := strings.SplitN(s, sep, 2)
		if len(parts) == 2 {
			w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
			h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
			if errW == nil && errH == nil {
				long := w
				if h > long {
					long = h
				}
				switch {
				case long >= 3840:
					return "4k"
				case long >= 1920:
					return "1080p"
				case long >= 1280:
					return "720p"
				case long >= 640:
					return "480p"
				}
			}
		}
	}
	return ""
}

func metadataFloat(v interface{}) (float64, bool) {
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
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
