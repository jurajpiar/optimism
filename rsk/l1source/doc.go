// Package l1source provides RSK-aware adapters for the op-service/sources
// L1 client. They are the runtime side of patch 0001
// ("eth-client-pluggable-validators"): once that patch exposes BlockVerifier
// and ReceiptsValidator hooks on EthClientConfig, cmd/ wrappers inject the
// functions defined here so RSK trie roots and tolerated field-skips replace
// the default Ethereum-style verification.
//
// Until patch 0001 is wired in (Phase 6), the same RSK behavior continues to
// live inline in the optimism fork; the helpers in this package match it
// 1:1 so the cutover is purely a wiring change.
package l1source
