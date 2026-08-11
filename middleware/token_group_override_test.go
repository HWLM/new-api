package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTokenGroupOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		header     string
		wantGroups []string
		wantOK     bool
		wantErr    error
	}{
		{name: "missing header"},
		{name: "auto uses the token default groups", header: " Auto "},
		{name: "normalizes and deduplicates groups", header: " token_1, token_2,token_1 ", wantGroups: []string{"token_1", "token_2"}, wantOK: true},
		{name: "rejects auto mixed with explicit groups", header: "token_1,Auto", wantErr: ErrTokenGroupOverrideAutoNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.Header.Set(TokenGroupOverrideHeader, test.header)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = request

			groups, ok, err := ReadTokenGroupOverride(ctx)

			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantOK, ok)
			assert.Equal(t, test.wantGroups, groups)
		})
	}
}

func TestResolveTokenGroupOverride(t *testing.T) {
	usableGroups := map[string]string{
		"token_1": "Token 1",
		"token_2": "Token 2",
	}

	tests := []struct {
		name           string
		originalGroups []string
		overrideGroups []string
		wantGroups     []string
		wantErr        error
	}{
		{
			name:           "keeps valid groups and filters unsupported groups",
			originalGroups: []string{"agent_A"},
			overrideGroups: []string{"token_1", "token_4"},
			wantGroups:     []string{"token_1"},
		},
		{
			name:           "filters the user group when a valid token group remains",
			originalGroups: []string{"agent_A"},
			overrideGroups: []string{"agent_A", "token_2"},
			wantGroups:     []string{"token_2"},
		},
		{
			name:           "rejects a user group without a usable token group",
			originalGroups: []string{"agent_A"},
			overrideGroups: []string{"agent_A"},
			wantErr:        ErrTokenGroupOverrideNoUsableGroup,
		},
		{
			name:           "rejects when every token group is unsupported",
			originalGroups: []string{"agent_A"},
			overrideGroups: []string{"token_3", "token_4"},
			wantErr:        ErrTokenGroupOverrideNoUsableGroup,
		},
		{
			name:           "ignores overrides for tokens directly associated with token groups",
			originalGroups: []string{"token_1", "token_2"},
			overrideGroups: []string{"token_4"},
			wantGroups:     []string{"token_1", "token_2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			groups, err := ResolveTokenGroupOverride(test.originalGroups, test.overrideGroups, "agent_A", usableGroups)

			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				assert.Nil(t, groups)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantGroups, groups)
		})
	}
}

func TestTokenSupportsGroupOverride(t *testing.T) {
	assert.True(t, TokenSupportsGroupOverride([]string{"agent_A"}, "agent_A"))
	assert.True(t, TokenSupportsGroupOverride([]string{"token_1", "agent_A"}, "agent_A"))
	assert.False(t, TokenSupportsGroupOverride([]string{"token_1", "token_2"}, "agent_A"))
	assert.False(t, TokenSupportsGroupOverride(nil, "agent_A"))
}
