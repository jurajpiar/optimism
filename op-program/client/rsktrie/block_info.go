package rsktrie

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum-optimism/optimism/op-service/eth"

	"gorsk/rskblocks"
)

// RSKBlockInfo implements eth.BlockInfo for RSK block headers.
// It wraps gorsk's BlockHeader and maps its fields to the BlockInfo interface.
type RSKBlockInfo struct {
	hash   common.Hash
	header *rskblocks.BlockHeader
	// Cached synthetic types.Header for the Header() method
	syntheticHeader *types.Header
}

var _ eth.BlockInfo = (*RSKBlockInfo)(nil)

// NewRSKBlockInfo creates a BlockInfo from an RSK block header with a trusted hash.
func NewRSKBlockInfo(hash common.Hash, header *rskblocks.BlockHeader) *RSKBlockInfo {
	return &RSKBlockInfo{hash: hash, header: header}
}

func (r *RSKBlockInfo) Hash() common.Hash       { return r.hash }
func (r *RSKBlockInfo) ParentHash() common.Hash  { return r.header.ParentHash }
func (r *RSKBlockInfo) Coinbase() common.Address { return r.header.Coinbase }
func (r *RSKBlockInfo) Root() common.Hash        { return r.header.StateRoot }

func (r *RSKBlockInfo) NumberU64() uint64 {
	if r.header.Number == nil {
		return 0
	}
	return r.header.Number.Uint64()
}

func (r *RSKBlockInfo) Time() uint64 {
	if r.header.Timestamp == nil {
		return 0
	}
	return r.header.Timestamp.Uint64()
}

// MixDigest returns zero hash — RSK uses merged mining, not PoS randomness.
func (r *RSKBlockInfo) MixDigest() common.Hash { return common.Hash{} }

// BaseFee returns nil — RSK does not use EIP-1559.
func (r *RSKBlockInfo) BaseFee() *big.Int { return nil }

// BlobBaseFee returns nil — RSK does not support EIP-4844.
func (r *RSKBlockInfo) BlobBaseFee(_ *params.ChainConfig) *big.Int { return nil }

// ExcessBlobGas returns nil — RSK does not support EIP-4844.
func (r *RSKBlockInfo) ExcessBlobGas() *uint64 { return nil }

func (r *RSKBlockInfo) ReceiptHash() common.Hash { return r.header.ReceiptTrieRoot }

// TxHash returns the transactions trie root (binary unitrie root on RSK).
func (r *RSKBlockInfo) TxHash() common.Hash { return r.header.TxTrieRoot }

func (r *RSKBlockInfo) GasUsed() uint64 {
	if r.header.GasUsed == nil {
		return 0
	}
	return r.header.GasUsed.Uint64()
}

// BlobGasUsed returns nil — RSK does not support EIP-4844.
func (r *RSKBlockInfo) BlobGasUsed() *uint64 { return nil }

func (r *RSKBlockInfo) GasLimit() uint64 {
	if len(r.header.GasLimit) == 0 {
		return 0
	}
	return new(big.Int).SetBytes(r.header.GasLimit).Uint64()
}

// ParentBeaconRoot returns nil — RSK does not have beacon chain.
func (r *RSKBlockInfo) ParentBeaconRoot() *common.Hash { return nil }

// WithdrawalsRoot returns nil — RSK does not have withdrawals.
func (r *RSKBlockInfo) WithdrawalsRoot() *common.Hash { return nil }

// HeaderRLP returns the RLP encoding used for hashing (compressed form).
func (r *RSKBlockInfo) HeaderRLP() ([]byte, error) {
	return r.header.GetEncodedForHash(), nil
}

// Header returns a synthetic types.Header with the subset of fields that
// downstream consumers actually use. RSK headers have a different structure,
// so this is a best-effort mapping.
func (r *RSKBlockInfo) Header() *types.Header {
	if r.syntheticHeader != nil {
		return r.syntheticHeader
	}
	r.syntheticHeader = &types.Header{
		ParentHash:  r.header.ParentHash,
		UncleHash:   r.header.UnclesHash,
		Coinbase:    r.header.Coinbase,
		Root:        r.header.StateRoot,
		TxHash:      r.header.TxTrieRoot,
		ReceiptHash: r.header.ReceiptTrieRoot,
		Bloom:       types.Bloom(r.header.LogsBloom),
		Difficulty:  r.header.Difficulty,
		Number:      r.header.Number,
		GasLimit:    r.GasLimit(),
		GasUsed:     r.GasUsed(),
		Time:        r.Time(),
		Extra:       r.header.ExtraData,
	}
	return r.syntheticHeader
}

// DecodeRSKBlockHeader decodes RLP bytes into an RSK BlockHeader.
// RSK headers are RLP-encoded as a flat list of fields. The number of fields
// varies by version and network configuration. We decode the mandatory fields
// and as many optional fields as are present.
func DecodeRSKBlockHeader(rlpData []byte) (*rskblocks.BlockHeader, error) {
	return rskblocks.DecodeRLPBlockHeader(rlpData)
}
