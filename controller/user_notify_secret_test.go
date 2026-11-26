package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// resolveNotifySecret 决定通知密钥这次要存什么。
//
// ⛔ 三条语义都是用户可见的：勾了「清除」必须真的清掉；提交了新值必须覆盖；
// 什么都没提交必须沿用已存值（否则「留空 = 保留」的界面承诺又会被打破）。
func TestResolveNotifySecret(t *testing.T) {
	cases := []struct {
		name      string
		current   string
		submitted string
		clear     bool
		want      string
	}{
		{
			name:      "clear wins over a submitted value",
			current:   "old-secret",
			submitted: "new-secret",
			clear:     true,
			want:      "",
		},
		{
			name:    "clear removes the stored secret",
			current: "old-secret",
			clear:   true,
			want:    "",
		},
		{
			name:      "a submitted value overwrites the stored one",
			current:   "old-secret",
			submitted: "new-secret",
			want:      "new-secret",
		},
		{
			name:    "nothing submitted keeps the stored secret",
			current: "old-secret",
			want:    "old-secret",
		},
		{
			name: "nothing anywhere stays empty",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveNotifySecret(tc.current, tc.submitted, tc.clear))
		})
	}
}
