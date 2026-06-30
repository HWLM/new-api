package operation_setting

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/setting/config"
)

type SubChannelSetting struct {
	Tags []string `json:"tags"`
}

var subChannelSetting = SubChannelSetting{
	Tags: []string{"subapi"},
}

var subChannelMu sync.RWMutex

func init() {
	config.GlobalConfig.Register("sub_channel_setting", &subChannelSetting)
}

func GetSubChannelSetting() *SubChannelSetting {
	return &subChannelSetting
}

// GetSubChannelTags 返回去重、剔空白后的 sub 渠道 tag 列表副本，可安全用于 SQL IN 查询。
func GetSubChannelTags() []string {
	subChannelMu.RLock()
	defer subChannelMu.RUnlock()
	if len(subChannelSetting.Tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(subChannelSetting.Tags))
	result := make([]string, 0, len(subChannelSetting.Tags))
	for _, t := range subChannelSetting.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		result = append(result, t)
	}
	return result
}
