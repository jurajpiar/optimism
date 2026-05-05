package nodecfg

import (
	"time"

	rsktxmgr "github.com/ethereum-optimism/optimism/rsk/txmgr"

	ps "github.com/ethereum-optimism/optimism/op-proposer/proposer"
	optxmgr "github.com/ethereum-optimism/optimism/op-service/txmgr"
)

// ProposerTxMgr returns a txmgr.CLIConfig with the RSK overrides used by the
// L2 output proposer. Same shape as BatcherTxMgr; kept separate so the two
// can drift independently if RSK demands diverge.
func ProposerTxMgr(l1RPC, privateKey string) optxmgr.CLIConfig {
	return optxmgr.CLIConfig{
		L1RPCURL:                   l1RPC,
		PrivateKey:                 privateKey,
		NumConfirmations:           1,
		SafeAbortNonceTooLowCount:  3,
		FeeLimitMultiplier:         5,
		FeeLimitThresholdGwei:      100.0,
		MinTipCapGwei:              0,
		MinBaseFeeGwei:             0,
		RebroadcastInterval:        35 * time.Second,
		ResubmissionTimeout:        90 * time.Second,
		NetworkTimeout:             25 * time.Second,
		RetryInterval:              5 * time.Second,
		MaxRetries:                 10,
		TxSendTimeout:              0,
		TxNotInMempoolTimeout:      3 * time.Minute,
		ReceiptQueryInterval:       5 * time.Second,
		GasPriceEstimatorFn:        rsktxmgr.DefaultGasPriceEstimator(),
		UseLegacyTx:                true,
		AlreadyPublishedCustomErrs: rsktxmgr.AlreadyPublishedErrs,
		WrapBackend:                rsktxmgr.WrapRateLimit(2, 10),
		PrepareBackoff:             rsktxmgr.RevertAwareBackoff(rsktxmgr.DefaultRSKL1BlockTime),
	}
}

// ApplyProposerRSK overrides RSK-required fields on a CLIConfig that was
// built from upstream proposer flags. Use it in cmd/rsk-op-proposer.
//
// AllowNonFinalized is forced to true (RSK has no finality beacon), and the
// txmgr is patched via applyRSKToTxMgr.
func ApplyProposerRSK(cfg *ps.CLIConfig) {
	cfg.AllowNonFinalized = true
	applyRSKToTxMgr(&cfg.TxMgrConfig)
}

// ProposerDefaults returns a fully-populated ps.CLIConfig with the RSK
// overrides: AllowNonFinalized=true (RSK has no finality beacon),
// PollInterval/ProposalInterval tuned for ~30 s blocks, dispute game type 1.
//
// dgfAddress is the DisputeGameFactory contract address on L1 (hex string).
func ProposerDefaults(l1RPC, privateKey, dgfAddress string) ps.CLIConfig {
	return ps.CLIConfig{
		L1EthRpc:                     l1RPC,
		PollInterval:                 2 * time.Second,
		AllowNonFinalized:            true,
		DGFAddress:                   dgfAddress,
		ProposalInterval:             2 * time.Minute,
		DisputeGameType:              1,
		ActiveSequencerCheckDuration: 5 * time.Second,
		WaitNodeSync:                 false,
		TxMgrConfig:                  ProposerTxMgr(l1RPC, privateKey),
	}
}
