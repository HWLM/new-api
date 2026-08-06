package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// TokenGroupOverrideHeader 是外部请求用于本次调用指定令牌分组的 header 名。
// 命中后会覆盖 token 自身配置的 groups，进入原有的白名单校验 / 展开 / 分发流程。
const TokenGroupOverrideHeader = "X-Niu-Token-Group"

// ErrTokenGroupOverrideAutoNotAllowed 表示 header 中使用了不被允许的 "auto"。
// auto 展开语义只允许由 token 自身配置（GetGroups）触发，避免外部请求绕过分组固化契约。
var ErrTokenGroupOverrideAutoNotAllowed = errors.New(TokenGroupOverrideHeader + " does not accept 'auto'")

// ReadTokenGroupOverride 读取 header X-Niu-Token-Group 指定的令牌分组列表。
//
// 返回值：
//   - groups: 拆分、去空、去重后的分组名列表，与 token.GetGroups() 输出形态一致。
//   - ok:     header 是否命中（存在且拆分后至少 1 项）。
//   - err:    仅在传入 "auto"（大小写不敏感）时非 nil；调用方应以 400 拒绝。
//
// 本函数只做形态标准化，不做用户白名单校验；后者由 auth 主流程复用
// service.GetUserUsableGroups 完成，避免重复实现。
func ReadTokenGroupOverride(c *gin.Context) ([]string, bool, error) {
	raw := strings.TrimSpace(c.GetHeader(TokenGroupOverrideHeader))
	if raw == "" {
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
