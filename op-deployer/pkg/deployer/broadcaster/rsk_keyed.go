package broadcaster

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/holiman/uint256"

	gorskClient "gorsk/ethclient"

	"github.com/ethereum-optimism/optimism/op-chain-ops/script"
	opcrypto "github.com/ethereum-optimism/optimism/op-service/crypto"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum-optimism/optimism/op-service/txmgr"
	"github.com/ethereum-optimism/optimism/op-service/txmgr/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/log"
	"github.com/hashicorp/go-multierror"
)

// RSKKeyedBroadcaster is a broadcaster for RSK chains.
// It uses the gorsk/ethclient for RSK-specific functionality including:
//   - Legacy gas pricing (no EIP-1559)
//   - RSK-specific timing (30-second block time)
//   - No blob transaction support
type RSKKeyedBroadcaster struct {
	lgr    log.Logger
	mgr    txmgr.TxManager
	bcasts []script.Broadcast
	client *gorskClient.Client
	mtx    sync.Mutex
}

// RSKKeyedBroadcasterOpts contains options for creating an RSK broadcaster.
type RSKKeyedBroadcasterOpts struct {
	Logger  log.Logger
	ChainID *big.Int
	RPCUrl  string
	Signer  opcrypto.SignerFn
	From    common.Address
}

// NewRSKKeyedBroadcaster creates a new broadcaster configured for RSK networks.
// It uses RSK-specific gas estimation and timing parameters.
func NewRSKKeyedBroadcaster(cfg RSKKeyedBroadcasterOpts) (*RSKKeyedBroadcaster, error) {
	// Connect to RSK node using gorsk client
	client, err := gorskClient.Dial(cfg.RPCUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RSK node: %w", err)
	}

	// Create RSK-specific txmgr config
	mgrCfg := &txmgr.Config{
		Backend:                   client, // gorsk client implements txmgr.ETHBackend
		ChainID:                   cfg.ChainID,
		TxSendTimeout:             5 * time.Minute,
		TxNotInMempoolTimeout:     3 * time.Minute,
		NetworkTimeout:            10 * time.Second,
		ReceiptQueryInterval:      30 * time.Second, // RSK block time
		NumConfirmations:          6,                // ~3 minutes at 30s blocks
		SafeAbortNonceTooLowCount: 3,
		Signer:                    cfg.Signer,
		From:                      cfg.From,
		GasPriceEstimatorFn:       RSKDeployerGasPriceEstimator,
	}

	// RSK-specific fee configuration
	minTipCap, err := eth.GweiToWei(0.06) // RSK minimum gas price
	if err != nil {
		panic(err)
	}
	minBaseFee, err := eth.GweiToWei(0.06) // RSK minimum gas price
	if err != nil {
		panic(err)
	}

	// Set atomic config values for RSK timing
	mgrCfg.RebroadcastInterval.Store(int64(30 * time.Second)) // RSK block time
	mgrCfg.ResubmissionTimeout.Store(int64(90 * time.Second)) // ~3 blocks
	mgrCfg.FeeLimitMultiplier.Store(5)
	mgrCfg.FeeLimitThreshold.Store(big.NewInt(100))
	mgrCfg.MinTipCap.Store(minTipCap)
	mgrCfg.MinBaseFee.Store(minBaseFee)
	mgrCfg.MinBlobTxFee.Store(big.NewInt(1)) // Dummy - RSK doesn't support blobs

	mgr, err := txmgr.NewSimpleTxManagerFromConfig(
		"rsk-transactor",
		cfg.Logger,
		&metrics.NoopTxMetrics{},
		mgrCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create RSK tx manager: %w", err)
	}

	return &RSKKeyedBroadcaster{
		lgr:    cfg.Logger,
		mgr:    mgr,
		client: client,
	}, nil
}

// Hook implements the Broadcaster interface.
func (t *RSKKeyedBroadcaster) Hook(bcast script.Broadcast) {
	if bcast.Type != script.BroadcastCreate2 && bcast.From != t.mgr.From() {
		panic(fmt.Sprintf("invalid from for broadcast:%v, expected:%v", bcast.From, t.mgr.From()))
	}
	t.mtx.Lock()
	t.bcasts = append(t.bcasts, bcast)
	t.mtx.Unlock()
}

// Broadcast implements the Broadcaster interface.
func (t *RSKKeyedBroadcaster) Broadcast(ctx context.Context) ([]BroadcastResult, error) {
	t.mtx.Lock()
	bcasts := t.bcasts
	t.bcasts = nil
	t.mtx.Unlock()

	if len(bcasts) == 0 {
		return nil, nil
	}

	results := make([]BroadcastResult, len(bcasts))

	latestHeader, err := t.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest block header: %w", err)
	}

	var txErr *multierror.Error
	for i, bcast := range bcasts {
		t.lgr.Info(
			"RSK transaction broadcasting",
			"index", i,
			"total", len(bcasts),
			"nonce", bcast.Nonce,
		)

		bcastRes, id := t.broadcast(ctx, bcast, latestHeader.GasLimit)

		t.lgr.Info(
			"RSK transaction broadcasted",
			"id", id,
			"nonce", bcast.Nonce,
		)

		outRes := BroadcastResult{
			Broadcast: bcasts[i],
		}

		if bcastRes.Err == nil {
			outRes.Receipt = bcastRes.Receipt
			outRes.TxHash = bcastRes.Receipt.TxHash

			if bcastRes.Receipt.Status == 0 {
				failErr := fmt.Errorf("RSK transaction failed: %s", outRes.Receipt.TxHash.String())
				txErr = multierror.Append(txErr, failErr)
				outRes.Err = failErr
				t.lgr.Error(
					"RSK transaction failed on chain",
					"id", id,
					"completed", i+1,
					"total", len(bcasts),
					"hash", outRes.Receipt.TxHash.String(),
					"nonce", outRes.Broadcast.Nonce,
				)
			} else {
				t.lgr.Info(
					"RSK transaction confirmed",
					"id", id,
					"completed", i+1,
					"total", len(bcasts),
					"hash", outRes.Receipt.TxHash.String(),
					"nonce", outRes.Broadcast.Nonce,
					"creation", outRes.Receipt.ContractAddress,
				)
			}
		} else {
			txErr = multierror.Append(txErr, bcastRes.Err)
			outRes.Err = bcastRes.Err
			t.lgr.Error(
				"RSK transaction failed",
				"id", id,
				"completed", i+1,
				"total", len(bcasts),
				"err", bcastRes.Err,
			)
		}

		results[i] = outRes
	}
	return results, txErr.ErrorOrNil()
}

func (t *RSKKeyedBroadcaster) broadcast(ctx context.Context, bcast script.Broadcast, blockGasLimit uint64) (txmgr.SendResponse, common.Hash) {
	id := bcast.ID()
	candidate := rskAsTxCandidate(bcast, blockGasLimit)
	receipt, err := t.mgr.Send(ctx, candidate)
	return txmgr.SendResponse{Receipt: receipt, Err: err}, id
}

// rskAsTxCandidate converts a broadcast to a transaction candidate for RSK.
// RSK has lower gas limits than Ethereum, so we use the same padding logic.
func rskAsTxCandidate(bcast script.Broadcast, blockGasLimit uint64) txmgr.TxCandidate {
	value := ((*uint256.Int)(bcast.Value)).ToBig()
	var candidate txmgr.TxCandidate
	switch bcast.Type {
	case script.BroadcastCall:
		to := &bcast.To
		candidate = txmgr.TxCandidate{
			TxData:   bcast.Input,
			To:       to,
			Value:    value,
			GasLimit: rskPadGasLimit(bcast.Input, bcast.GasUsed, false, blockGasLimit),
		}
	case script.BroadcastCreate:
		candidate = txmgr.TxCandidate{
			TxData:   bcast.Input,
			To:       nil,
			GasLimit: rskPadGasLimit(bcast.Input, bcast.GasUsed, true, blockGasLimit),
		}
	case script.BroadcastCreate2:
		txData := make([]byte, len(bcast.Salt)+len(bcast.Input))
		copy(txData, bcast.Salt[:])
		copy(txData[len(bcast.Salt):], bcast.Input)

		candidate = txmgr.TxCandidate{
			TxData:   txData,
			To:       &script.DeterministicDeployerAddress,
			Value:    value,
			GasLimit: rskPadGasLimit(bcast.Input, bcast.GasUsed, true, blockGasLimit),
		}
	default:
		panic(fmt.Sprintf("unrecognized broadcast type: '%s'", bcast.Type))
	}
	return candidate
}

// rskPadGasLimit calculates the gas limit for an RSK transaction.
// Uses the same logic as the standard broadcaster but may be adjusted for RSK specifics.
func rskPadGasLimit(data []byte, gasUsed uint64, creation bool, blockGasLimit uint64) uint64 {
	intrinsicGas, err := core.IntrinsicGas(data, nil, nil, creation, true, true, false)
	if err != nil {
		panic(err)
	}

	floorDataGas, err := core.FloorDataGas(data)
	if err != nil {
		panic(err)
	}

	gas := intrinsicGas + gasUsed
	if floorDataGas > gas {
		gas = floorDataGas
	}

	// Use standard padding factor for RSK
	limit := uint64(float64(gas) * GasPadFactor)
	if limit > blockGasLimit {
		return blockGasLimit
	}
	return limit
}

// Client returns the underlying RSK client.
func (t *RSKKeyedBroadcaster) Client() *gorskClient.Client {
	return t.client
}

// Close closes the RSK client connection.
func (t *RSKKeyedBroadcaster) Close() {
	t.client.Close()
}
