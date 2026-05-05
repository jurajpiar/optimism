package l1source

import (
	"context"
	"fmt"

	"github.com/ethereum-optimism/optimism/rsk/trie"

	"github.com/ethereum/go-ethereum/core/types"
)

// BlockVerifierFn matches the shape of patch 0001's pluggable block verifier
// hook on op-service/sources EthClientConfig. It is given the header reported
// by the L1 RPC plus the decoded transactions and must return an error if the
// block is not internally consistent.
//
// The default Ethereum implementation recomputes both the transaction trie
// root and the block hash; on RSK only the transaction root can be checked
// without a full RSK header codec, and the block hash is trusted from the RPC.
type BlockVerifierFn func(ctx context.Context, header *types.Header, txs types.Transactions) error

// VerifyRSKBlock recomputes the RSK transaction trie root using oprsk/trie
// and compares it to the header.TxHash. Block-hash recomputation is
// intentionally skipped: RSK uses RSKIP92 / UMM encoding that is not
// reproducible from a go-ethereum types.Header alone, so we trust the RPC
// for the block hash itself.
func VerifyRSKBlock(_ context.Context, header *types.Header, txs types.Transactions) error {
	if header == nil {
		return fmt.Errorf("nil header")
	}
	computed := trie.CalculateTxRoot(txs)
	if header.TxHash != computed {
		return fmt.Errorf("tx root mismatch: header %s, computed %s", header.TxHash, computed)
	}
	if header.WithdrawalsHash != nil {
		// RSK is pre-merge and has no withdrawals (EIP-4895). A non-nil
		// withdrawals root would indicate an upstream contract bug.
		return fmt.Errorf("RSK L1 block has unexpected withdrawalsRoot: %s", header.WithdrawalsHash)
	}
	return nil
}

// ChainAwareBlockVerifier returns a BlockVerifierFn that uses VerifyRSKBlock
// for known RSK chain IDs and falls back to fallback otherwise. If fallback
// is nil and the chain is non-RSK, the verifier is a no-op (matching the
// "Ethereum default" behavior of having no extra hook).
func ChainAwareBlockVerifier(l1ChainID uint64, fallback BlockVerifierFn) BlockVerifierFn {
	if trie.IsRSKChain(l1ChainID) {
		return VerifyRSKBlock
	}
	if fallback == nil {
		return func(context.Context, *types.Header, types.Transactions) error { return nil }
	}
	return fallback
}

// HeaderVerifierFn matches the shape of patch 0001's pluggable header
// verifier hook on op-service/sources EthClientConfig. It receives the
// header reported by the L1 RPC and must return an error if the header is
// not internally consistent.
type HeaderVerifierFn func(ctx context.Context, header *types.Header) error

// VerifyRSKHeader is the RSK adapter for the header-only verification path
// (used by op-service/sources headerCall). It is a no-op: RSK uses the
// RSKIP92 / UMM-aware block-hash codec which is not reproducible from a
// go-ethereum types.Header alone, so the block hash reported by the RPC is
// trusted. Per-receipt and per-transaction integrity is enforced by
// VerifyRSKBlock and ValidateRSKReceipts on the data paths.
func VerifyRSKHeader(_ context.Context, _ *types.Header) error {
	return nil
}

// ChainAwareHeaderVerifier returns a HeaderVerifierFn that uses
// VerifyRSKHeader for RSK chain IDs and falls back to fallback otherwise.
func ChainAwareHeaderVerifier(l1ChainID uint64, fallback HeaderVerifierFn) HeaderVerifierFn {
	if trie.IsRSKChain(l1ChainID) {
		return VerifyRSKHeader
	}
	if fallback == nil {
		return func(context.Context, *types.Header) error { return nil }
	}
	return fallback
}
