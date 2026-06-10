package controller

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	customMenuMaxTotal   = 20
	customMenuMaxEnabled = 10
	customMenuMaxNameLen = 5
)

type customMenuItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	RequireLogin string `json:"requireLogin"`
	VisibleTo    string `json:"visibleTo"`
	OpenMode     string `json:"openMode"`
	LayoutMode   string `json:"layoutMode"`
	Enabled      bool   `json:"enabled"`
}

type customMenuPagesConfig struct {
	Items []customMenuItem `json:"items"`
}

func isValidCustomMenuURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "vbscript:") {
		return false
	}
	if strings.HasPrefix(trimmed, "//") {
		return false
	}
	if strings.HasPrefix(trimmed, "/") {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	return parsed.Host != ""
}

func validateCustomMenuPages(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var cfg customMenuPagesConfig
	if err := common.UnmarshalJsonStr(trimmed, &cfg); err != nil {
		return errors.New("自定义菜单 JSON 解析失败: " + err.Error())
	}
	if len(cfg.Items) > customMenuMaxTotal {
		return fmt.Errorf("自定义菜单最多 %d 条", customMenuMaxTotal)
	}
	enabledCount := 0
	seenIDs := make(map[string]struct{}, len(cfg.Items))
	for idx, item := range cfg.Items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return fmt.Errorf("第 %d 条菜单缺少 id", idx+1)
		}
		if _, dup := seenIDs[id]; dup {
			return fmt.Errorf("菜单 id 重复: %s", id)
		}
		seenIDs[id] = struct{}{}

		name := strings.TrimSpace(item.Name)
		if name == "" {
			return fmt.Errorf("第 %d 条菜单的名称不能为空", idx+1)
		}
		if utf8.RuneCountInString(name) > customMenuMaxNameLen {
			return fmt.Errorf("第 %d 条菜单的名称不能超过 %d 个字符", idx+1, customMenuMaxNameLen)
		}

		if !isValidCustomMenuURL(item.URL) {
			return fmt.Errorf("第 %d 条菜单的 URL 非法,需为 http(s):// 或 / 开头的站内路径", idx+1)
		}

		// requireLogin is optional for backward compatibility — missing value means "yes" (legacy default).
		if item.RequireLogin != "" && item.RequireLogin != "yes" && item.RequireLogin != "no" {
			return fmt.Errorf("第 %d 条菜单的登录要求必须为 yes 或 no", idx+1)
		}

		// visibleTo only matters when login is required; when login is not required, item is public so visibleTo is ignored.
		// We still validate the value if present (form preserves the field even when hidden in UI).
		if item.RequireLogin != "no" {
			if item.VisibleTo != "user" && item.VisibleTo != "admin" {
				return fmt.Errorf("第 %d 条菜单的可见角色必须为 user 或 admin", idx+1)
			}
		} else if item.VisibleTo != "" && item.VisibleTo != "user" && item.VisibleTo != "admin" {
			return fmt.Errorf("第 %d 条菜单的可见角色必须为 user 或 admin", idx+1)
		}

		// openMode is optional for backward compatibility — missing value means "iframe" (legacy default).
		if item.OpenMode != "" && item.OpenMode != "iframe" && item.OpenMode != "newWindow" {
			return fmt.Errorf("第 %d 条菜单的跳转方式必须为 iframe 或 newWindow", idx+1)
		}

		// layoutMode is optional for backward compatibility — missing value means "sidebar" (legacy default).
		// Only meaningful when openMode is "iframe"; ignored for "newWindow" but still validated if present.
		if item.LayoutMode != "" && item.LayoutMode != "sidebar" && item.LayoutMode != "fullwidth" {
			return fmt.Errorf("第 %d 条菜单的布局方式必须为 sidebar 或 fullwidth", idx+1)
		}

		if item.Enabled {
			enabledCount++
		}
	}
	if enabledCount > customMenuMaxEnabled {
		return fmt.Errorf("自定义菜单同时启用不能超过 %d 条", customMenuMaxEnabled)
	}
	return nil
}
