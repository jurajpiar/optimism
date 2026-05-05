package l1source

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum-optimism/optimism/rsk/trie"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestVerifyRSKBlock_EmptyBlockMatches(t *testing.T) {
	txs := types.Transactions{}
	header := &types.Header{
		Number: big.NewInt(1),
		TxHash: trie.CalculateTxRoot(txs),
	}
	if err := VerifyRSKBlock(context.Background(), header, txs); err != nil {
		t.Fatalf("expected empty block to verify, got %v", err)
	}
}

func TestVerifyRSKBlock_TxRootMismatch(t *testing.T) {
	header := &types.Header{
		Number: big.NewInt(1),
		TxHash: common.HexToHash("0xdeadbeef"),
	}
	err := VerifyRSKBlock(context.Background(), header, types.Transactions{})
	if err == nil {
		t.Fatal("expected tx root mismatch error, got nil")
	}
}

func TestVerifyRSKBlock_RejectsWithdrawalsRoot(t *testing.T) {
	wr := common.HexToHash("0x01")
	txs := types.Transactions{}
	header := &types.Header{
		Number:          big.NewInt(1),
		TxHash:          trie.CalculateTxRoot(txs),
		WithdrawalsHash: &wr,
	}
	if err := VerifyRSKBlock(context.Background(), header, txs); err == nil {
		t.Fatal("expected error for non-nil withdrawals root, got nil")
	}
}

func TestChainAwareBlockVerifier_FallbackForEthereum(t *testing.T) {
	called := false
	fb := func(context.Context, *types.Header, types.Transactions) error {
		called = true
		return nil
	}
	v := ChainAwareBlockVerifier(1, fb)
	_ = v(context.Background(), &types.Header{}, types.Transactions{})
	if !called {
		t.Fatal("expected fallback to be called for chain id 1")
	}
}

func TestChainAwareBlockVerifier_RSKChainUsesRSKImpl(t *testing.T) {
	v := ChainAwareBlockVerifier(trie.RegtestChainID, nil)
	header := &types.Header{
		Number: big.NewInt(1),
		TxHash: trie.CalculateTxRoot(types.Transactions{}),
	}
	if err := v(context.Background(), header, types.Transactions{}); err != nil {
		t.Fatalf("RSK regtest block should verify via RSK impl, got %v", err)
	}
}
