package txmgr

import (
	"math/big"

	rskethclient "github.com/smishraIOV/gorsk/ethclient"

	optxmgr "github.com/ethereum-optimism/optimism/op-service/txmgr"
)

// DefaultMinLegacyGasPriceWei is the floor applied to RSK's eth_gasPrice
// result before it is used as the legacy GasPrice. It mirrors the value
// previously hard-coded in cmd/rollup-node (7.5M wei = 0.0075 gwei).
//
// RSK's minimum gas price drifts slowly relative to Ethereum's baseFee, but
// occasionally dips below the floor that makes batcher/proposer txs land
// reliably in the next block; the floor avoids underpriced txs.
var DefaultMinLegacyGasPriceWei = big.NewInt(7_500_000)

// DefaultGasPriceEstimator returns a GasPriceEstimatorFn suitable for the
// op-service/txmgr running against RSK L1: it calls eth_gasPrice and floors
// the result at DefaultMinLegacyGasPriceWei.
//
// Combine with txmgr.Config.UseLegacyTx = true so the manager produces type-0
// transactions (RSK doesn't support EIP-1559 / EIP-4844).
func DefaultGasPriceEstimator() optxmgr.GasPriceEstimatorFn {
	return rskethclient.RSKGasPriceEstimatorFnWithMinimumLegacyGasPrice(DefaultMinLegacyGasPriceWei)
}

// GasPriceEstimatorWithMinWei returns a GasPriceEstimatorFn that floors the
// reported legacy gas price at minLegacyWei. Pass nil or non-positive to
// disable the floor.
func GasPriceEstimatorWithMinWei(minLegacyWei *big.Int) optxmgr.GasPriceEstimatorFn {
	return rskethclient.RSKGasPriceEstimatorFnWithMinimumLegacyGasPrice(minLegacyWei)
}
