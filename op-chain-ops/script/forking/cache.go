package forking

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
)

type storageKey struct {
	Addr common.Address
	Slot common.Hash
}

// CachedSource wraps a ForkSource, and caches the retrieved data for faster repeat-queries.
// The ForkSource should be immutable (as per the StateRoot value).
// All cache data accumulates in-memory in LRU collections per data type.
// If the underlying source supports ProofSource, CachedSource also implements ProofSource
// for batch fetching with eth_getProof.
type CachedSource struct {
	stateRoot common.Hash
	src       ForkSource

	nonces   *lru.Cache[common.Address, uint64]
	balances *lru.Cache[common.Address, *uint256.Int]
	storage  *lru.Cache[storageKey, common.Hash]
	code     *lru.Cache[common.Address, []byte]
	codeHash *lru.Cache[common.Address, common.Hash]
}

var _ ForkSource = (*CachedSource)(nil)

func mustNewLRU[K comparable, V any](size int) *lru.Cache[K, V] {
	out, err := lru.New[K, V](size)
	if err != nil {
		panic(err) // bad size parameter may produce an error
	}
	return out
}

func Cache(src ForkSource) *CachedSource {
	return &CachedSource{
		stateRoot: src.StateRoot(),
		src:       src,
		nonces:    mustNewLRU[common.Address, uint64](10000),
		balances:  mustNewLRU[common.Address, *uint256.Int](10000),
		storage:   mustNewLRU[storageKey, common.Hash](10000),
		code:      mustNewLRU[common.Address, []byte](1000),
		codeHash:  mustNewLRU[common.Address, common.Hash](10000),
	}
}

// SupportsProof returns true if the underlying source supports eth_getProof
func (c *CachedSource) SupportsProof() bool {
	_, ok := c.src.(ProofSource)
	return ok
}

func (c *CachedSource) URLOrAlias() string {
	return c.src.URLOrAlias()
}

func (c *CachedSource) StateRoot() common.Hash {
	return c.stateRoot
}

func (c *CachedSource) Nonce(addr common.Address) (uint64, error) {
	if v, ok := c.nonces.Get(addr); ok {
		return v, nil
	}
	v, err := c.src.Nonce(addr)
	if err != nil {
		return 0, err
	}
	c.nonces.Add(addr, v)
	return v, nil
}

func (c *CachedSource) Balance(addr common.Address) (*uint256.Int, error) {
	if v, ok := c.balances.Get(addr); ok {
		return v.Clone(), nil
	}
	v, err := c.src.Balance(addr)
	if err != nil {
		return nil, err
	}
	c.balances.Add(addr, v)
	return v.Clone(), nil
}

func (c *CachedSource) StorageAt(addr common.Address, key common.Hash) (common.Hash, error) {
	if v, ok := c.storage.Get(storageKey{Addr: addr, Slot: key}); ok {
		return v, nil
	}
	v, err := c.src.StorageAt(addr, key)
	if err != nil {
		return common.Hash{}, err
	}
	c.storage.Add(storageKey{Addr: addr, Slot: key}, v)
	return v, nil
}

func (c *CachedSource) Code(addr common.Address) ([]byte, error) {
	if v, ok := c.code.Get(addr); ok {
		return v, nil
	}
	v, err := c.src.Code(addr)
	if err != nil {
		return nil, err
	}
	c.code.Add(addr, v)
	return v, nil
}

// CodeHash returns the cached code hash for an address, if available.
// Returns zero hash and false if not cached.
func (c *CachedSource) CodeHash(addr common.Address) (common.Hash, bool) {
	return c.codeHash.Get(addr)
}

// GetProof fetches account data and storage slots using eth_getProof and populates all caches.
// Returns error if the underlying source doesn't support ProofSource.
func (c *CachedSource) GetProof(addr common.Address, slots []common.Hash) (*AccountProof, error) {
	ps, ok := c.src.(ProofSource)
	if !ok {
		return nil, nil // Silently return nil if not supported
	}

	proof, err := ps.GetProof(addr, slots)
	if err != nil {
		return nil, err
	}

	// Populate all caches from the proof response
	c.nonces.Add(addr, proof.Nonce)
	c.balances.Add(addr, proof.Balance)
	c.codeHash.Add(addr, proof.CodeHash)

	// Cache all storage values
	for slot, value := range proof.Storage {
		c.storage.Add(storageKey{Addr: addr, Slot: slot}, value)
	}

	return proof, nil
}

// PrefetchAccount fetches account data using eth_getProof (if supported) and populates caches.
// This is more efficient than individual Nonce/Balance calls.
func (c *CachedSource) PrefetchAccount(addr common.Address) error {
	if !c.SupportsProof() {
		return nil
	}
	_, err := c.GetProof(addr, nil)
	return err
}

// PrefetchStorage fetches account data and storage slots using eth_getProof and populates caches.
// This is much more efficient than individual StorageAt calls.
func (c *CachedSource) PrefetchStorage(addr common.Address, slots []common.Hash) error {
	if !c.SupportsProof() {
		return nil
	}
	_, err := c.GetProof(addr, slots)
	return err
}
