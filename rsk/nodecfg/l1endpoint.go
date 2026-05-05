package nodecfg

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-node/config"
	"github.com/ethereum-optimism/optimism/op-service/client"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	opmetrics "github.com/ethereum-optimism/optimism/op-service/metrics"
	"github.com/ethereum-optimism/optimism/op-service/sources"

	"github.com/ethereum-optimism/optimism/rsk/l1source"
	oprsktrie "github.com/ethereum-optimism/optimism/rsk/trie"
)

// RSKL1Endpoint wraps an existing L1EndpointSetup (typically the upstream
// *config.L1EndpointConfig produced by op-node's CLI flags) and post-processes
// the resulting *sources.L1ClientConfig to install the patch-0001 hooks with
// oprsk/l1source impls. The wrapped Setup is otherwise unchanged: same RPC
// dial, same caches, same provider kind.
//
// This is the single point of integration between optimism's L1 source
// machinery and the RSK adapter package; cmd/rsk-op-node and cmd/rollup-node
// (dev) both go through it.
type RSKL1Endpoint struct {
	// Inner is the upstream L1EndpointSetup (e.g. *config.L1EndpointConfig)
	// whose Setup we delegate to.
	Inner config.L1EndpointSetup
	// L1ChainID is the expected RSK chain id (30/31/33). It is used by the
	// chain-aware adapters to fall through to default Ethereum behavior on
	// non-RSK chains, which is useful for tests that swap in a non-RSK L1.
	L1ChainID uint64
}

var _ config.L1EndpointSetup = (*RSKL1Endpoint)(nil)

func (r *RSKL1Endpoint) Check() error {
	if r.Inner == nil {
		return errors.New("oprsk/nodecfg.RSKL1Endpoint: nil Inner")
	}
	return r.Inner.Check()
}

func (r *RSKL1Endpoint) Setup(ctx context.Context, lg log.Logger, defaultCacheSize int, metrics opmetrics.RPCMetricer) (client.RPC, *sources.L1ClientConfig, error) {
	rpcCl, l1Cfg, err := r.Inner.Setup(ctx, lg, defaultCacheSize, metrics)
	if err != nil {
		return nil, nil, err
	}
	InstallRSKHooks(l1Cfg, rpcCl, r.L1ChainID)
	return rpcCl, l1Cfg, nil
}

// InstallRSKHooks wires the four patch-0001 extension points on the given
// L1ClientConfig with their oprsk/l1source impls. It is idempotent: if any
// hook is already non-nil it is left untouched. The provided rpcCl is used
// only by the TxHashesFromBlock impl (eth_getBlockByHash, hashes-only).
//
// l1ChainID is used by the chain-aware adapters: on a non-RSK chain id the
// hooks fall through to standard-Ethereum behavior so this function is safe
// to call unconditionally.
func InstallRSKHooks(l1Cfg *sources.L1ClientConfig, rpcCl client.RPC, l1ChainID uint64) {
	if l1Cfg == nil {
		return
	}

	if l1Cfg.BlockVerifier == nil {
		l1Cfg.BlockVerifier = sources.BlockVerifierFn(l1source.ChainAwareBlockVerifier(l1ChainID, nil))
	}
	if l1Cfg.HeaderVerifier == nil {
		l1Cfg.HeaderVerifier = sources.HeaderVerifierFn(l1source.ChainAwareHeaderVerifier(l1ChainID, nil))
	}
	if l1Cfg.ReceiptsValidator == nil {
		l1Cfg.ReceiptsValidator = adaptReceiptsValidator(l1source.ChainAwareReceiptsValidator(l1ChainID, defaultEthReceiptsValidator))
	}
	if l1Cfg.TxHashesFromBlock == nil && oprsktrie.IsRSKChain(l1ChainID) {
		l1Cfg.TxHashesFromBlock = newRSKTxHashesFromBlock(rpcCl)
	}
}

// adaptReceiptsValidator converts an oprsk/l1source.ReceiptsValidatorFn (which
// uses BlockRef to avoid an op-service/eth import and []*types.Receipt) into
// the patch-0001 signature on EthClientConfig (which uses eth.BlockID and
// types.Receipts). Since types.Receipts is just []*types.Receipt under the
// hood, the conversion is a free type assertion.
func adaptReceiptsValidator(inner l1source.ReceiptsValidatorFn) sources.ReceiptsValidatorFn {
	return func(ctx context.Context, b eth.BlockID, receiptHash common.Hash, txHashes []common.Hash, receipts types.Receipts) error {
		return inner(ctx, l1source.BlockRef{Number: b.Number, Hash: b.Hash}, receiptHash, txHashes, []*types.Receipt(receipts))
	}
}

// defaultEthReceiptsValidator is the fallback used by the chain-aware
// validator on non-RSK chains. It mirrors op-service/sources.validateReceipts
// at the BlockRef level so the chain-aware wrapper never needs to know about
// optimism's internal validateReceipts symbol.
//
// On RSK the chain-aware validator never reaches here, so this is exercised
// only when InstallRSKHooks is called on a non-RSK chain id (tests, mixed
// devnets). It deliberately returns an error to surface mis-wiring fast in
// that case rather than silently skipping receipt validation; the upstream
// EthClient would otherwise still run its own default validateReceipts when
// the hook is left nil — which is precisely what we want on non-RSK L1.
func defaultEthReceiptsValidator(_ context.Context, _ l1source.BlockRef, _ common.Hash, _ []common.Hash, _ []*types.Receipt) error {
	return errors.New("oprsk/nodecfg: receipts validator invoked for non-RSK chain via RSK adapter; leave the hook nil instead")
}

// rskBlockTxHashes is the JSON shape returned by eth_getBlockByHash with the
// fullTxs=false argument: only transaction hashes are present.
type rskBlockTxHashes struct {
	Transactions []common.Hash `json:"transactions"`
}

// newRSKTxHashesFromBlock returns a TxHashesFromBlockFn that calls
// eth_getBlockByHash(blockHash, false) on the supplied RPC client and returns
// the canonical RSK-side transaction hashes. This bypasses the local
// go-ethereum tx.Hash() computation, which on RSK can disagree for system /
// REMASC transactions due to RSK's custom RLP encoding.
func newRSKTxHashesFromBlock(rpcCl client.RPC) sources.TxHashesFromBlockFn {
	return func(ctx context.Context, blockHash common.Hash) ([]common.Hash, error) {
		if rpcCl == nil {
			return nil, errors.New("oprsk/nodecfg: nil rpc client for TxHashesFromBlock")
		}
		var blk rskBlockTxHashes
		if err := rpcCl.CallContext(ctx, &blk, "eth_getBlockByHash", blockHash, false); err != nil {
			return nil, fmt.Errorf("eth_getBlockByHash hashes-only for %s: %w", blockHash, err)
		}
		return blk.Transactions, nil
	}
}

// chainIDFromHexutil is a small helper used by tests to construct chain ids
// from hex-encoded RPC results without importing op-service/sources.
func chainIDFromHexutil(b *hexutil.Big) uint64 {
	if b == nil {
		return 0
	}
	return b.ToInt().Uint64()
}
