package broadcaster

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum-optimism/optimism/op-service/txmgr"
)

var (
	// rskBaseFeePadFactor = 50% as a divisor
	rskBaseFeePadFactor = big.NewInt(2)
	// rskTipMulFactor = 5 as a multiplier
	rskTipMulFactor = big.NewInt(5)
	// rskMaxTip is the maximum tip for RSK (lower than Ethereum due to lower fees)
	rskMaxTip = big.NewInt(5 * 1e9) // 5 gwei
	// rskMinTip is the minimum tip for RSK
	rskMinTip = big.NewInt(60000000) // 0.06 gwei - RSK minimum gas price
	// zeroBigInt is used for blob fees since RSK doesn't support them
	// We return zero instead of nil to avoid nil pointer dereference in txmgr
	zeroBigInt = big.NewInt(0)
)

// RSKDeployerGasPriceEstimator is a custom gas price estimator for use with op-deployer
// on RSK networks. It handles RSK's differences from Ethereum:
//   - Uses legacy gasPrice instead of EIP-1559 baseFee + priorityFee
//   - Returns zero for blob fees (RSK doesn't support EIP-4844)
//   - Uses RSK's minimumGasPrice (mapped to header.BaseFee) for base fee estimation
//
// It pads the base fee by 50% and multiplies the suggested tip by 5, capped at 5 gwei.
func RSKDeployerGasPriceEstimator(ctx context.Context, client txmgr.ETHBackend) (*big.Int, *big.Int, *big.Int, *big.Int, error) {
	// Get current block header for minimumGasPrice (mapped to BaseFee by gorsk client)
	chainHead, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get RSK block header: %w", err)
	}

	// Get suggested gas price from RSK node
	// In gorsk, SuggestGasTipCap returns eth_gasPrice result
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get RSK gas price: %w", err)
	}

	// Use minimumGasPrice as base fee (gorsk maps this to BaseFee)
	baseFee := chainHead.BaseFee
	if baseFee == nil {
		// Fallback to tip if no minimumGasPrice
		baseFee = new(big.Int).Set(tip)
	}

	// Pad base fee by 50%
	baseFeePad := new(big.Int).Div(baseFee, rskBaseFeePadFactor)
	paddedBaseFee := new(big.Int).Add(baseFee, baseFeePad)

	// Multiply tip by 5
	paddedTip := new(big.Int).Mul(tip, rskTipMulFactor)

	// Apply min/max bounds
	if paddedTip.Cmp(rskMinTip) < 0 {
		paddedTip.Set(rskMinTip)
	}
	if paddedTip.Cmp(rskMaxTip) > 0 {
		paddedTip.Set(rskMaxTip)
	}

	// Return zero for blob fees - RSK doesn't support EIP-4844 blob transactions
	// We use zero instead of nil to avoid nil pointer dereference in txmgr.SuggestGasPriceCaps
	// which compares blob fees even when they won't be used
	return paddedTip, paddedBaseFee, new(big.Int).Set(zeroBigInt), new(big.Int).Set(zeroBigInt), nil
}
