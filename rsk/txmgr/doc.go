// Package txmgr provides RSK-specific helpers for op-service/txmgr:
//
//   - A GasPriceEstimatorFn that uses eth_gasPrice and floors the result
//     for legacy (pre-EIP-1559) transactions, re-exported from gorsk.
//   - A set of RSK-specific RPC error messages that map to "already known"
//     and so should be treated as published (AlreadyPublishedCustomErrs).
//
// The package is consumed by cmd/ wrappers that construct txmgr.CLIConfig
// (batcher, proposer, etc.) so the RSK overrides live in one place.
package txmgr
