package nodecfg

import (
	"time"

	"github.com/ethereum-optimism/optimism/op-node/config"
	"github.com/ethereum-optimism/optimism/op-node/rollup/driver"
	opsync "github.com/ethereum-optimism/optimism/op-node/rollup/sync"
	"github.com/ethereum-optimism/optimism/op-service/sources"
)

// L1Endpoint returns an L1EndpointConfig with the defaults required for an
// RSK L1: trusted RPC (PoW chain, no fork-choice protection from beacon),
// RPCKindBasic (RSK doesn't implement eth_getBlockReceipts), and conservative
// batching/concurrency tuned for RSK's ~30 s block time.
//
// l1NodeAddr is the RSK RPC URL.
func L1Endpoint(l1NodeAddr string) *config.L1EndpointConfig {
	return &config.L1EndpointConfig{
		L1NodeAddr:       l1NodeAddr,
		L1TrustRPC:       true,
		L1RPCKind:        sources.RPCKindBasic,
		RateLimit:        0,
		BatchSize:        20,
		HttpPollInterval: 100 * time.Millisecond,
		MaxConcurrency:   10,
		CacheSize:        0,
	}
}

// L1Beacon returns a Beacon endpoint config with the RSK-specific
// BeaconCheckIgnore=true (RSK is PoW, has no consensus beacon). If
// beaconAddr is empty a dummy localhost:0 is used; the beacon will never
// actually be hit because checks are ignored.
func L1Beacon(beaconAddr string) *config.L1BeaconEndpointConfig {
	if beaconAddr == "" {
		beaconAddr = "http://localhost:0"
	}
	return &config.L1BeaconEndpointConfig{
		BeaconAddr:        beaconAddr,
		BeaconCheckIgnore: true,
	}
}

// Driver returns a driver.Config with the RSK-specific VerifierConfDepth=4
// (~2 minutes behind tip; mitigates reorg-related receipt fetch failures)
// and SequencerConfDepth=2.
//
// sequencer toggles SequencerEnabled.
func Driver(sequencer bool) driver.Config {
	return driver.Config{
		VerifierConfDepth:  4,
		SequencerEnabled:   sequencer,
		SequencerConfDepth: 2,
	}
}

// Sync returns the op-node Sync config used on RSK: CL-sync with
// SkipSyncStartCheck=true (the L1-origin sanity check assumes Ethereum
// finality semantics that don't apply to RSK).
func Sync() opsync.Config {
	return opsync.Config{
		SyncMode:           opsync.CLSync,
		SkipSyncStartCheck: true,
	}
}

// L1EpochPollInterval returns 0 — the L1 epoch poll loop is disabled on RSK
// because the rollup driver pulls L1 blocks reactively via the safedb path.
func L1EpochPollInterval() time.Duration { return 0 }

// ApplyOpNodeRSK overrides RSK-required fields on a *config.Config that was
// built from upstream op-node flags. Use it in cmd/rsk-op-node to ride on
// top of upstream's CLI plumbing while forcing the RSK pieces (RPCKindBasic,
// trusted RPC, beacon-check-ignore, conf depths, sync skip-start-check,
// disabled L1 epoch poll).
//
// It additionally wraps cfg.L1 in an RSKL1Endpoint so the patch-0001 hooks
// (BlockVerifier / HeaderVerifier / ReceiptsValidator / TxHashesFromBlock)
// are installed at L1Setup time with their oprsk/l1source impls. Pass the
// known RSK chain id so the chain-aware adapters can route correctly.
//
// The L1 endpoint URL, JWT path, datadir, ports, and other operator-supplied
// flags are preserved. cfg.L1 / cfg.Beacon must be the concrete
// *config.L1EndpointConfig / *config.L1BeaconEndpointConfig produced by the
// upstream flags package; if they are not, the corresponding overrides are
// silently skipped (and L1 hooks are not wrapped).
func ApplyOpNodeRSK(cfg *config.Config, l1ChainID uint64) {
	if l1, ok := cfg.L1.(*config.L1EndpointConfig); ok && l1 != nil {
		l1.L1TrustRPC = true
		l1.L1RPCKind = sources.RPCKindBasic
		if l1.HttpPollInterval == 0 {
			l1.HttpPollInterval = 100 * time.Millisecond
		}
		// Wrap with the patch-0001 hook installer. Done last so the
		// underlying *L1EndpointConfig still reflects the RSK-required
		// fields above.
		cfg.L1 = &RSKL1Endpoint{Inner: l1, L1ChainID: l1ChainID}
	}
	if beacon, ok := cfg.Beacon.(*config.L1BeaconEndpointConfig); ok && beacon != nil {
		beacon.BeaconCheckIgnore = true
		if beacon.BeaconAddr == "" {
			beacon.BeaconAddr = "http://localhost:0"
		}
	} else if cfg.Beacon == nil {
		cfg.Beacon = L1Beacon("")
	}
	cfg.Driver.VerifierConfDepth = 4
	cfg.Driver.SequencerConfDepth = 2
	cfg.Sync.SyncMode = opsync.CLSync
	cfg.Sync.SkipSyncStartCheck = true
	cfg.L1EpochPollInterval = 0
}
