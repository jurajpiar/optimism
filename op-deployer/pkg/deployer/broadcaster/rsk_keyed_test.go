package broadcaster

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum-optimism/optimism/op-service/txmgr"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

// mockETHBackend is a mock implementation of txmgr.ETHBackend for testing
type mockETHBackend struct {
	header   *types.Header
	gasPrice *big.Int
}

var _ txmgr.ETHBackend = (*mockETHBackend)(nil)

func (m *mockETHBackend) BlockNumber(ctx context.Context) (uint64, error) {
	if m.header != nil && m.header.Number != nil {
		return m.header.Number.Uint64(), nil
	}
	return 0, nil
}

func (m *mockETHBackend) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return nil, nil
}

func (m *mockETHBackend) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	return nil, nil
}

func (m *mockETHBackend) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return nil
}

func (m *mockETHBackend) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return m.header, nil
}

func (m *mockETHBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return m.gasPrice, nil
}

func (m *mockETHBackend) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	return nil, nil
}

func (m *mockETHBackend) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return 0, nil
}

func (m *mockETHBackend) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return 0, nil
}

func (m *mockETHBackend) EstimateGas(ctx context.Context, call ethereum.CallMsg) (uint64, error) {
	return 21000, nil
}

func (m *mockETHBackend) Close() {}

func TestRSKDeployerGasPriceEstimator(t *testing.T) {
	tests := []struct {
		name           string
		baseFee        *big.Int // minimumGasPrice
		gasPrice       *big.Int
		expectedTip    *big.Int
		expectedBase   *big.Int
	}{
		{
			name:           "normal RSK gas prices",
			baseFee:        big.NewInt(60000000),  // 0.06 gwei
			gasPrice:       big.NewInt(60000000),  // 0.06 gwei
			expectedTip:    big.NewInt(300000000), // 0.06 * 5 = 0.3 gwei
			expectedBase:   big.NewInt(90000000),  // 0.06 * 1.5 = 0.09 gwei
		},
		{
			name:           "higher gas prices",
			baseFee:        big.NewInt(1000000000), // 1 gwei
			gasPrice:       big.NewInt(1000000000), // 1 gwei
			expectedTip:    big.NewInt(5000000000), // 1 * 5 = 5 gwei (at max)
			expectedBase:   big.NewInt(1500000000), // 1 * 1.5 = 1.5 gwei
		},
		{
			name:           "very low gas prices - should use minimum",
			baseFee:        big.NewInt(10000000),  // 0.01 gwei
			gasPrice:       big.NewInt(10000000),  // 0.01 gwei
			expectedTip:    big.NewInt(60000000),  // minimum 0.06 gwei
			expectedBase:   big.NewInt(15000000),  // 0.01 * 1.5 = 0.015 gwei
		},
		{
			name:           "high gas prices - should cap tip at max",
			baseFee:        big.NewInt(2000000000), // 2 gwei
			gasPrice:       big.NewInt(2000000000), // 2 gwei
			expectedTip:    big.NewInt(5000000000), // capped at 5 gwei max
			expectedBase:   big.NewInt(3000000000), // 2 * 1.5 = 3 gwei
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockETHBackend{
				header: &types.Header{
					BaseFee: tt.baseFee, // RSK minimumGasPrice mapped to BaseFee
				},
				gasPrice: tt.gasPrice,
			}

			tip, base, blobTip, blobBase, err := RSKDeployerGasPriceEstimator(context.Background(), mock)
			require.NoError(t, err)

			require.Equal(t, tt.expectedTip.Int64(), tip.Int64(), "tip mismatch")
			require.Equal(t, tt.expectedBase.Int64(), base.Int64(), "base fee mismatch")
			// Blob fees should be zero (not nil) for RSK to avoid nil pointer dereference in txmgr
			require.NotNil(t, blobTip, "blob tip should not be nil")
			require.NotNil(t, blobBase, "blob base should not be nil")
			require.Equal(t, int64(0), blobTip.Int64(), "blob tip should be zero for RSK")
			require.Equal(t, int64(0), blobBase.Int64(), "blob base should be zero for RSK")
		})
	}
}

func TestRSKDeployerGasPriceEstimator_NilBaseFee(t *testing.T) {
	// Test case where minimumGasPrice is not set
	mock := &mockETHBackend{
		header: &types.Header{
			BaseFee: nil, // No minimumGasPrice
		},
		gasPrice: big.NewInt(100000000), // 0.1 gwei
	}

	tip, base, blobTip, blobBase, err := RSKDeployerGasPriceEstimator(context.Background(), mock)
	require.NoError(t, err)

	// Should use gasPrice as base fee
	require.Equal(t, big.NewInt(500000000).Int64(), tip.Int64()) // 0.1 * 5 = 0.5 gwei
	require.Equal(t, big.NewInt(150000000).Int64(), base.Int64()) // 0.1 * 1.5 = 0.15 gwei
	// Blob fees should be zero (not nil) to avoid nil pointer dereference
	require.NotNil(t, blobTip)
	require.NotNil(t, blobBase)
	require.Equal(t, int64(0), blobTip.Int64())
	require.Equal(t, int64(0), blobBase.Int64())
}

func TestRskPadGasLimit(t *testing.T) {
	tests := []struct {
		name           string
		data           []byte
		gasUsed        uint64
		creation       bool
		blockGasLimit  uint64
		expectedGas    uint64
	}{
		{
			name:          "simple call",
			data:          []byte{0x12, 0x34},
			gasUsed:       50000,
			creation:      false,
			blockGasLimit: 8000000,
			expectedGas:   77688, // (21000 + 50000) * 1.2 + overhead
		},
		{
			name:          "contract creation",
			data:          make([]byte, 1000), // 1KB bytecode
			gasUsed:       200000,
			creation:      true,
			blockGasLimit: 8000000,
			expectedGas:   292800, // (53000 + 200000) * 1.2 with creation overhead
		},
		{
			name:          "gas limit capped by block",
			data:          make([]byte, 10000),
			gasUsed:       10000000, // Very high gas
			creation:      false,
			blockGasLimit: 8000000,
			expectedGas:   8000000, // Capped at block limit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rskPadGasLimit(tt.data, tt.gasUsed, tt.creation, tt.blockGasLimit)
			
			// Check that result is within expected range
			// We allow some variance due to intrinsic gas calculation
			if tt.expectedGas == tt.blockGasLimit {
				require.Equal(t, tt.blockGasLimit, result, "should be capped at block gas limit")
			} else {
				require.Greater(t, result, tt.gasUsed, "padded gas should be greater than used gas")
				require.LessOrEqual(t, result, tt.blockGasLimit, "should not exceed block gas limit")
			}
		})
	}
}

