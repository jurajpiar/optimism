// Package rsktrie provides RSK binary unitrie read/write functionality
// for the fault proof program. It mirrors the mpt package's interface
// but uses RSK's RSKIP-107 binary unitrie instead of Ethereum's MPT.
package rsktrie

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"

	"gorsk/rsktrie"
)

// oracleStore implements rsktrie.TrieStore backed by the preimage oracle.
// Non-embedded trie nodes are fetched by their keccak256 hash via getPreimage.
type oracleStore struct {
	getPreimage func(key common.Hash) []byte
}

func (s *oracleStore) Save(t *rsktrie.Trie) {
	panic("save not supported in oracle store")
}

func (s *oracleStore) Retrieve(hash []byte) *rsktrie.Trie {
	if len(hash) != 32 {
		panic(fmt.Sprintf("expected 32 byte hash, got %d bytes", len(hash)))
	}
	data := s.getPreimage(common.BytesToHash(hash))
	if data == nil {
		return nil
	}
	node, err := rsktrie.FromMessage(data, s)
	if err != nil {
		panic(fmt.Errorf("failed to deserialize trie node: %w", err))
	}
	return node
}

func (s *oracleStore) RetrieveValue(hash []byte) []byte {
	if len(hash) != 32 {
		return nil
	}
	return s.getPreimage(common.BytesToHash(hash))
}

// ReadTrie takes an RSK binary unitrie root hash and a preimage oracle getter,
// traverses the trie in-order, and returns all leaf values ordered by their
// RLP-encoded index key. This mirrors mpt.ReadTrie but for RSK's binary unitrie.
func ReadTrie(root common.Hash, getPreimage func(key common.Hash) []byte) []hexutil.Bytes {
	if root == (common.Hash{}) {
		return nil
	}

	store := &oracleStore{getPreimage: getPreimage}

	// Fetch and parse the root node
	rootData := getPreimage(root)
	if rootData == nil {
		panic(fmt.Sprintf("missing root node %s", root.Hex()))
	}
	rootNode, err := rsktrie.FromMessage(rootData, store)
	if err != nil {
		panic(fmt.Errorf("failed to parse root node: %w", err))
	}

	// In-order traversal collects leaves sorted by their bit-level key,
	// which corresponds to the canonical ordering of RLP-encoded uint indices.
	iter := rootNode.GetInOrderIterator()

	var values [][]byte
	var keys []uint64
	for iter.HasNext() {
		elem := iter.Next()
		node := elem.GetNode()
		val := node.GetValue()
		if val == nil {
			continue
		}

		// The key in the unitrie is the bit-expanded form of the RLP-encoded index.
		// Encode() compacts the bit-level key back to bytes.
		nodeKey := elem.GetNodeKey()
		keyBytes := nodeKey.Encode()

		var idx uint64
		if err := rlp.DecodeBytes(keyBytes, &idx); err != nil {
			panic(fmt.Errorf("invalid trie key: %w", err))
		}
		keys = append(keys, idx)
		values = append(values, val)
	}

	out := make([]hexutil.Bytes, len(values))
	for i, idx := range keys {
		if idx >= uint64(len(values)) {
			panic(fmt.Sprintf("bad key: %d", idx))
		}
		if out[idx] != nil {
			panic(fmt.Sprintf("duplicate key %d", idx))
		}
		out[idx] = values[i]
	}
	return out
}

// collectingStore wraps a TrieStore and collects all serialized non-embedded
// nodes during trie construction for storage as preimages.
type collectingStore struct {
	nodes []hexutil.Bytes
}

func (s *collectingStore) Save(t *rsktrie.Trie) {
	msg := t.ToMessage()
	if len(msg) > rsktrie.MaxEmbeddedNodeSizeInBytes {
		s.nodes = append(s.nodes, common.CopyBytes(msg))
	}
}

func (s *collectingStore) Retrieve(hash []byte) *rsktrie.Trie { return nil }
func (s *collectingStore) RetrieveValue(hash []byte) []byte    { return nil }

// WriteTrie takes a list of values and builds an RSK binary unitrie with values
// keyed by their RLP-encoded index. Returns the root hash and all non-embedded
// node serializations (to be stored as preimages keyed by keccak256).
// This mirrors mpt.WriteTrie but for RSK's binary unitrie.
//
// Values longer than 32 bytes are stored as "long values" in the unitrie:
// the trie node contains keccak256(value) + length, and the raw value is
// stored separately as a preimage.
func WriteTrie(values []hexutil.Bytes) (common.Hash, []hexutil.Bytes) {
	trie := rsktrie.NewTrie(nil)

	for i, val := range values {
		key, err := rlp.EncodeToBytes(uint64(i))
		if err != nil {
			panic(fmt.Errorf("failed to encode key %d: %w", i, err))
		}
		trie = trie.Put(key, val)
	}

	rootHash := trie.GetHash()

	// Collect all trie node serializations.
	var nodes []hexutil.Bytes
	collectNodes(trie, &nodes)

	// Also collect long values (> 32 bytes) as separate preimages.
	// The trie node only stores keccak256(value) + length for these.
	for _, val := range values {
		if len(val) > 32 {
			nodes = append(nodes, common.CopyBytes(val))
		}
	}

	return common.BytesToHash(rootHash), nodes
}

// collectNodes performs a pre-order traversal of the trie, collecting
// serialized forms of all non-embedded nodes. Embedded nodes are inlined
// in their parents and don't need separate preimage storage.
func collectNodes(t *rsktrie.Trie, out *[]hexutil.Bytes) {
	if t == nil {
		return
	}

	// Collect this node's serialization
	msg := t.ToMessage()
	*out = append(*out, common.CopyBytes(msg))

	// Recurse into non-embedded children
	if left := t.GetLeft(); !left.IsEmpty() && !left.IsEmbeddable() {
		if node := left.GetNode(); node != nil {
			collectNodes(node, out)
		}
	}
	if right := t.GetRight(); !right.IsEmpty() && !right.IsEmbeddable() {
		if node := right.GetNode(); node != nil {
			collectNodes(node, out)
		}
	}
}
