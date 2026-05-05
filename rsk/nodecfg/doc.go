// Package nodecfg centralizes the RSK-specific defaults that the op-stack
// services (op-node, op-batcher, op-proposer) need to talk to an RSK L1.
//
// Each builder fills only the fields that differ from upstream's defaults
// (RPCKindBasic, BeaconCheckIgnore, AllowNonFinalized, legacy-tx + RSK gas
// estimator on the txmgr, throttling disabled, etc.) so callers can compose
// them with their non-RSK config (listen ports, data dirs, JWT, ...).
//
// The package replaces the ~300 LoC of inline RSK overrides previously living
// in cmd/rollup-node/main.go and is reused by the per-role wrappers
// (cmd/rsk-op-node, cmd/rsk-op-batcher, cmd/rsk-op-proposer).
package nodecfg
