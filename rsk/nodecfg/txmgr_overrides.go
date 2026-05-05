package nodecfg

import (
	rsktxmgr "github.com/ethereum-optimism/optimism/rsk/txmgr"

	optxmgr "github.com/ethereum-optimism/optimism/op-service/txmgr"
)

// applyRSKToTxMgr patches the txmgr fields that have no upstream CLI flag
// representation (function-typed estimator, error matchers) plus those that
// must be forced for RSK regardless of what the operator passed (legacy tx,
// no tip, no base-fee floor).
//
// Used by ApplyBatcherRSK and ApplyProposerRSK.
func applyRSKToTxMgr(cfg *optxmgr.CLIConfig) {
	cfg.UseLegacyTx = true
	cfg.GasPriceEstimatorFn = rsktxmgr.DefaultGasPriceEstimator()
	cfg.AlreadyPublishedCustomErrs = rsktxmgr.AlreadyPublishedErrs
	cfg.MinTipCapGwei = 0
	cfg.MinBaseFeeGwei = 0
}
