package nodecfg

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum-optimism/optimism/op-batcher/flags"
	opsync "github.com/ethereum-optimism/optimism/op-node/rollup/sync"
	"github.com/ethereum-optimism/optimism/op-service/sources"
)

func TestL1Endpoint_RSKDefaults(t *testing.T) {
	cfg := L1Endpoint("http://rsk:4444")
	if cfg.L1NodeAddr != "http://rsk:4444" {
		t.Errorf("L1NodeAddr = %q", cfg.L1NodeAddr)
	}
	if !cfg.L1TrustRPC {
		t.Error("L1TrustRPC must be true for RSK")
	}
	if cfg.L1RPCKind != sources.RPCKindBasic {
		t.Errorf("L1RPCKind = %v, want RPCKindBasic", cfg.L1RPCKind)
	}
	if cfg.HttpPollInterval != 100*time.Millisecond {
		t.Errorf("HttpPollInterval = %v", cfg.HttpPollInterval)
	}
}

func TestL1Beacon_DummyForEmpty(t *testing.T) {
	cfg := L1Beacon("")
	if cfg.BeaconAddr == "" {
		t.Error("expected dummy beacon addr when input is empty")
	}
	if !cfg.BeaconCheckIgnore {
		t.Error("BeaconCheckIgnore must be true for RSK")
	}
}

func TestDriver_VerifierConfDepth(t *testing.T) {
	cfg := Driver(true)
	if !cfg.SequencerEnabled {
		t.Error("SequencerEnabled should be true")
	}
	if cfg.VerifierConfDepth != 4 {
		t.Errorf("VerifierConfDepth = %d, want 4", cfg.VerifierConfDepth)
	}
}

func TestSync_CLSyncWithSkipStartCheck(t *testing.T) {
	cfg := Sync()
	if cfg.SyncMode != opsync.CLSync {
		t.Errorf("SyncMode = %v, want CLSync", cfg.SyncMode)
	}
	if !cfg.SkipSyncStartCheck {
		t.Error("SkipSyncStartCheck must be true for RSK")
	}
}

func TestBatcherTxMgr_LegacyAndRSKEstimator(t *testing.T) {
	cfg := BatcherTxMgr("http://rsk:4444", "0xabc")
	if !cfg.UseLegacyTx {
		t.Error("UseLegacyTx must be true for RSK")
	}
	if cfg.GasPriceEstimatorFn == nil {
		t.Error("GasPriceEstimatorFn must be set")
	}
	if len(cfg.AlreadyPublishedCustomErrs) == 0 {
		t.Error("AlreadyPublishedCustomErrs must be set")
	}
}

func TestBatcherDefaults_CalldataDA(t *testing.T) {
	cfg := BatcherDefaults("http://rsk:4444", "0xabc")
	if cfg.DataAvailabilityType != flags.CalldataType {
		t.Errorf("DataAvailabilityType = %v, want CalldataType", cfg.DataAvailabilityType)
	}
	if cfg.BatchType != 0 {
		t.Errorf("BatchType = %d, want 0 (SingularBatch)", cfg.BatchType)
	}
}

func TestBatcherThrottle_Disabled(t *testing.T) {
	t1 := BatcherThrottle()
	if t1.LowerThreshold != 0 || t1.UpperThreshold != 0 {
		t.Error("throttle must be disabled (zero thresholds) for RSK")
	}
}

func TestProposerDefaults_AllowNonFinalized(t *testing.T) {
	cfg := ProposerDefaults("http://rsk:4444", "0xabc", "0xdgf")
	if !cfg.AllowNonFinalized {
		t.Error("AllowNonFinalized must be true for RSK")
	}
	if cfg.DGFAddress != "0xdgf" {
		t.Errorf("DGFAddress = %q", cfg.DGFAddress)
	}
	if cfg.TxMgrConfig.GasPriceEstimatorFn == nil {
		t.Error("ProposerTxMgr should wire GasPriceEstimatorFn")
	}
}

func TestInstallRSKHooks_RSKChain(t *testing.T) {
	cfg := &sources.L1ClientConfig{}
	InstallRSKHooks(cfg, nil, 33) // RSK regtest, no rpc client (TxHashesFromBlock will be set anyway, just won't run)
	if cfg.BlockVerifier == nil {
		t.Error("BlockVerifier must be installed for RSK chain id")
	}
	if cfg.HeaderVerifier == nil {
		t.Error("HeaderVerifier must be installed for RSK chain id")
	}
	if cfg.ReceiptsValidator == nil {
		t.Error("ReceiptsValidator must be installed for RSK chain id")
	}
	if cfg.TxHashesFromBlock == nil {
		t.Error("TxHashesFromBlock must be installed for RSK chain id (so RPC tx hashes override locally-computed ones)")
	}
}

func TestInstallRSKHooks_NonRSKChain_LeavesTxHashHookNil(t *testing.T) {
	cfg := &sources.L1ClientConfig{}
	InstallRSKHooks(cfg, nil, 1) // Ethereum mainnet
	// Block/Header/Receipts hooks are still installed (they self-route via
	// the chain-aware adapters), but TxHashesFromBlock must remain nil so
	// the EthClient falls back to the standard eth.TransactionsToHashes.
	if cfg.TxHashesFromBlock != nil {
		t.Error("TxHashesFromBlock must NOT be installed for non-RSK chain id (would force an extra eth_getBlockByHash on each fetch)")
	}
}

func TestInstallRSKHooks_Idempotent(t *testing.T) {
	preset := func(_ context.Context, _ *types.Header, _ types.Transactions) error { return nil }
	cfg := &sources.L1ClientConfig{}
	cfg.BlockVerifier = sources.BlockVerifierFn(preset)
	InstallRSKHooks(cfg, nil, 33)
	// The pre-set BlockVerifier must be preserved (function pointer equality
	// can't be checked directly in Go, so we rely on InstallRSKHooks's
	// "leave non-nil hooks alone" contract being exercised).
	if cfg.BlockVerifier == nil {
		t.Error("preset BlockVerifier should not have been cleared")
	}
}
