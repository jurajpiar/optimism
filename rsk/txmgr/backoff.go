package txmgr

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum-optimism/optimism/op-service/txmgr"
)

// DefaultRSKL1BlockTime is the conservative fallback used when an L1 backend
// isn't available to measure live (or measurement fails). RSK targets ~30 s
// blocks on mainnet and testnet; regtest is configurable but typically faster.
const DefaultRSKL1BlockTime = 30 * time.Second

// RevertAwareBackoff returns a txmgr.Config.PrepareBackoff strategy that
// pauses for l1BlockTime when err is a *txmgr.ContractRevertError, and 2 s
// (matching upstream's prior fixed delay) for any other error.
//
// Contract reverts only resolve as new L1 blocks land — backing off at L1
// pace avoids hammering the node while waiting for state to update.
func RevertAwareBackoff(l1BlockTime time.Duration) func(int, error) time.Duration {
	if l1BlockTime <= 0 {
		l1BlockTime = DefaultRSKL1BlockTime
	}
	return func(_ int, err error) time.Duration {
		var revertErr *txmgr.ContractRevertError
		if errors.As(err, &revertErr) {
			return l1BlockTime
		}
		return 2 * time.Second
	}
}

// MeasureL1BlockTime samples the last 10 L1 blocks via HeaderByNumber to
// estimate the average inter-block interval. Returns DefaultRSKL1BlockTime if
// the chain is too young or any RPC call fails. timeout caps each header
// query; total wall time is bounded by 2*timeout.
//
// Intended to be called once at startup by callers wiring up
// txmgr.Config.PrepareBackoff = RevertAwareBackoff(measured).
func MeasureL1BlockTime(ctx context.Context, client txmgr.ETHBackend, timeout time.Duration) time.Duration {
	const window = 10
	ctx1, cancel1 := context.WithTimeout(ctx, timeout)
	defer cancel1()
	latest, err := client.HeaderByNumber(ctx1, nil)
	if err != nil || latest.Number.Uint64() < window {
		return DefaultRSKL1BlockTime
	}
	ctx2, cancel2 := context.WithTimeout(ctx, timeout)
	defer cancel2()
	older, err := client.HeaderByNumber(ctx2, new(big.Int).Sub(latest.Number, big.NewInt(window)))
	if err != nil {
		return DefaultRSKL1BlockTime
	}
	avg := (latest.Time - older.Time) / window
	if avg < 1 {
		avg = 1
	}
	return time.Duration(avg) * time.Second
}
