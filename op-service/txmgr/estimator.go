package txmgr

import (
	"context"
	"math/big"
)

type GasPriceEstimatorFn func(ctx context.Context, backend ETHBackend) (*big.Int, *big.Int, *big.Int, error)

func DefaultGasPriceEstimatorFn(ctx context.Context, backend ETHBackend) (*big.Int, *big.Int, *big.Int, error) {
	tip, err := backend.SuggestGasPrice(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	head, err := backend.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	// Pre-London blocks don't have a base fee
	if head.BaseFee == nil {
		// Return gas price as tip, nil baseFee, nil blobFee for pre-London blocks
		return tip, nil, nil, nil
	}

	blobFee, err := backend.BlobBaseFee(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	return tip, head.BaseFee, blobFee, nil
}
