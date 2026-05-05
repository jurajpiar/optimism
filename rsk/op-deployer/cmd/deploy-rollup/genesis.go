package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

// GenesisJSON represents the genesis.json structure we generate
type GenesisJSON struct {
	Config     map[string]interface{}    `json:"config"`
	Nonce      string                    `json:"nonce"`
	Timestamp  string                    `json:"timestamp"`
	ExtraData  string                    `json:"extraData"`
	GasLimit   string                    `json:"gasLimit"`
	Difficulty string                    `json:"difficulty"`
	MixHash    string                    `json:"mixHash"`
	Coinbase   string                    `json:"coinbase"`
	Alloc      map[string]GenesisAccount `json:"alloc"`
}

// GenesisAccount represents an account in genesis alloc
type GenesisAccount struct {
	Balance string            `json:"balance,omitempty"`
	Nonce   string            `json:"nonce,omitempty"`
	Code    string            `json:"code,omitempty"`
	Storage map[string]string `json:"storage,omitempty"`
}

// ComputeGenesisBlockHash computes the L2 genesis block hash from genesis.json
// This uses go-ethereum's ToBlock() function which computes the state trie root
func ComputeGenesisBlockHash(genesisPath string) (string, error) {
	// Read genesis.json
	data, err := os.ReadFile(genesisPath)
	if err != nil {
		return "", fmt.Errorf("failed to read genesis file: %w", err)
	}

	// Parse into core.Genesis
	var genesis core.Genesis
	if err := json.Unmarshal(data, &genesis); err != nil {
		return "", fmt.Errorf("failed to parse genesis JSON: %w", err)
	}

	// ToBlock computes the genesis block with proper state root
	block := genesis.ToBlock()

	return block.Hash().Hex(), nil
}

// parseGenesisAlloc converts our GenesisJSON alloc format to core.GenesisAlloc
func parseGenesisAlloc(alloc map[string]GenesisAccount) (types.GenesisAlloc, error) {
	result := make(types.GenesisAlloc)

	for addrStr, account := range alloc {
		addr := common.HexToAddress(addrStr)

		ga := types.Account{}

		// Parse balance
		if account.Balance != "" {
			balance, ok := new(big.Int).SetString(account.Balance, 0)
			if !ok {
				// Try parsing as hex
				if len(account.Balance) > 2 && account.Balance[:2] == "0x" {
					balance, ok = new(big.Int).SetString(account.Balance[2:], 16)
				}
			}
			if ok {
				ga.Balance = balance
			}
		}

		// Parse nonce
		if account.Nonce != "" {
			nonce, err := hexutil.DecodeUint64(account.Nonce)
			if err == nil {
				ga.Nonce = nonce
			}
		}

		// Parse code
		if account.Code != "" {
			code, err := hexutil.Decode(account.Code)
			if err == nil {
				ga.Code = code
			}
		}

		// Parse storage
		if len(account.Storage) > 0 {
			ga.Storage = make(map[common.Hash]common.Hash)
			for keyStr, valStr := range account.Storage {
				key := common.HexToHash(keyStr)
				val := common.HexToHash(valStr)
				ga.Storage[key] = val
			}
		}

		result[addr] = ga
	}

	return result, nil
}
