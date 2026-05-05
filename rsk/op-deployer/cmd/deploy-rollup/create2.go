package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ethereum-optimism/optimism/rsk/op-deployer/redact"
)

const (
	// CREATE2 Deployer Constants (Arachnid's Deterministic Deployment Proxy)
	// See: https://github.com/Arachnid/deterministic-deployment-proxy
	CREATE2DeployerAddress = "0x4e59b44847b379578588920cA78FbF26c0B4956C"
	CREATE2DeployerSigner  = "0x3fab184622dc19b6109349b94811493bf2a45362"

	// Pre-signed raw transaction to deploy the CREATE2 factory (gas price: 100 gwei, gas limit: 100000)
	CREATE2DeployRawTx = "0xf8a58085174876e800830186a08080b853604580600e600039806000f350fe7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf31ba02222222222222222222222222222222222222222222222222222222222222222a02222222222222222222222222222222222222222222222222222222222222222"

	// Amount needed to fund signer: gas_limit * gas_price = 100000 * 100 gwei = 0.01 ETH
	CREATE2FundingAmount = "0.01ether"

	// RSK Cow account - pre-funded account in RSK regtest
	// Private key: 0xc85ef7d79691fe79573b1a7e708c0b39e2f92b2a10f015d38a7362a67f22f791
	RSKCowPrivateKey = "c85ef7d79691fe79573b1a7e708c0b39e2f92b2a10f015d38a7362a67f22f791"
)

// fundEOAIfNeeded checks EOA balance and funds it from funder account if below minimum
func fundEOAIfNeeded(ctx context.Context, cfg *Config) error {
	printStatus("Checking EOA balance for %s...", cfg.EOAAddress)

	// Get current balance
	balance, err := castBalance(ctx, cfg.EOAAddress, cfg.RPCURL)
	if err != nil {
		printStatus("Warning: failed to check EOA balance: %v", err)
		balance = "0"
	}

	printStatus("Current EOA balance: %s", balance)

	// Check if balance is sufficient (compare with min-balance)
	// Use cast to compare balances
	needsFunding, err := checkNeedsFunding(ctx, cfg.EOAAddress, cfg.MinBalance, cfg.RPCURL)
	if err != nil {
		printStatus("Warning: failed to check if funding needed: %v, will attempt to fund", err)
		needsFunding = true
	}

	if !needsFunding {
		printStatus("✓ EOA has sufficient balance (>= %s)", cfg.MinBalance)
		return nil
	}

	printStatus("EOA balance below minimum (%s), funding from funder account...", cfg.MinBalance)

	// Get funder address for logging
	funderAddr, err := getFunderAddress(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to get funder address: %w", err)
	}
	printStatus("Funder address: %s", funderAddr)

	// Check funder balance
	funderBalance, err := castBalance(ctx, funderAddr, cfg.RPCURL)
	if err != nil {
		printStatus("Warning: failed to check funder balance: %v", err)
	} else {
		printStatus("Funder balance: %s", funderBalance)
	}

	// Send funds from funder to EOA
	printStatus("Sending %s to %s...", cfg.FundAmount, cfg.EOAAddress)

	gasPrice, err := getGasPrice(ctx, cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("failed to get gas price: %w", err)
	}

	cmd := exec.CommandContext(ctx, "cast", "send",
		"--rpc-url="+cfg.RPCURL,
		"--private-key="+cfg.FunderPrivateKeyWith0x(),
		"--legacy",
		"--gas-price", gasPrice,
		cfg.EOAAddress,
		"--value", cfg.FundAmount,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to fund EOA: %w\nOutput: %s", err, redact.Output(string(output)))
	}

	printStatus("Transaction sent, waiting for confirmation...")
	time.Sleep(2 * time.Second)

	// Verify new balance
	newBalance, err := castBalance(ctx, cfg.EOAAddress, cfg.RPCURL)
	if err != nil {
		printStatus("Warning: failed to verify new balance: %v", err)
	} else {
		printStatus("✓ EOA funded successfully. New balance: %s", newBalance)
	}

	cfg.LogToMaster("Funded EOA %s with %s from funder", cfg.EOAAddress, cfg.FundAmount)

	return nil
}

// checkNeedsFunding checks if the address balance is below the minimum
func checkNeedsFunding(ctx context.Context, address, minBalance, rpcURL string) (bool, error) {
	// Use cast to get balance in wei and compare
	cmd := exec.CommandContext(ctx, "cast", "balance", address, "--rpc-url="+rpcURL)
	output, err := cmd.Output()
	if err != nil {
		return true, err
	}
	balance := strings.TrimSpace(string(output))

	// Convert minBalance to wei using cast
	cmd = exec.CommandContext(ctx, "cast", "to-wei", minBalance)
	output, err = cmd.Output()
	if err != nil {
		// Try parsing minBalance directly as wei
		return balance == "0" || balance == "", nil
	}
	minWei := strings.TrimSpace(string(output))

	// Compare as big integers using cast
	cmd = exec.CommandContext(ctx, "cast", "--to-dec", balance)
	balanceDecOutput, err := cmd.Output()
	if err != nil {
		return true, nil
	}
	balanceDec := strings.TrimSpace(string(balanceDecOutput))

	cmd = exec.CommandContext(ctx, "cast", "--to-dec", minWei)
	minDecOutput, err := cmd.Output()
	if err != nil {
		return true, nil
	}
	minDec := strings.TrimSpace(string(minDecOutput))

	// Simple string comparison won't work for large numbers, but we can use
	// a simple heuristic: if balance is 0 or empty, we need funding
	if balanceDec == "0" || balanceDec == "" {
		return true, nil
	}

	// For a proper comparison, we'd need big.Int, but for now use length comparison
	// (longer decimal string = larger number, assuming no leading zeros)
	if len(balanceDec) < len(minDec) {
		return true, nil
	}
	if len(balanceDec) > len(minDec) {
		return false, nil
	}
	// Same length, compare lexicographically
	return balanceDec < minDec, nil
}

// getFunderAddress derives the address from the funder private key
func getFunderAddress(ctx context.Context, cfg *Config) (string, error) {
	cmd := exec.CommandContext(ctx, "cast", "wallet", "address", "--private-key="+cfg.FunderPrivateKeyWith0x())
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// deployCREATE2IfNeeded checks if CREATE2 factory exists and deploys it if not
func deployCREATE2IfNeeded(ctx context.Context, cfg *Config) error {
	printStatus("Checking if CREATE2 Deployer exists at %s...", CREATE2DeployerAddress)

	logPath := cfg.LogPath("00_create2_deploy.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFile.Close()

	log := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintln(logFile, msg)
		printStatus("%s", msg)
	}

	// Check if code exists at the CREATE2 deployer address
	code, err := castCode(ctx, CREATE2DeployerAddress, cfg.RPCURL)
	if err != nil {
		log("Warning: failed to check CREATE2 deployer code: %v", err)
	}

	if code != "0x" && code != "" && code != "0x0" {
		log("✓ CREATE2 Deployer already exists at %s", CREATE2DeployerAddress)
		return nil
	}

	log("CREATE2 Deployer not found, deploying...")

	// Step 1: Check if the signer already has funds
	balance, err := castBalance(ctx, CREATE2DeployerSigner, cfg.RPCURL)
	if err != nil {
		log("Warning: failed to check signer balance: %v", err)
		balance = "0"
	}
	log("CREATE2 deployer signer balance: %s", balance)

	// Fund signer if needed
	if balance == "0" || balance == "0x0" {
		log("Funding deployment signer %s with %s...", CREATE2DeployerSigner, CREATE2FundingAmount)

		if err := fundCREATE2Signer(ctx, cfg, logFile); err != nil {
			return fmt.Errorf("failed to fund CREATE2 deployer signer: %w", err)
		}
		log("✓ Funded CREATE2 deployer signer")

		// Wait for transaction to be mined
		time.Sleep(2 * time.Second)
	} else {
		log("CREATE2 deployer signer already has funds")
	}

	// Step 2: Send the pre-signed raw transaction to deploy the factory
	log("Sending pre-signed deployment transaction...")

	if err := publishCREATE2Tx(ctx, cfg, logFile); err != nil {
		return fmt.Errorf("failed to deploy CREATE2 factory: %w", err)
	}

	// Step 3: Wait for transaction to be mined and verify deployment
	log("Waiting for transaction to be mined...")
	time.Sleep(3 * time.Second)

	code, err = castCode(ctx, CREATE2DeployerAddress, cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("failed to verify CREATE2 deployer: %w", err)
	}

	if code == "0x" || code == "" || code == "0x0" {
		return fmt.Errorf("CREATE2 Deployer deployment verification failed: expected code at %s but found: %s", CREATE2DeployerAddress, code)
	}

	log("✓ CREATE2 Deployer deployed successfully at %s", CREATE2DeployerAddress)
	cfg.LogToMaster("CREATE2 Deployer deployed at %s", CREATE2DeployerAddress)

	return nil
}

// castCode gets the code at an address using cast
func castCode(ctx context.Context, address, rpcURL string) (string, error) {
	cmd := exec.CommandContext(ctx, "cast", "code", address, "--rpc-url="+rpcURL)
	output, err := cmd.Output()
	if err != nil {
		return "0x", err
	}
	return strings.TrimSpace(string(output)), nil
}

// castBalance gets the balance of an address using cast
func castBalance(ctx context.Context, address, rpcURL string) (string, error) {
	cmd := exec.CommandContext(ctx, "cast", "balance", address, "--rpc-url="+rpcURL)
	output, err := cmd.Output()
	if err != nil {
		return "0", err
	}
	return strings.TrimSpace(string(output)), nil
}

// fundCREATE2Signer funds the CREATE2 deployer signer address
func fundCREATE2Signer(ctx context.Context, cfg *Config, logFile *os.File) error {
	gasPrice, err := getGasPrice(ctx, cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("failed to get gas price: %w", err)
	}

	// Try cast send first
	cmd := exec.CommandContext(ctx, "cast", "send",
		"--rpc-url="+cfg.RPCURL,
		"--private-key="+cfg.PrivateKeyWith0x(),
		"--legacy",
		"--gas-price", gasPrice,
		"--gas-limit", "21000",
		CREATE2DeployerSigner,
		"--value", CREATE2FundingAmount,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err == nil {
		return nil
	}

	printStatus("cast send failed, trying fallback method...")

	// Fallback: Get nonce and sign transaction manually
	nonce, err := getNonce(ctx, cfg.EOAAddress, cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("failed to get nonce: %w", err)
	}

	fmt.Fprintf(logFile, "Using nonce: %s\n", nonce)

	// Sign the transaction locally using cast mktx
	signedTx, err := signFundingTx(ctx, cfg, nonce, gasPrice)
	if err != nil {
		return fmt.Errorf("failed to sign funding transaction: %w", err)
	}

	fmt.Fprintf(logFile, "Signed funding transaction, broadcasting...\n")

	// Send raw transaction via RPC
	return sendRawTransaction(ctx, cfg.RPCURL, signedTx, logFile)
}

// publishCREATE2Tx publishes the pre-signed CREATE2 deployment transaction
func publishCREATE2Tx(ctx context.Context, cfg *Config, logFile *os.File) error {
	// Try cast publish first
	cmd := exec.CommandContext(ctx, "cast", "publish",
		"--rpc-url="+cfg.RPCURL,
		CREATE2DeployRawTx,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err == nil {
		return nil
	}

	printStatus("cast publish failed, trying direct RPC call...")

	// Fallback: Send raw transaction via RPC
	return sendRawTransaction(ctx, cfg.RPCURL, CREATE2DeployRawTx, logFile)
}

// getNonce gets the nonce for an address via RPC
func getNonce(ctx context.Context, address, rpcURL string) (string, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_getTransactionCount",
		"params":  []interface{}{address, "latest"},
		"id":      1,
	}

	resp, err := rpcCall(ctx, rpcURL, reqBody)
	if err != nil {
		return "", err
	}

	result, ok := resp["result"].(string)
	if !ok {
		return "0x0", nil
	}
	return result, nil
}

// signFundingTx signs a funding transaction using cast mktx
func signFundingTx(ctx context.Context, cfg *Config, nonce, gasPrice string) (string, error) {
	// Parse nonce from hex
	var nonceInt int64
	fmt.Sscanf(nonce, "0x%x", &nonceInt)

	cmd := exec.CommandContext(ctx, "cast", "mktx",
		"--rpc-url="+cfg.RPCURL,
		"--private-key="+cfg.PrivateKeyWith0x(),
		"--legacy",
		"--gas-price", gasPrice,
		"--gas-limit", "21000",
		"--nonce", fmt.Sprintf("%d", nonceInt),
		CREATE2DeployerSigner,
		"--value", CREATE2FundingAmount,
	)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("cast mktx failed: %s", string(exitErr.Stderr))
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// sendRawTransaction sends a raw transaction via RPC
func sendRawTransaction(ctx context.Context, rpcURL, signedTx string, logFile *os.File) error {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_sendRawTransaction",
		"params":  []interface{}{signedTx},
		"id":      1,
	}

	resp, err := rpcCall(ctx, rpcURL, reqBody)
	if err != nil {
		return err
	}

	// Log only the result hash or error — avoid dumping the full RPC response
	// which could contain sensitive data.
	if errVal, ok := resp["error"]; ok {
		fmt.Fprintf(logFile, "RPC response error: %v\n", errVal)
		return fmt.Errorf("RPC error: %v", errVal)
	}
	if result, ok := resp["result"]; ok {
		fmt.Fprintf(logFile, "RPC result: %v\n", result)
	}

	return nil
}

// rpcCall makes a JSON-RPC call
func rpcCall(ctx context.Context, rpcURL string, reqBody interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// getGasPrice queries eth_gasPrice from the node and returns the result as a
// decimal string (e.g. "7706359"). cast's --gas-price flag expects decimal,
// but eth_gasPrice returns hex, so we convert here.
// This is used to explicitly set --gas-price on cast send/mktx commands,
// preventing cast from applying its own gas price estimation which may
// exceed the RSK node's gas price cap.
func getGasPrice(ctx context.Context, rpcURL string) (string, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_gasPrice",
		"params":  []interface{}{},
		"id":      1,
	}
	resp, err := rpcCall(ctx, rpcURL, reqBody)
	if err != nil {
		return "", fmt.Errorf("eth_gasPrice RPC failed: %w", err)
	}
	hex, ok := resp["result"].(string)
	if !ok {
		return "", fmt.Errorf("unexpected eth_gasPrice result: %v", resp["result"])
	}
	// Strip 0x prefix and parse as hex integer, then return decimal string.
	hex = strings.TrimPrefix(hex, "0x")
	val, ok := new(big.Int).SetString(hex, 16)
	if !ok {
		return "", fmt.Errorf("failed to parse gas price hex value: 0x%s", hex)
	}
	return val.String(), nil
}

// fundRoleAccountsIfNeeded funds the derived batcher and proposer addresses
// from the deployer (EOA) key when --fund-roles is set.
func fundRoleAccountsIfNeeded(ctx context.Context, cfg *Config) error {
	type roleAccount struct {
		name    string
		address string
	}

	roles := []roleAccount{}
	if cfg.Batcher != "" && cfg.Batcher != cfg.EOAAddress {
		roles = append(roles, roleAccount{"batcher", cfg.Batcher})
	}
	if cfg.Proposer != "" && cfg.Proposer != cfg.EOAAddress {
		roles = append(roles, roleAccount{"proposer", cfg.Proposer})
	}

	if len(roles) == 0 {
		printStatus("No separate role accounts to fund (all roles use EOA)")
		return nil
	}

	for _, role := range roles {
		printStatus("Checking %s account %s...", role.name, role.address)

		needsFunding, err := checkNeedsFunding(ctx, role.address, cfg.MinBalance, cfg.RPCURL)
		if err != nil {
			printStatus("Warning: failed to check %s balance: %v, will attempt to fund", role.name, err)
			needsFunding = true
		}

		if !needsFunding {
			printStatus("✓ %s account has sufficient balance", role.name)
			continue
		}

		printStatus("Funding %s account %s with %s...", role.name, role.address, cfg.FundAmount)

		gasPrice, err := getGasPrice(ctx, cfg.RPCURL)
		if err != nil {
			return fmt.Errorf("failed to get gas price: %w", err)
		}

		cmd := exec.CommandContext(ctx, "cast", "send",
			"--rpc-url="+cfg.RPCURL,
			"--private-key="+cfg.PrivateKeyWith0x(),
			"--legacy",
			"--gas-price", gasPrice,
			role.address,
			"--value", cfg.FundAmount,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to fund %s account: %w\nOutput: %s", role.name, err, redact.Output(string(output)))
		}

		printStatus("Transaction sent, waiting for confirmation...")
		time.Sleep(2 * time.Second)

		newBalance, err := castBalance(ctx, role.address, cfg.RPCURL)
		if err != nil {
			printStatus("Warning: failed to verify %s balance: %v", role.name, err)
		} else {
			printStatus("✓ %s account funded. Balance: %s", role.name, newBalance)
		}

		cfg.LogToMaster("Funded %s account %s with %s", role.name, role.address, cfg.FundAmount)
	}

	return nil
}
