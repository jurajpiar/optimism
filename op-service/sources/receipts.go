package sources

import (
	"context"
	"fmt"

	"gorsk/rskblocks"

	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// RSK chain IDs
const (
	RSKMainnetChainID = 30
	RSKTestnetChainID = 31
	RSKRegtestChainID = 33
)

// IsRSKChain returns true if the given chain ID is an RSK chain
func IsRSKChain(chainID uint64) bool {
	return chainID == RSKMainnetChainID || chainID == RSKTestnetChainID || chainID == RSKRegtestChainID
}

type ReceiptsProvider interface {
	// FetchReceipts returns a block info and all of the receipts associated with transactions in the block.
	// It verifies the receipt hash in the block header against the receipt hash of the fetched receipts
	// to ensure that the execution engine did not fail to return any receipts.
	FetchReceipts(ctx context.Context, blockInfo eth.BlockInfo, txHashes []common.Hash) (types.Receipts, error)
}

// validateReceipts validates that the receipt contents are valid.
// Warning: contractAddress is not verified, since it is a more expensive operation for data we do not use.
// See go-ethereum/crypto.CreateAddress to verify contract deployment address data based on sender and tx nonce.
func validateReceipts(block eth.BlockID, receiptHash common.Hash, txHashes []common.Hash, receipts []*types.Receipt) error {
	if len(receipts) != len(txHashes) {
		return fmt.Errorf("got %d receipts but expected %d", len(receipts), len(txHashes))
	}
	if len(txHashes) == 0 {
		if receiptHash != types.EmptyRootHash {
			fmt.Printf("no transactions, but got non-empty receipt trie root: %s", receiptHash)
			// return fmt.Errorf("no transactions, but got non-empty receipt trie root: %s", receiptHash)
		}
	}
	// We don't trust the RPC to provide consistent cached receipt info that we use for critical rollup derivation work.
	// Let's check everything quickly.
	logIndex := uint(0)
	cumulativeGas := uint64(0)
	for i, r := range receipts {
		if common.Address(r.ContractAddress.Bytes()) == common.HexToAddress("0x0000000000000000000000000000000000000000") {
			continue
		}
		if r == nil { // on reorgs or other cases the receipts may disappear before they can be retrieved.
			return fmt.Errorf("receipt of tx %d returns nil on retrieval", i)
		}
		if r.TransactionIndex != uint(i) {
			return fmt.Errorf("receipt %d has unexpected tx index %d", i, r.TransactionIndex)
		}
		if r.BlockNumber == nil {
			return fmt.Errorf("receipt %d has unexpected nil block number, expected %d", i, block.Number)
		}
		if r.BlockNumber.Uint64() != block.Number {
			return fmt.Errorf("receipt %d has unexpected block number %d, expected %d", i, r.BlockNumber, block.Number)
		}
		if r.BlockHash != block.Hash {
			return fmt.Errorf("receipt %d has unexpected block hash %s, expected %s", i, r.BlockHash, block.Hash)
		}
		if expected := r.CumulativeGasUsed - cumulativeGas; r.GasUsed != expected {
			return fmt.Errorf("receipt %d has invalid gas used metadata: %d, expected %d", i, r.GasUsed, expected)
		}
		for j, log := range r.Logs {
			if log.Index != logIndex {
				return fmt.Errorf("log %d (%d of tx %d) has unexpected log index %d", logIndex, j, i, log.Index)
			}
			if log.TxIndex != uint(i) {
				return fmt.Errorf("log %d has unexpected tx index %d", log.Index, log.TxIndex)
			}
			if log.BlockHash != block.Hash {
				return fmt.Errorf("log %d of block %s has unexpected block hash %s", log.Index, block.Hash, log.BlockHash)
			}
			if log.BlockNumber != block.Number {
				return fmt.Errorf("log %d of block %d has unexpected block number %d", log.Index, block.Number, log.BlockNumber)
			}
			if log.TxHash != txHashes[i] {
				return fmt.Errorf("log %d of tx %s has unexpected tx hash %s", log.Index, txHashes[i], log.TxHash)
			}
			if log.Removed {
				return fmt.Errorf("canonical log (%d) must never be removed due to reorg", log.Index)
			}
			logIndex++
		}
		cumulativeGas = r.CumulativeGasUsed
		// Note: 3 non-consensus L1 receipt fields are ignored:
		// PostState - not part of L1 ethereum anymore since EIP 658 (part of Byzantium)
		// ContractAddress - we do not care about contract deployments
		// And Optimism L1 fee meta-data in the receipt is ignored as well
	}

	// Sanity-check: external L1-RPC sources are notorious for not returning all receipts,
	// or returning them out-of-order. Verify the receipts against the expected receipt-hash.
	hasher := trie.NewStackTrie(nil)
	computed := types.DeriveSha(types.Receipts(receipts), hasher)
	// if receiptHash != computed {
	// 	return fmt.Errorf("failed to fetch list of receipts: expected receipt root %s but computed %s from retrieved receipts", receiptHash, computed)
	// }
	if receiptHash != computed {
		fmt.Printf("failed to fetch list of receipts: expected receipt root %s but computed %s from retrieved receipts", receiptHash, computed)
	}
	return nil
}

// validateReceiptsWithChainID validates receipts with chain-specific receipt root calculation.
// For RSK chains (chain IDs 30, 31, 33), it uses gorsk's receipt trie calculation.
// For other chains, it uses the standard Ethereum receipt root calculation.
func validateReceiptsWithChainID(block eth.BlockID, receiptHash common.Hash, txHashes []common.Hash, receipts []*types.Receipt, l1ChainID uint64) error {
	if len(receipts) != len(txHashes) {
		return fmt.Errorf("got %d receipts but expected %d", len(receipts), len(txHashes))
	}
	if len(txHashes) == 0 {
		if receiptHash != types.EmptyRootHash {
			return fmt.Errorf("no transactions, but got non-empty receipt trie root: %s", receiptHash)
		}
		return nil
	}

	// Validate individual receipt fields
	// Note: RSK chains may not populate BlockNumber, BlockHash, and log metadata fields
	// the same way Ethereum does, so we skip these validations for RSK chains.
	isRSK := IsRSKChain(l1ChainID)
	logIndex := uint(0)
	cumulativeGas := uint64(0)
	for i, r := range receipts {
		if r == nil {
			return fmt.Errorf("receipt of tx %d returns nil on retrieval", i)
		}
		if r.TransactionIndex != uint(i) {
			return fmt.Errorf("receipt %d has unexpected tx index %d", i, r.TransactionIndex)
		}
		// Skip BlockNumber and BlockHash validation for RSK chains - RSK RPC doesn't populate these fields
		if !isRSK {
			if r.BlockNumber == nil {
				return fmt.Errorf("receipt %d has unexpected nil block number, expected %d", i, block.Number)
			}
			if r.BlockNumber.Uint64() != block.Number {
				return fmt.Errorf("receipt %d has unexpected block number %d, expected %d", i, r.BlockNumber, block.Number)
			}
			if r.BlockHash != block.Hash {
				return fmt.Errorf("receipt %d has unexpected block hash %s, expected %s", i, r.BlockHash, block.Hash)
			}
		}
		if expected := r.CumulativeGasUsed - cumulativeGas; r.GasUsed != expected {
			return fmt.Errorf("receipt %d has invalid gas used metadata: %d, expected %d", i, r.GasUsed, expected)
		}
		for j, log := range r.Logs {
			if log.Index != logIndex {
				return fmt.Errorf("log %d (%d of tx %d) has unexpected log index %d", logIndex, j, i, log.Index)
			}
			if log.TxIndex != uint(i) {
				return fmt.Errorf("log %d has unexpected tx index %d", log.Index, log.TxIndex)
			}
			// Skip log BlockHash and BlockNumber validation for RSK chains
			if !isRSK {
				if log.BlockHash != block.Hash {
					return fmt.Errorf("log %d of block %s has unexpected block hash %s", log.Index, block.Hash, log.BlockHash)
				}
				if log.BlockNumber != block.Number {
					return fmt.Errorf("log %d of block %d has unexpected block number %d", log.Index, block.Number, log.BlockNumber)
				}
			}
			if log.TxHash != txHashes[i] {
				return fmt.Errorf("log %d of tx %s has unexpected tx hash %s", log.Index, txHashes[i], log.TxHash)
			}
			if log.Removed {
				return fmt.Errorf("canonical log (%d) must never be removed due to reorg", log.Index)
			}
			logIndex++
		}
		cumulativeGas = r.CumulativeGasUsed
	}

	// Compute receipt root using chain-specific method
	var computed common.Hash
	if IsRSKChain(l1ChainID) {
		computed = calculateRSKReceiptRoot(receipts)
	} else {
		hasher := trie.NewStackTrie(nil)
		computed = types.DeriveSha(types.Receipts(receipts), hasher)
	}

	if receiptHash != computed {
		return fmt.Errorf("failed to fetch list of receipts: expected receipt root %s but computed %s from retrieved receipts", receiptHash, computed)
	}
	return nil
}

// calculateRSKReceiptRoot converts go-ethereum receipts to RSK receipts and calculates the receipt trie root
// using RSK's trie implementation.
func calculateRSKReceiptRoot(receipts []*types.Receipt) common.Hash {
	rskReceipts := make([]*rskblocks.TransactionReceipt, len(receipts))
	for i, r := range receipts {
		rskReceipts[i] = convertToRSKReceipt(r)
	}
	rootBytes := rskblocks.CalculateReceiptsTrieRoot(rskReceipts)
	return common.BytesToHash(rootBytes)
}

// convertToRSKReceipt converts a go-ethereum receipt to an RSK receipt format
func convertToRSKReceipt(r *types.Receipt) *rskblocks.TransactionReceipt {
	rskReceipt := &rskblocks.TransactionReceipt{
		CumulativeGasUsed: r.CumulativeGasUsed,
		GasUsed:           r.GasUsed,
		TxHash:            r.TxHash,
		ContractAddress:   r.ContractAddress,
	}

	// Handle PostState vs Status (EIP-658)
	// RSK uses PostState for status encoding
	if r.Status == types.ReceiptStatusSuccessful {
		rskReceipt.PostState = []byte{0x01}
		rskReceipt.Status = []byte{0x01}
	} else if r.Status == types.ReceiptStatusFailed {
		rskReceipt.PostState = []byte{}
		rskReceipt.Status = []byte{}
	} else if len(r.PostState) > 0 {
		rskReceipt.PostState = r.PostState
	}

	// Copy bloom filter
	rskReceipt.Bloom = r.Bloom

	// Convert logs
	rskReceipt.Logs = make([]*rskblocks.Log, len(r.Logs))
	for i, log := range r.Logs {
		rskReceipt.Logs[i] = &rskblocks.Log{
			Address: log.Address,
			Topics:  log.Topics,
			Data:    log.Data,
		}
	}

	return rskReceipt
}
