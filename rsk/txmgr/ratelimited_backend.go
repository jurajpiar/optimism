package txmgr

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum-optimism/optimism/op-service/txmgr"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/time/rate"
)

// gasPriceSuggester mirrors the unexported optional interface from
// op-service/txmgr/estimator.go. Anything implementing SuggestGasPrice
// satisfies it via Go's structural typing — the name is intentionally
// re-declared here so the type assertion against r.inner can find a
// pre-EIP-1559 gas-price source on the wrapped backend.
type gasPriceSuggester interface {
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
}

// rateLimitedBackend wraps a txmgr.ETHBackend with a token-bucket limiter so
// every RPC call waits for a token before proceeding. Used to throttle
// requests against slow L1 nodes (e.g. RSK).
//
// This wrapper lives in oprsk/ rather than upstream optimism so the upstream
// patch series stays minimal: the only upstream surface is the
// txmgr.CLIConfig.WrapBackend hook that lets us install this from
// oprsk/nodecfg without optimism needing to depend on golang.org/x/time/rate.
type rateLimitedBackend struct {
	inner   txmgr.ETHBackend
	limiter *rate.Limiter
}

// WrapRateLimit returns a function that, when used as the value of
// txmgr.CLIConfig.WrapBackend, installs a token-bucket rate-limited wrapper
// around the resolved L1 backend. rps is the sustained requests-per-second;
// burst is the bucket capacity (must be ≥ 1; defaults to 10).
//
// rps ≤ 0 disables wrapping (returns identity), so callers can build the
// wrapper unconditionally and let the rps value decide.
func WrapRateLimit(rps float64, burst int) func(txmgr.ETHBackend) txmgr.ETHBackend {
	if rps <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = 10
	}
	return func(inner txmgr.ETHBackend) txmgr.ETHBackend {
		return &rateLimitedBackend{
			inner:   inner,
			limiter: rate.NewLimiter(rate.Limit(rps), burst),
		}
	}
}

func (r *rateLimitedBackend) wait(ctx context.Context) error {
	return r.limiter.Wait(ctx)
}

func (r *rateLimitedBackend) BlockNumber(ctx context.Context) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.BlockNumber(ctx)
}

func (r *rateLimitedBackend) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.CallContract(ctx, msg, blockNumber)
}

func (r *rateLimitedBackend) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.TransactionReceipt(ctx, txHash)
}

func (r *rateLimitedBackend) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	if err := r.wait(ctx); err != nil {
		return err
	}
	return r.inner.SendTransaction(ctx, tx)
}

func (r *rateLimitedBackend) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.HeaderByNumber(ctx, number)
}

func (r *rateLimitedBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.SuggestGasTipCap(ctx)
}

// SuggestGasPrice delegates to the inner backend if it supports legacy gas
// pricing (eth_gasPrice). Without this, optimism's DefaultGasPriceEstimatorFn
// can't fall back to eth_gasPrice on pre-EIP-1559 L1s when wrapped.
func (r *rateLimitedBackend) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	gps, ok := r.inner.(gasPriceSuggester)
	if !ok {
		return nil, errors.New("oprsk/txmgr: wrapped L1 backend does not support SuggestGasPrice")
	}
	return gps.SuggestGasPrice(ctx)
}

func (r *rateLimitedBackend) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.BlobBaseFee(ctx)
}

func (r *rateLimitedBackend) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.NonceAt(ctx, account, blockNumber)
}

func (r *rateLimitedBackend) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.PendingNonceAt(ctx, account)
}

func (r *rateLimitedBackend) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	if err := r.wait(ctx); err != nil {
		return 0, err
	}
	return r.inner.EstimateGas(ctx, msg)
}

func (r *rateLimitedBackend) Close() {
	r.inner.Close()
}
