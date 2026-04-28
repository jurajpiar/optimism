package rsktrie

// RSK chain IDs
const (
	MainnetChainID = 30
	TestnetChainID = 31
	RegtestChainID = 33
)

// IsRSKChain returns true if the given chain ID is an RSK chain.
func IsRSKChain(chainID uint64) bool {
	return chainID == MainnetChainID || chainID == TestnetChainID || chainID == RegtestChainID
}
