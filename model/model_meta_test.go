package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultOfficialPriceBasis(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			name:     "empty model basis defaults to foreign pricing",
			value:    "",
			expected: OfficialPriceBasisConsumeUSDExchangeRate,
		},
		{
			name:     "explicit auto basis remains available for vendor inheritance",
			value:    OfficialPriceBasisAuto,
			expected: OfficialPriceBasisAuto,
		},
		{
			name:     "legacy domestic basis is normalized",
			value:    "1:1",
			expected: OfficialPriceBasisOneToOne,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, DefaultOfficialPriceBasis(test.value))
		})
	}
}
