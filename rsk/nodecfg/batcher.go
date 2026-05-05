package nodecfg

import (
	"time"

	rsktxmgr "github.com/ethereum-optimism/optimism/rsk/txmgr"

	"github.com/ethereum-optimism/optimism/op-batcher/batcher"
	batcherconfig "github.com/ethereum-optimism/optimism/op-batcher/config"
	"github.com/ethereum-optimism/optimism/op-batcher/flags"
	"github.com/ethereum-optimism/optimism/op-node/rollup/derive"
	optxmgr "github.com/ethereum-optimism/optimism/op-service/txmgr"
)

// BatcherTxMgr returns a txmgr.CLIConfig with all the RSK-specific overrides
// the batcher needs:
//   - legacy tx (RSK has no EIP-1559)
//   - RSK gas estimator (eth_gasPrice + minimum legacy floor)
//   - long polling/rebroadcast intervals tuned for ~30 s blocks
//   - RSK "already published" error matchers
//
// l1RPC is the RSK L1 RPC URL; privateKey is hex-encoded with optional 0x prefix.
func BatcherTxMgr(l1RPC, privateKey string) optxmgr.CLIConfig {
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

// BatcherThrottle returns a ThrottleConfig that disables throttling — RSK
// has no equivalent of miner_setMaxDASize, so any non-zero threshold would
// stall the batcher when the RSK node refuses the call.
func BatcherThrottle() batcher.ThrottleConfig {
	return batcher.ThrottleConfig{
		ControllerType:      batcherconfig.StepControllerType,
		LowerThreshold:      0,
		UpperThreshold:      0,
		TxSizeLowerLimit:    0,
		TxSizeUpperLimit:    0,
		BlockSizeLowerLimit: 0,
		BlockSizeUpperLimit: 0,
	}
}

// BatcherDefaults returns a fully-populated batcher.CLIConfig with all the
// RSK-relevant defaults (TxMgr, Throttle, calldata-only DA, singular batch
// type, shadow compressor). The caller still sets endpoints, ports, log
// config, etc. on the returned struct.
//
// l1RPC + privateKey are passed through to BatcherTxMgr.
func BatcherDefaults(l1RPC, privateKey string) batcher.CLIConfig {
	return batcher.CLIConfig{
		L1EthRpc:               l1RPC,
		MaxChannelDuration:     0,
		SubSafetyMargin:        10,
		PollInterval:           2 * time.Second,
		MaxPendingTransactions: 1,
		MaxL1TxSize:            120000,
		MaxBlocksPerSpanBatch:  0,
		TargetNumFrames:        1,
		ApproxComprRatio:       0.4,
		Compressor:             "shadow",
		CompressionAlgo:        derive.Zlib,
		Stopped:                false,
		WaitNodeSync:           false,
		CheckRecentTxsDepth:    0,
		BatchType:              0,                  // SingularBatch
		DataAvailabilityType:   flags.CalldataType, // RSK doesn't support blobs
		TxMgrConfig:            BatcherTxMgr(l1RPC, privateKey),
		ThrottleConfig:         BatcherThrottle(),
	}
}

// ApplyBatcherRSK overrides RSK-required fields on a CLIConfig that was built
// from upstream flags. Use it in cmd/rsk-op-batcher to ride on top of the
// upstream batcher.NewConfig CLI flag plumbing while forcing the RSK pieces
// that have no CLI representation (legacy tx, RSK gas estimator, RSK error
// matchers, no throttling, calldata-only DA, singular batches).
//
// L1RPCURL, PrivateKey, L1EthRpc and other operator-supplied flags on cfg
// are preserved.
func ApplyBatcherRSK(cfg *batcher.CLIConfig) {
	cfg.BatchType = 0
	cfg.DataAvailabilityType = flags.CalldataType
	cfg.ThrottleConfig = BatcherThrottle()
	applyRSKToTxMgr(&cfg.TxMgrConfig)
}
