package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// TokenGroupOverrideHeader 是外部请求用于本次调用指定令牌分组的 header 名。
// 命中后会覆盖 token 自身配置的 groups，进入原有的白名单校验 / 展开 / 分发流程。
const TokenGroupOverrideHeader = "X-Niu-Token-Group"

// ErrTokenGroupOverrideAutoNotAllowed 表示 auto 与具体分组同时出现。
// 单独使用 auto 等价于未传 header，混合使用则因语义冲突而拒绝。
var ErrTokenGroupOverrideAutoNotAllowed = errors.New(TokenGroupOverrideHeader + " does not accept 'auto' together with explicit groups")

var (
	ErrTokenGroupOverrideNoUsableGroup = errors.New(TokenGroupOverrideHeader + " does not contain any usable token group")
)

// TokenSupportsGroupOverride reports whether the token is associated with the
// user's group. Tokens directly associated with token groups ignore the header.
func TokenSupportsGroupOverride(tokenGroups []string, userGroup string) bool {
	for _, group := range tokenGroups {
		if group == userGroup {
			return true
		}
	}
	return false
}

// ReadTokenGroupOverride 读取 header X-Niu-Token-Group 指定的令牌分组列表。
//
// 返回值：
//   - groups: 拆分、去空、去重后的分组名列表，与 token.GetGroups() 输出形态一致。
//   - ok:     是否需要覆盖；未传、全空白或单独传 auto 时为 false。
//   - err:    auto 与具体分组混用时非 nil；调用方应以 400 拒绝。
//
// 本函数只做形态标准化，不做用户分组关联校验；后者由 auth 主流程完成。
func ReadTokenGroupOverride(c *gin.Context) ([]string, bool, error) {
	raw := strings.TrimSpace(c.GetHeader(TokenGroupOverrideHeader))
	if raw == "" {
		return nil, false, nil
	}
	if strings.EqualFold(raw, "auto") {
		return nil, false, nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.EqualFold(p, "auto") {
			return nil, false, ErrTokenGroupOverrideAutoNotAllowed
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, false, nil
	}
	return out, true, nil
}

// ResolveTokenGroupOverride applies a per-request group selection to tokens
// associated with the user's group. Unsupported entries are ignored so a
// request can still use the valid entries that remain.
func ResolveTokenGroupOverride(originalGroups, overrideGroups []string, userGroup string, usableGroups map[string]string) ([]string, error) {
	if !TokenSupportsGroupOverride(originalGroups, userGroup) {
		return originalGroups, nil
	}

	resolvedGroups := make([]string, 0, len(overrideGroups))
	for _, group := range overrideGroups {
		if group == userGroup {
			continue
		}
		if _, ok := usableGroups[group]; !ok {
			continue
		}
		resolvedGroups = append(resolvedGroups, group)
	}
	if len(resolvedGroups) == 0 {
		return nil, ErrTokenGroupOverrideNoUsableGroup
	}
	return resolvedGroups, nil
}
