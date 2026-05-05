package l1source

import (
	"context"
	"testing"

	"github.com/ethereum-optimism/optimism/rsk/trie"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestValidateRSKReceipts_EmptyOK(t *testing.T) {
	err := ValidateRSKReceipts(
		context.Background(),
		BlockRef{Number: 1, Hash: common.Hash{0x42}},
		types.EmptyRootHash,
		nil, nil,
	)
	if err != nil {
		t.Fatalf("empty receipts should validate, got %v", err)
	}
}

func TestValidateRSKReceipts_LengthMismatch(t *testing.T) {
	err := ValidateRSKReceipts(
		context.Background(),
		BlockRef{},
		common.Hash{},
		[]common.Hash{{0x01}},
		nil,
	)
	if err == nil {
		t.Fatal("expected length mismatch error")
	}
}

func TestValidateRSKReceipts_NonEmptyEmptyTxHashError(t *testing.T) {
	err := ValidateRSKReceipts(
		context.Background(),
		BlockRef{},
		common.HexToHash("0xff"),
		nil, nil,
	)
	if err == nil {
		t.Fatal("expected error for non-empty receipt root with no txs")
	}
}

func TestValidateRSKReceipts_RootMismatch(t *testing.T) {
	txHash := common.HexToHash("0xaa")
	r := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 100,
		GasUsed:           100,
		TxHash:            txHash,
		TransactionIndex:  0,
	}
	err := ValidateRSKReceipts(
		context.Background(),
		BlockRef{Number: 1, Hash: common.Hash{0x42}},
		common.HexToHash("0xdead"),
		[]common.Hash{txHash},
		[]*types.Receipt{r},
	)
	if err == nil {
		t.Fatal("expected receipt root mismatch error")
	}
}

func TestValidateRSKReceipts_RootMatch(t *testing.T) {
	txHash := common.HexToHash("0xaa")
	r := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		CumulativeGasUsed: 100,
		GasUsed:           100,
		TxHash:            txHash,
		TransactionIndex:  0,
	}
	root := trie.CalculateReceiptsRoot(types.Receipts{r})
	if err := ValidateRSKReceipts(
		context.Background(),
		BlockRef{Number: 1, Hash: common.Hash{0x42}},
		root,
		[]common.Hash{txHash},
		[]*types.Receipt{r},
	); err != nil {
		t.Fatalf("expected receipt to validate against computed root, got %v", err)
	}
}

func TestChainAwareReceiptsValidator_RSK(t *testing.T) {
	v := ChainAwareReceiptsValidator(trie.MainnetChainID, nil)
	if err := v(context.Background(), BlockRef{}, types.EmptyRootHash, nil, nil); err != nil {
		t.Fatalf("RSK mainnet validator should accept empty receipts, got %v", err)
	}
}

func TestChainAwareReceiptsValidator_NonRSKNoFallback(t *testing.T) {
	v := ChainAwareReceiptsValidator(1, nil)
	if err := v(context.Background(), BlockRef{}, types.EmptyRootHash, nil, nil); err == nil {
		t.Fatal("expected error from missing-fallback validator")
	}
}
