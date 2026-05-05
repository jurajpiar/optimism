package l1source

import (
	"context"
	"fmt"

	"github.com/ethereum-optimism/optimism/rsk/trie"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// BlockRef identifies an L1 block by number and hash. It mirrors
// op-service/eth.BlockID so the receipts validator can be used without a
// hard import of eth.BlockID at this layer (avoids a cycle once oprsk is
// imported from optimism via patch 0001).
type BlockRef struct {
	Number uint64
	Hash   common.Hash
}

// ReceiptsValidatorFn matches the shape of patch 0001's pluggable receipt
// validator hook on op-service/sources EthClientConfig. It receives the
// block, the expected receipts trie root from the header, the transaction
// hashes the block declares, and the receipts as fetched from the RPC.
type ReceiptsValidatorFn func(ctx context.Context, block BlockRef, receiptHash common.Hash, txHashes []common.Hash, receipts []*types.Receipt) error

// ValidateRSKReceipts is the RSK-aware impl. It mirrors the inline behavior
// in optimism/op-service/sources/receipts.go (validateReceiptsWithChainID
// with isRSK=true): it skips the receipt-level BlockNumber/BlockHash and
// log BlockNumber/BlockHash checks (RSK RPC does not populate them), skips
// the cumulative-gas monotonicity check (RSK appends a per-block REMASC
// reward tx with gasUsed=0/cumGas=0), and computes the trie root via
// gorsk's Unitrie.
func ValidateRSKReceipts(_ context.Context, block BlockRef, receiptHash common.Hash, txHashes []common.Hash, receipts []*types.Receipt) error {
	if len(receipts) != len(txHashes) {
		return fmt.Errorf("got %d receipts but expected %d", len(receipts), len(txHashes))
	}
	if len(txHashes) == 0 {
		if receiptHash != types.EmptyRootHash {
			return fmt.Errorf("no transactions, but got non-empty receipt trie root: %s", receiptHash)
		}
		return nil
	}

	logIndex := uint(0)
	for i, r := range receipts {
		if r == nil {
			return fmt.Errorf("receipt of tx %d returns nil on retrieval", i)
		}
		if r.TransactionIndex != uint(i) {
			return fmt.Errorf("receipt %d has unexpected tx index %d", i, r.TransactionIndex)
		}
		// gasUsed/cumulativeGas monotonicity is intentionally NOT checked on
		// RSK; integrity is enforced by the trie-root comparison below.
		for j, log := range r.Logs {
			if log.Index != logIndex {
				return fmt.Errorf("log %d (%d of tx %d) has unexpected log index %d", logIndex, j, i, log.Index)
			}
			if log.TxIndex != uint(i) {
				return fmt.Errorf("log %d has unexpected tx index %d", log.Index, log.TxIndex)
			}
			if log.TxHash != txHashes[i] {
				return fmt.Errorf("log %d of tx %s has unexpected tx hash %s", log.Index, txHashes[i], log.TxHash)
			}
			if log.Removed {
				return fmt.Errorf("canonical log (%d) must never be removed due to reorg", log.Index)
			}
			logIndex++
		}
	}

	computed := trie.CalculateReceiptsRoot(receipts)
	if receiptHash != computed {
		return fmt.Errorf("failed to fetch list of receipts: expected receipt root %s but computed %s from retrieved receipts", receiptHash, computed)
	}
	return nil
}

// ChainAwareReceiptsValidator returns a ReceiptsValidatorFn that delegates
// to ValidateRSKReceipts for RSK chains and to fallback otherwise. If
// fallback is nil for a non-RSK chain, the returned validator returns an
// error explaining the missing default — patch 0001 must always provide one.
func ChainAwareReceiptsValidator(l1ChainID uint64, fallback ReceiptsValidatorFn) ReceiptsValidatorFn {
	if trie.IsRSKChain(l1ChainID) {
		return ValidateRSKReceipts
	}
	if fallback == nil {
		return func(_ context.Context, _ BlockRef, _ common.Hash, _ []common.Hash, _ []*types.Receipt) error {
			return fmt.Errorf("oprsk/l1source: no fallback receipts validator for chain %d", l1ChainID)
		}
	}
	return fallback
}
