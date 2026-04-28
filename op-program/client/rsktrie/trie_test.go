package rsktrie

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"

	"gorsk/rsktrie"
)

// TestWriteReadRoundTrip verifies that WriteTrie produces the same root
// as gorsk's direct trie building, and that ReadTrie can recover all values.
func TestWriteReadRoundTrip(t *testing.T) {
	// Build test data: simulate 5 transactions (arbitrary byte slices)
	values := [][]byte{
		{0xde, 0xad, 0xbe, 0xef},
		{0xca, 0xfe, 0xba, 0xbe},
		{0x01, 0x02, 0x03},
		{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa},
		{0x42},
	}

	// Build expected root using gorsk directly
	expectedTrie := rsktrie.NewTrie(nil)
	for i, v := range values {
		key, _ := rlp.EncodeToBytes(uint64(i))
		expectedTrie = expectedTrie.Put(key, v)
	}
	expectedRoot := common.BytesToHash(expectedTrie.GetHash())

	// Use our WriteTrie
	hexValues := make([]hexutil.Bytes, len(values))
	for i, v := range values {
		hexValues[i] = v
	}
	gotRoot, nodes := WriteTrie(hexValues)

	if gotRoot != expectedRoot {
		t.Fatalf("root mismatch: got %s, want %s", gotRoot.Hex(), expectedRoot.Hex())
	}

	if len(nodes) == 0 {
		t.Fatal("expected at least one node preimage")
	}

	// Build a preimage map from the nodes
	preimages := make(map[common.Hash][]byte)
	for _, node := range nodes {
		hash := common.BytesToHash(rsktrie.Keccak256(node))
		preimages[hash] = node
	}

	// Use ReadTrie to recover values
	recovered := ReadTrie(gotRoot, func(key common.Hash) []byte {
		data, ok := preimages[key]
		if !ok {
			t.Fatalf("missing preimage for %s", key.Hex())
		}
		return data
	})

	if len(recovered) != len(values) {
		t.Fatalf("recovered %d values, want %d", len(recovered), len(values))
	}

	for i, v := range values {
		if string(recovered[i]) != string(v) {
			t.Errorf("value %d mismatch: got %x, want %x", i, recovered[i], v)
		}
	}
}

// TestEmptyTrie verifies that an empty value list produces no output.
func TestEmptyTrie(t *testing.T) {
	result := ReadTrie(common.Hash{}, func(key common.Hash) []byte {
		t.Fatal("should not be called for empty trie")
		return nil
	})
	if result != nil {
		t.Fatalf("expected nil for empty trie, got %v", result)
	}
}

// TestSingleValue verifies a trie with a single value.
func TestSingleValue(t *testing.T) {
	values := []hexutil.Bytes{{0x42, 0x43, 0x44}}
	root, nodes := WriteTrie(values)

	preimages := make(map[common.Hash][]byte)
	for _, node := range nodes {
		hash := common.BytesToHash(rsktrie.Keccak256(node))
		preimages[hash] = node
	}

	recovered := ReadTrie(root, func(key common.Hash) []byte {
		return preimages[key]
	})

	if len(recovered) != 1 {
		t.Fatalf("recovered %d values, want 1", len(recovered))
	}
	if string(recovered[0]) != string(values[0]) {
		t.Errorf("value mismatch: got %x, want %x", recovered[0], values[0])
	}
}
