package deployer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestL1ChainType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected L1ChainType
		valid    bool
	}{
		{
			name:     "ethereum is valid",
			input:    "ethereum",
			expected: L1ChainTypeEthereum,
			valid:    true,
		},
		{
			name:     "rsk is valid",
			input:    "rsk",
			expected: L1ChainTypeRSK,
			valid:    true,
		},
		{
			name:     "empty string is invalid",
			input:    "",
			expected: "",
			valid:    false,
		},
		{
			name:     "unknown type is invalid",
			input:    "arbitrum",
			expected: "",
			valid:    false,
		},
		{
			name:     "case sensitive - uppercase is invalid",
			input:    "ETHEREUM",
			expected: "",
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test ValidL1ChainType
			ct := L1ChainType(tt.input)
			require.Equal(t, tt.valid, ValidL1ChainType(ct))

			// Test ParseL1ChainType
			parsed, err := ParseL1ChainType(tt.input)
			if tt.valid {
				require.NoError(t, err)
				require.Equal(t, tt.expected, parsed)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestL1ChainTypeConstants(t *testing.T) {
	// Verify constants have expected values
	require.Equal(t, L1ChainType("ethereum"), L1ChainTypeEthereum)
	require.Equal(t, L1ChainType("rsk"), L1ChainTypeRSK)
	
	// Verify flag name
	require.Equal(t, "l1-chain-type", L1ChainTypeFlagName)
}
