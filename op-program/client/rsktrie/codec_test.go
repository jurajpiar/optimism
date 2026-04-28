package rsktrie

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	gorskTrie "gorsk/rsktrie"
)

func TestTransactionCodecRoundTrip(t *testing.T) {
	// Create test transactions
	txs := types.Transactions{
		types.NewTx(&types.LegacyTx{
			Nonce:    1,
			GasPrice: big.NewInt(1000000000),
			Gas:      21000,
			To:       ptrAddr(common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")),
			Value:    big.NewInt(1000000),
			Data:     nil,
			V:        big.NewInt(27),
			R:        big.NewInt(12345),
			S:        big.NewInt(67890),
		}),
		types.NewTx(&types.LegacyTx{
			Nonce:    0,
			GasPrice: big.NewInt(0),
			Gas:      50000,
			To:       nil, // contract creation
			Value:    big.NewInt(0),
			Data:     []byte{0x60, 0x60, 0x60, 0x40},
			V:        big.NewInt(28),
			R:        big.NewInt(99999),
			S:        big.NewInt(88888),
		}),
	}

	// Encode to RSK format
	encoded, err := EncodeRSKTransactions(txs)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if len(encoded) != 2 {
		t.Fatalf("expected 2 encoded txs, got %d", len(encoded))
	}

	// Decode back
	decoded, err := DecodeRSKTransactions(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 decoded txs, got %d", len(decoded))
	}

	// Verify fields match
	for i := range txs {
		if decoded[i].Nonce() != txs[i].Nonce() {
			t.Errorf("tx %d nonce: got %d, want %d", i, decoded[i].Nonce(), txs[i].Nonce())
		}
		if decoded[i].Gas() != txs[i].Gas() {
			t.Errorf("tx %d gas: got %d, want %d", i, decoded[i].Gas(), txs[i].Gas())
		}
		if decoded[i].GasPrice().Cmp(txs[i].GasPrice()) != 0 {
			t.Errorf("tx %d gasPrice: got %s, want %s", i, decoded[i].GasPrice(), txs[i].GasPrice())
		}
	}
}

func TestReceiptCodecRoundTrip(t *testing.T) {
	receipts := types.Receipts{
		{
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			Bloom:             types.Bloom{},
			Logs: []*types.Log{
				{
					Address: common.HexToAddress("0xdeadbeef"),
					Topics:  []common.Hash{common.HexToHash("0x01")},
					Data:    []byte{0x42},
				},
			},
			GasUsed: 21000,
		},
		{
			Status:            types.ReceiptStatusFailed,
			CumulativeGasUsed: 50000,
			Bloom:             types.Bloom{},
			Logs:              []*types.Log{},
			GasUsed:           30000,
		},
	}

	encoded, err := EncodeRSKReceipts(receipts)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeRSKReceipts(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 decoded receipts, got %d", len(decoded))
	}

	if decoded[0].Status != types.ReceiptStatusSuccessful {
		t.Errorf("receipt 0 status: got %d, want %d", decoded[0].Status, types.ReceiptStatusSuccessful)
	}
	if decoded[1].Status != types.ReceiptStatusFailed {
		t.Errorf("receipt 1 status: got %d, want %d", decoded[1].Status, types.ReceiptStatusFailed)
	}
	if decoded[0].GasUsed != 21000 {
		t.Errorf("receipt 0 gasUsed: got %d, want 21000", decoded[0].GasUsed)
	}
	if decoded[0].CumulativeGasUsed != 21000 {
		t.Errorf("receipt 0 cumulativeGasUsed: got %d, want 21000", decoded[0].CumulativeGasUsed)
	}
	if len(decoded[0].Logs) != 1 {
		t.Errorf("receipt 0 logs: got %d, want 1", len(decoded[0].Logs))
	}
}

// TestEncodeTrieRoundTrip verifies that encoding txs → building RSK trie → reading back works.
func TestEncodeTrieRoundTrip(t *testing.T) {
	txs := types.Transactions{
		types.NewTx(&types.LegacyTx{
			Nonce:    5,
			GasPrice: big.NewInt(20000000000),
			Gas:      21000,
			To:       ptrAddr(common.HexToAddress("0xaabbccdd")),
			Value:    big.NewInt(1e18),
			V:        big.NewInt(27),
			R:        big.NewInt(111),
			S:        big.NewInt(222),
		}),
	}

	// Encode to RSK format
	encoded, err := EncodeRSKTransactions(txs)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Build trie
	root, nodes := WriteTrie(encoded)
	if root == (common.Hash{}) {
		t.Fatal("empty root")
	}

	// Build preimage map
	preimages := make(map[common.Hash][]byte)
	for _, node := range nodes {
		hash := common.BytesToHash(keccak256(node))
		preimages[hash] = node
	}

	// Read back
	recovered := ReadTrie(root, func(key common.Hash) []byte {
		return preimages[key]
	})

	// Decode RSK format back to go-ethereum txs
	recoveredTxs, err := DecodeRSKTransactions(recovered)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(recoveredTxs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(recoveredTxs))
	}
	if recoveredTxs[0].Nonce() != 5 {
		t.Errorf("nonce: got %d, want 5", recoveredTxs[0].Nonce())
	}
}

func ptrAddr(a common.Address) *common.Address { return &a }

func keccak256(data []byte) []byte {
	return gorskTrie.Keccak256(data)
}
