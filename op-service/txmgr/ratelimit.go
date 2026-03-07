package txmgr

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/time/rate"
)

// RateLimitedBackend wraps an ETHBackend with a token-bucket rate limiter.
// Every RPC call waits for a token before proceeding, preventing bursts
// from overwhelming slow L1 nodes (e.g. RSK).
type RateLimitedBackend struct {
	inner   ETHBackend
	limiter *rate.Limiter
}

// NewRateLimitedBackend creates a rate-limited wrapper around inner.
// rps is the sustained requests-per-second; burst is the maximum number
// of requests that can fire in a single instant (token bucket capacity).
func NewRateLimitedBackend(inner ETHBackend, rps float64, burst int) *RateLimitedBackend {
	return &RateLimitedBackend{
		inner:   inner,
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

func (r *RateLimitedBackend) wait(ctx context.Context) error {
	return r.limiter.Wait(ctx)
}

func (r *RateLimitedBackend) BlockNumber(ctx context.Context) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.BlockNumber(ctx)
}

func (r *RateLimitedBackend) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.CallContract(ctx, msg, blockNumber)
}

func (r *RateLimitedBackend) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.TransactionReceipt(ctx, txHash)
}

func (r *RateLimitedBackend) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	if err := r.wait(ctx); err != nil {
		return err
	}
	return r.inner.SendTransaction(ctx, tx)
}

func (r *RateLimitedBackend) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.HeaderByNumber(ctx, number)
}

func (r *RateLimitedBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.SuggestGasTipCap(ctx)
}

func (r *RateLimitedBackend) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.BlobBaseFee(ctx)
}

func (r *RateLimitedBackend) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.NonceAt(ctx, account, blockNumber)
}

func (r *RateLimitedBackend) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.PendingNonceAt(ctx, account)
}

func (r *RateLimitedBackend) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.EstimateGas(ctx, msg)
}

func (r *RateLimitedBackend) Close() {
	r.inner.Close()
}
