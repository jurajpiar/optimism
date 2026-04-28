package rsktrie

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"

	"gorsk/rskblocks"
)

// EncodeRSKTransactions converts go-ethereum transactions to RSK-format RLP
// encoded byte slices suitable for building an RSK binary unitrie.
func EncodeRSKTransactions(txs types.Transactions) ([]hexutil.Bytes, error) {
	out := make([]hexutil.Bytes, len(txs))
	for i, tx := range txs {
		rskTx := convertTxToRSK(tx)
		encoded, err := rlp.EncodeToBytes(rskTx)
		if err != nil {
			return nil, fmt.Errorf("encode RSK tx %d: %w", i, err)
		}
		out[i] = encoded
	}
	return out, nil
}

// EncodeRSKReceipts converts go-ethereum receipts to RSK-format RLP
// encoded byte slices suitable for building an RSK binary unitrie.
func EncodeRSKReceipts(receipts types.Receipts) ([]hexutil.Bytes, error) {
	out := make([]hexutil.Bytes, len(receipts))
	for i, r := range receipts {
		rskR := convertReceiptToRSK(r)
		encoded, err := rlp.EncodeToBytes(rskR)
		if err != nil {
			return nil, fmt.Errorf("encode RSK receipt %d: %w", i, err)
		}
		out[i] = encoded
	}
	return out, nil
}

// DecodeRSKTransactions decodes RSK-format RLP transaction byte slices
// (as read from a binary unitrie) back into go-ethereum Transactions.
func DecodeRSKTransactions(opaque []hexutil.Bytes) (types.Transactions, error) {
	txs := make(types.Transactions, len(opaque))
	for i, raw := range opaque {
		var rskTx rskblocks.Transaction
		if err := rlp.DecodeBytes(raw, &rskTx); err != nil {
			return nil, fmt.Errorf("decode RSK tx %d: %w", i, err)
		}
		txs[i] = convertTxFromRSK(&rskTx)
	}
	return txs, nil
}

// DecodeRSKReceipts decodes RSK-format RLP receipt byte slices
// (as read from a binary unitrie) back into go-ethereum Receipts.
func DecodeRSKReceipts(opaque []hexutil.Bytes) (types.Receipts, error) {
	receipts := make(types.Receipts, len(opaque))
	for i, raw := range opaque {
		var rskR rskblocks.TransactionReceipt
		if err := rlp.DecodeBytes(raw, &rskR); err != nil {
			return nil, fmt.Errorf("decode RSK receipt %d: %w", i, err)
		}
		receipts[i] = convertReceiptFromRSK(&rskR)
	}
	return receipts, nil
}

// convertTxToRSK converts a go-ethereum Transaction to an RSK Transaction.
func convertTxToRSK(tx *types.Transaction) *rskblocks.Transaction {
	v, r, s := tx.RawSignatureValues()
	var to *common.Address
	if tx.To() != nil {
		addr := *tx.To()
		to = &addr
	}
	return rskblocks.NewSignedTransaction(
		tx.Nonce(), to, tx.Value(), tx.Gas(), tx.GasPrice(), tx.Data(),
		v, r, s,
	)
}

// convertReceiptToRSK converts a go-ethereum Receipt to an RSK TransactionReceipt.
func convertReceiptToRSK(r *types.Receipt) *rskblocks.TransactionReceipt {
	logs := make([]*rskblocks.Log, len(r.Logs))
	for i, log := range r.Logs {
		logs[i] = &rskblocks.Log{
			Address: log.Address,
			Topics:  log.Topics,
			Data:    log.Data,
		}
	}

	var postState, status []byte
	if len(r.PostState) > 0 {
		postState = r.PostState
	} else if r.Status == types.ReceiptStatusSuccessful {
		postState = []byte{0x01}
	}
	if r.Status == types.ReceiptStatusSuccessful {
		status = []byte{0x01}
	}

	return &rskblocks.TransactionReceipt{
		PostState:         postState,
		CumulativeGasUsed: r.CumulativeGasUsed,
		Bloom:             r.Bloom,
		Logs:              logs,
		TxHash:            r.TxHash,
		ContractAddress:   r.ContractAddress,
		GasUsed:           r.GasUsed,
		Status:            status,
	}
}

// convertTxFromRSK converts an RSK Transaction back to a go-ethereum Transaction.
func convertTxFromRSK(rskTx *rskblocks.Transaction) *types.Transaction {
	nonce := rskTx.Nonce()
	gasPrice := rskTx.GasPrice()
	gas := rskTx.Gas()
	to := rskTx.To()
	value := rskTx.Value()
	data := rskTx.Data()
	v, r, s := rskTx.RawSignatureValues()

	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	if value == nil {
		value = new(big.Int)
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gas,
		To:       to,
		Value:    value,
		Data:     data,
		V:        v,
		R:        r,
		S:        s,
	})
	return tx
}

// convertReceiptFromRSK converts an RSK TransactionReceipt back to a go-ethereum Receipt.
func convertReceiptFromRSK(rskR *rskblocks.TransactionReceipt) *types.Receipt {
	logs := make([]*types.Log, len(rskR.Logs))
	for i, l := range rskR.Logs {
		logs[i] = &types.Log{
			Address: l.Address,
			Topics:  l.Topics,
			Data:    l.Data,
		}
	}

	var status uint64
	if len(rskR.Status) > 0 && rskR.Status[0] == 0x01 {
		status = types.ReceiptStatusSuccessful
	} else {
		status = types.ReceiptStatusFailed
	}

	return &types.Receipt{
		PostState:         rskR.PostState,
		Status:            status,
		CumulativeGasUsed: rskR.CumulativeGasUsed,
		Bloom:             rskR.Bloom,
		Logs:              logs,
		TxHash:            rskR.TxHash,
		ContractAddress:   rskR.ContractAddress,
		GasUsed:           rskR.GasUsed,
	}
}
