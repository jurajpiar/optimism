package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum-optimism/optimism/rsk/op-deployer/redact"
)

// ForgeExecutor handles execution of forge scripts
type ForgeExecutor struct {
	cfg          *Config
	contractsDir string
}

// NewForgeExecutor creates a new forge executor
func NewForgeExecutor(cfg *Config) *ForgeExecutor {
	return &ForgeExecutor{
		cfg:          cfg,
		contractsDir: cfg.ContractsDir(),
	}
}

// ForgeScriptInput holds input configuration for a forge script
type ForgeScriptInput struct {
	// Script path relative to contracts-bedrock/scripts/deploy/
	Script string
	// Signature to call (e.g., "run(string)")
	Sig string
	// Arguments to pass to the signature
	SigArgs []string
	// Environment variables to set
	Env map[string]string
	// WorkingDir overrides the forge project root (defaults to the
	// contracts-bedrock package). Set this to run scripts from out-of-tree
	// forge packages such as oprsk-contracts/ — when set, Script and the
	// resulting broadcast/cache directories are resolved relative to it.
	WorkingDir string
}

// ForgeScriptResult holds the result of running a forge script
type ForgeScriptResult struct {
	Success      bool
	Output       string
	BroadcastDir string
	SigName      string // Function name from signature (e.g., "run", "runSplit")
	Duration     time.Duration
}

// RunScript executes a forge script with RSK-compatible flags
func (f *ForgeExecutor) RunScript(ctx context.Context, input ForgeScriptInput, logger *StepLogger) (*ForgeScriptResult, error) {
	startTime := time.Now()

	// Build forge command with RSK-specific flags.
	//
	// --gas-estimate-multiplier 110: Forge's default 130% padding pushes
	// individual contract deploys (e.g. OPContractsManagerStandardValidator,
	// OPContractsManagerV2, OptimismPortal2) above RSKj's RSKIP144 per-tx
	// sublist gas cap of 6.8M, even though the unpadded estimates fit. 10%
	// padding keeps the worst-case raw estimate (~6.0M) under 6.8M while
	// still leaving enough cushion to avoid OOG on the actual broadcast.
	args := []string{
		"script",
		input.Script,
		"--rpc-url", f.cfg.RPCURL,
		"--private-key", f.cfg.PrivateKeyWith0x(),
		"--broadcast",
		"--legacy",                       // RSK: Use legacy transaction format (no EIP-1559)
		"--slow",                         // RSK: Synchronous mode - wait for each tx to be mined
		"--gas-estimate-multiplier", "110", // RSK: keep padded gas under 6.8M sublist cap
		"-vvvv",                          // Verbose output for debugging
	}

	// Add signature and arguments if provided
	if input.Sig != "" {
		args = append(args, "--sig", input.Sig)
		args = append(args, input.SigArgs...)
	}

	// Resolve the forge project root for this invocation. Out-of-tree
	// callers (e.g. oprsk-contracts/) override f.contractsDir via WorkingDir.
	workingDir := f.contractsDir
	if input.WorkingDir != "" {
		workingDir = input.WorkingDir
	}

	// Ensure forge's deployment-sequence cache directory exists.
	// Forge writes to cache/<ScriptName>/<ChainID>/ but does not mkdir -p first.
	scriptBase := input.Script
	if idx := strings.Index(scriptBase, ":"); idx != -1 {
		scriptBase = scriptBase[:idx]
	}
	scriptBase = filepath.Base(scriptBase)
	cacheDir := filepath.Join(workingDir, "cache", scriptBase, fmt.Sprintf("%d", f.cfg.L1ChainID))
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		logger.Printf("Warning: failed to create cache dir %s: %v\n", cacheDir, err)
	}

	cmd := exec.CommandContext(ctx, "forge", args...)
	cmd.Dir = workingDir

	// Set up environment
	cmd.Env = os.Environ()
	if f.cfg.FoundryProfile != "" {
		cmd.Env = append(cmd.Env, "FOUNDRY_PROFILE="+f.cfg.FoundryProfile)
	}
	for k, v := range input.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Log command being executed (with secrets redacted)
	cmdStr := redact.Command(append([]string{"forge"}, args...))
	logger.Printf("=== Executing: %s ===\n", cmdStr)
	logger.Printf("=== Working directory: %s ===\n", workingDir)
	logger.Printf("=== Started at: %s ===\n\n", time.Now().Format(time.RFC3339))

	// Capture both stdout and stderr
	var outputBuffer bytes.Buffer
	multiWriter := io.MultiWriter(&outputBuffer, logger)
	cmd.Stdout = multiWriter
	cmd.Stderr = multiWriter

	// Run the command
	err := cmd.Run()
	duration := time.Since(startTime)

	// Extract function name from signature (e.g., "runSplit(...)" -> "runSplit")
	sigName := "run" // default
	if input.Sig != "" {
		if idx := strings.Index(input.Sig, "("); idx > 0 {
			sigName = input.Sig[:idx]
		}
	}

	result := &ForgeScriptResult{
		Success:  err == nil,
		Output:   outputBuffer.String(),
		SigName:  sigName,
		Duration: duration,
	}

	// Find broadcast directory (rooted at the script's working dir).
	result.BroadcastDir = f.findBroadcastDirIn(workingDir, input.Script)

	logger.Printf("\n=== Completed in %.1fs (success=%v) ===\n", duration.Seconds(), result.Success)

	if err != nil {
		return result, fmt.Errorf("forge script failed: %w", err)
	}

	return result, nil
}

// L2GenesisInput matches the Input struct in L2Genesis.s.sol
type L2GenesisInput struct {
	L1ChainID                                uint64 `json:"l1ChainID"`
	L2ChainID                                uint64 `json:"l2ChainID"`
	L1CrossDomainMessengerProxy              string `json:"l1CrossDomainMessengerProxy"`
	L1StandardBridgeProxy                    string `json:"l1StandardBridgeProxy"`
	L1ERC721BridgeProxy                      string `json:"l1ERC721BridgeProxy"`
	OpChainProxyAdminOwner                   string `json:"opChainProxyAdminOwner"`
	SequencerFeeVaultRecipient               string `json:"sequencerFeeVaultRecipient"`
	SequencerFeeVaultMinimumWithdrawalAmount uint64 `json:"sequencerFeeVaultMinimumWithdrawalAmount"`
	SequencerFeeVaultWithdrawalNetwork       uint64 `json:"sequencerFeeVaultWithdrawalNetwork"`
	BaseFeeVaultRecipient                    string `json:"baseFeeVaultRecipient"`
	BaseFeeVaultMinimumWithdrawalAmount      uint64 `json:"baseFeeVaultMinimumWithdrawalAmount"`
	BaseFeeVaultWithdrawalNetwork            uint64 `json:"baseFeeVaultWithdrawalNetwork"`
	L1FeeVaultRecipient                      string `json:"l1FeeVaultRecipient"`
	L1FeeVaultMinimumWithdrawalAmount        uint64 `json:"l1FeeVaultMinimumWithdrawalAmount"`
	L1FeeVaultWithdrawalNetwork              uint64 `json:"l1FeeVaultWithdrawalNetwork"`
	OperatorFeeVaultRecipient                string `json:"operatorFeeVaultRecipient"`
	OperatorFeeVaultMinimumWithdrawalAmount  uint64 `json:"operatorFeeVaultMinimumWithdrawalAmount"`
	OperatorFeeVaultWithdrawalNetwork        uint64 `json:"operatorFeeVaultWithdrawalNetwork"`
	GovernanceTokenOwner                     string `json:"governanceTokenOwner"`
	Fork                                     uint64 `json:"fork"`
	EnableGovernance                         bool   `json:"enableGovernance"`
	FundDevAccounts                          bool   `json:"fundDevAccounts"`
	UseRevenueShare                          bool   `json:"useRevenueShare"`
	ChainFeesRecipient                       string `json:"chainFeesRecipient"`
	L1FeesDepositor                          string `json:"l1FeesDepositor"`
	UseCustomGasToken                        bool   `json:"useCustomGasToken"`
	UseInterop                               bool   `json:"useInterop"`
	GasPayingTokenName                       string `json:"gasPayingTokenName"`
	GasPayingTokenSymbol                     string `json:"gasPayingTokenSymbol"`
	NativeAssetLiquidityAmount               uint64 `json:"nativeAssetLiquidityAmount"`
	LiquidityControllerOwner                 string `json:"liquidityControllerOwner"`
	DevFeatureBitmap                         string `json:"devFeatureBitmap"` // bytes32 hex string (e.g. "0x000...000")
}

// RunL2GenesisScript runs the L2Genesis script to generate L2 genesis allocations.
// We create a thin wrapper script (L2GenesisDump.s.sol) that calls L2Genesis.run()
// then vm.dumpState() to export the EVM state as a JSON allocs file.
func (f *ForgeExecutor) RunL2GenesisScript(ctx context.Context, input *L2GenesisInput, outputPath string, logger *StepLogger) (*ForgeScriptResult, error) {
	startTime := time.Now()

	// Convert outputPath to absolute so vm.dumpState writes to the right place
	absOutputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve output path: %w", err)
	}

	// ---- Write the wrapper script ----
	// L2Genesis.run(Input) sets up predeploys but doesn't dump state.
	// vm.dumpState(path) is a Foundry cheatcode that writes the full EVM state to JSON.
	wrapperPath := filepath.Join(f.contractsDir, "scripts", "L2GenesisDump.s.sol")
	wrapperContent := `// SPDX-License-Identifier: MIT
pragma solidity 0.8.15;
import {L2Genesis} from "scripts/L2Genesis.s.sol";
contract L2GenesisDump is L2Genesis {
    function runWithDump(Input memory _input, string memory _outputPath) public {
        run(_input);
        vm.dumpState(_outputPath);
    }
}
`
	if err := os.WriteFile(wrapperPath, []byte(wrapperContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write L2GenesisDump wrapper: %w", err)
	}
	defer os.Remove(wrapperPath) // Clean up after forge runs

	// ---- Build the tuple literal for forge ----
	// forge script --sig parses each positional arg according to its type.
	// Tuples must be in human-readable format: (val1,val2,...), NOT ABI-encoded hex.
	const inputTupleSig = "(uint256,uint256,address,address,address,address,address,uint256,uint256,address,uint256,uint256,address,uint256,uint256,address,uint256,uint256,address,uint256,bool,bool,bool,address,address,bool,bool,string,string,uint256,address,bytes32)"

	devFeatureBitmap := input.DevFeatureBitmap
	if devFeatureBitmap == "" {
		devFeatureBitmap = "0x0000000000000000000000000000000000000000000000000000000000000000"
	}

	tupleLiteral := fmt.Sprintf(`(%d,%d,%s,%s,%s,%s,%s,%d,%d,%s,%d,%d,%s,%d,%d,%s,%d,%d,%s,%d,%t,%t,%t,%s,%s,%t,%t,"%s","%s",%d,%s,%s)`,
		input.L1ChainID,
		input.L2ChainID,
		input.L1CrossDomainMessengerProxy,
		input.L1StandardBridgeProxy,
		input.L1ERC721BridgeProxy,
		input.OpChainProxyAdminOwner,
		input.SequencerFeeVaultRecipient,
		input.SequencerFeeVaultMinimumWithdrawalAmount,
		input.SequencerFeeVaultWithdrawalNetwork,
		input.BaseFeeVaultRecipient,
		input.BaseFeeVaultMinimumWithdrawalAmount,
		input.BaseFeeVaultWithdrawalNetwork,
		input.L1FeeVaultRecipient,
		input.L1FeeVaultMinimumWithdrawalAmount,
		input.L1FeeVaultWithdrawalNetwork,
		input.OperatorFeeVaultRecipient,
		input.OperatorFeeVaultMinimumWithdrawalAmount,
		input.OperatorFeeVaultWithdrawalNetwork,
		input.GovernanceTokenOwner,
		input.Fork,
		input.EnableGovernance,
		input.FundDevAccounts,
		input.UseRevenueShare,
		input.ChainFeesRecipient,
		input.L1FeesDepositor,
		input.UseCustomGasToken,
		input.UseInterop,
		input.GasPayingTokenName,
		input.GasPayingTokenSymbol,
		input.NativeAssetLiquidityAmount,
		input.LiquidityControllerOwner,
		devFeatureBitmap,
	)

	// ---- Build forge command ----
	// No --broadcast, --rpc-url, --legacy, or --slow because this runs locally.
	// forge --sig expects one positional CLI arg per parameter:
	//   arg 1 = tuple literal in human-readable format
	//   arg 2 = output path string
	args := []string{
		"script",
		"scripts/L2GenesisDump.s.sol:L2GenesisDump",
		"--sig", "runWithDump(" + inputTupleSig + ",string)",
		tupleLiteral,
		absOutputPath,
		"-vvvv", // Verbose output for debugging
	}

	cmd := exec.CommandContext(ctx, "forge", args...)
	cmd.Dir = f.contractsDir

	// Log command being executed
	cmdStr := fmt.Sprintf("forge script L2GenesisDump --sig runWithDump(Input,string) -> %s", absOutputPath)
	logger.Printf("=== Executing: %s ===\n", cmdStr)
	logger.Printf("=== Working directory: %s ===\n", f.contractsDir)
	logger.Printf("=== Started at: %s ===\n\n", time.Now().Format(time.RFC3339))

	// Capture both stdout and stderr
	var outputBuffer bytes.Buffer
	multiWriter := io.MultiWriter(&outputBuffer, logger)
	cmd.Stdout = multiWriter
	cmd.Stderr = multiWriter

	// Run the command
	err = cmd.Run()
	duration := time.Since(startTime)

	result := &ForgeScriptResult{
		Success:  err == nil,
		Output:   outputBuffer.String(),
		Duration: duration,
	}

	logger.Printf("\n=== Completed in %.1fs (success=%v) ===\n", duration.Seconds(), result.Success)

	if err != nil {
		return result, fmt.Errorf("L2Genesis script failed: %w", err)
	}

	return result, nil
}

// findBroadcastDir finds the broadcast directory for a script in the default
// contracts-bedrock package.
func (f *ForgeExecutor) findBroadcastDir(scriptPath string) string {
	return f.findBroadcastDirIn(f.contractsDir, scriptPath)
}

// findBroadcastDirIn finds the broadcast directory for a script under an
// explicit forge project root (used by out-of-tree scripts such as the
// oprsk-contracts/ runSplit driver).
func (f *ForgeExecutor) findBroadcastDirIn(projectRoot, scriptPath string) string {
	scriptName := scriptPath
	if idx := strings.Index(scriptName, ":"); idx != -1 {
		scriptName = scriptName[:idx]
	}
	scriptName = filepath.Base(scriptName)
	return filepath.Join(projectRoot, "broadcast", scriptName, fmt.Sprintf("%d", f.cfg.L1ChainID))
}

// ParseBroadcastJSON parses the latest broadcast JSON to extract deployed addresses.
// sigName is the function name from the signature (e.g., "run", "runSplit") -
// Forge creates broadcast files named {sigName}-latest.json
func (f *ForgeExecutor) ParseBroadcastJSON(broadcastDir string, sigName string) (map[string]interface{}, error) {
	// Default to "run" if sigName is empty
	if sigName == "" {
		sigName = "run"
	}

	// Try {sigName}-latest.json first (e.g., runSplit-latest.json)
	broadcastPath := filepath.Join(broadcastDir, fmt.Sprintf("%s-latest.json", sigName))

	if _, err := os.Stat(broadcastPath); os.IsNotExist(err) {
		// Specific broadcast file doesn't exist, look for any *-latest.json file
		// This handles edge cases where the expected file name doesn't match
		matches, globErr := filepath.Glob(filepath.Join(broadcastDir, "*-latest.json"))
		if globErr != nil {
			return nil, fmt.Errorf("failed to glob broadcast files: %w", globErr)
		}

		if len(matches) == 0 {
			return nil, fmt.Errorf("no broadcast files found in %s (expected %s-latest.json)", broadcastDir, sigName)
		}

		// If multiple matches, use the most recently modified one
		var latestPath string
		var latestTime time.Time
		for _, match := range matches {
			info, statErr := os.Stat(match)
			if statErr != nil {
				continue
			}
			if latestPath == "" || info.ModTime().After(latestTime) {
				latestPath = match
				latestTime = info.ModTime()
			}
		}

		if latestPath == "" {
			return nil, fmt.Errorf("no readable broadcast files found in %s", broadcastDir)
		}

		broadcastPath = latestPath
	}

	data, err := os.ReadFile(broadcastPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read broadcast file %s: %w", broadcastPath, err)
	}

	var broadcast map[string]interface{}
	if err := json.Unmarshal(data, &broadcast); err != nil {
		return nil, fmt.Errorf("failed to parse broadcast JSON: %w", err)
	}

	return broadcast, nil
}

// ExtractContractAddress extracts a contract address from broadcast JSON by contract name
func ExtractContractAddress(broadcast map[string]interface{}, contractName string) (string, error) {
	transactions, ok := broadcast["transactions"].([]interface{})
	if !ok {
		return "", fmt.Errorf("no transactions found in broadcast")
	}

	for _, tx := range transactions {
		txMap, ok := tx.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this transaction deployed the contract we're looking for
		name, _ := txMap["contractName"].(string)
		if name == contractName {
			if addr, ok := txMap["contractAddress"].(string); ok {
				return addr, nil
			}
		}
	}

	return "", fmt.Errorf("contract %s not found in broadcast", contractName)
}

// ExtractAllContractAddresses extracts all deployed contract addresses from broadcast JSON
// This handles both direct CREATE/CREATE2 transactions and contracts deployed via OPCM internal calls
func ExtractAllContractAddresses(broadcast map[string]interface{}) (map[string]string, error) {
	addresses := make(map[string]string)

	transactions, ok := broadcast["transactions"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no transactions found in broadcast")
	}

	for _, tx := range transactions {
		txMap, ok := tx.(map[string]interface{})
		if !ok {
			continue
		}

		txType, _ := txMap["transactionType"].(string)

		// Handle direct CREATE/CREATE2 transactions
		if txType == "CREATE" || txType == "CREATE2" {
			name, _ := txMap["contractName"].(string)
			addr, _ := txMap["contractAddress"].(string)

			if name != "" && addr != "" {
				addresses[name] = addr
			}
		}

		// Handle OPCM-style deployments where contracts are in additionalContracts
		// The function name tells us what logical contract was deployed
		if txType == "CALL" {
			funcName, _ := txMap["function"].(string)
			additionalContracts, ok := txMap["additionalContracts"].([]interface{})
			if !ok || len(additionalContracts) == 0 {
				continue
			}

			// Map function names to logical contract names
			// OPCM deploys all proxies as "Proxy" contracts, so we need to infer the name from the function
			logicalName := mapOPCMFunctionToContractName(funcName)
			if logicalName == "" {
				// If we can't map the function, use the actual contract name from the first additional contract
				for _, ac := range additionalContracts {
					acMap, ok := ac.(map[string]interface{})
					if !ok {
						continue
					}
					name, _ := acMap["contractName"].(string)
					addr, _ := acMap["address"].(string)
					if name != "" && addr != "" {
						addresses[name] = addr
					}
				}
				continue
			}

			// Get the address from the first additional contract (the main deployed contract)
			for _, ac := range additionalContracts {
				acMap, ok := ac.(map[string]interface{})
				if !ok {
					continue
				}
				addr, _ := acMap["address"].(string)
				if addr != "" {
					addresses[logicalName] = addr
					break // Only take the first one for the logical name
				}
			}
		}
	}

	return addresses, nil
}

// mapOPCMFunctionToContractName maps OPCM deploy function names to logical contract names
func mapOPCMFunctionToContractName(funcName string) string {
	// Function signatures look like "deployOptimismPortal(((address,...),...))"
	// We extract the function name prefix to determine the contract type

	switch {
	case strings.HasPrefix(funcName, "deployAddressManager"):
		return "AddressManager"
	case strings.HasPrefix(funcName, "deployProxyAdmin"):
		return "ProxyAdmin"
	case strings.HasPrefix(funcName, "deployL1ERC721Bridge"):
		return "L1ERC721BridgeProxy"
	case strings.HasPrefix(funcName, "deployOptimismPortal"):
		return "OptimismPortalProxy"
	case strings.HasPrefix(funcName, "deployETHLockbox"):
		return "ETHLockboxProxy"
	case strings.HasPrefix(funcName, "deploySystemConfig"):
		return "SystemConfigProxy"
	case strings.HasPrefix(funcName, "deployOptimismMintableERC20Factory"):
		return "OptimismMintableERC20FactoryProxy"
	case strings.HasPrefix(funcName, "deployL1StandardBridge"):
		return "L1StandardBridgeProxy"
	case strings.HasPrefix(funcName, "deployL1CrossDomainMessenger"):
		return "L1CrossDomainMessengerProxy"
	case strings.HasPrefix(funcName, "deployDisputeGameFactory"):
		return "DisputeGameFactoryProxy"
	case strings.HasPrefix(funcName, "deployDelayedWETH"):
		return "DelayedWETHProxy"
	case strings.HasPrefix(funcName, "deployAnchorStateRegistry"):
		return "AnchorStateRegistryProxy"
	case strings.HasPrefix(funcName, "deployPermissionedDisputeGame"):
		return "PermissionedDisputeGame"
	case strings.HasPrefix(funcName, "deployFaultDisputeGame"):
		return "FaultDisputeGame"
	default:
		return ""
	}
}

// ExtractOPChainAddressesFromReturns extracts OP Chain addresses from the broadcast returns field
// The returns field contains a tuple of addresses in the order defined by DeployOPChain.Output struct
func ExtractOPChainAddressesFromReturns(broadcast map[string]interface{}) (map[string]string, error) {
	addresses := make(map[string]string)

	returns, ok := broadcast["returns"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no returns found in broadcast")
	}

	output, ok := returns["output_"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no output_ found in returns")
	}

	value, ok := output["value"].(string)
	if !ok {
		return nil, fmt.Errorf("no value found in output_")
	}

	// Parse the tuple of addresses: "(addr1, addr2, ...)"
	// Remove parentheses and split by comma
	value = strings.TrimPrefix(value, "(")
	value = strings.TrimSuffix(value, ")")
	parts := strings.Split(value, ", ")

	// The order matches DeployOPChain.Output struct:
	// 1. opChainProxyAdmin
	// 2. addressManager
	// 3. l1ERC721BridgeProxy
	// 4. systemConfigProxy
	// 5. optimismMintableERC20FactoryProxy
	// 6. l1StandardBridgeProxy
	// 7. l1CrossDomainMessengerProxy
	// 8. optimismPortalProxy
	// 9. ethLockboxProxy
	// 10. disputeGameFactoryProxy
	// 11. anchorStateRegistryProxy
	// 12. faultDisputeGame
	// 13. permissionedDisputeGame
	// 14. delayedWETHPermissionedGameProxy
	// 15. delayedWETHPermissionlessGameProxy

	fieldNames := []string{
		"ProxyAdmin",
		"AddressManager",
		"L1ERC721BridgeProxy",
		"SystemConfigProxy",
		"OptimismMintableERC20FactoryProxy",
		"L1StandardBridgeProxy",
		"L1CrossDomainMessengerProxy",
		"OptimismPortalProxy",
		"EthLockboxProxy",
		"DisputeGameFactoryProxy",
		"AnchorStateRegistryProxy",
		"FaultDisputeGame",
		"PermissionedDisputeGame",
		"DelayedWETHPermissionedGameProxy",
		"DelayedWETHPermissionlessGameProxy",
	}

	if len(parts) < len(fieldNames) {
		return nil, fmt.Errorf("expected %d addresses in returns, got %d", len(fieldNames), len(parts))
	}

	for i, name := range fieldNames {
		addr := strings.TrimSpace(parts[i])
		if addr != "" && addr != "0x0000000000000000000000000000000000000000" {
			addresses[name] = addr
		}
	}

	return addresses, nil
}

// ParseScriptOutput extracts contract addresses from forge script output
// This parses both the "== Return ==" section (for struct returns) and
// the "== Logs ==" section (for assertion log lines)
// Similar to how op-deployer extracts addresses from forge output
func ParseScriptOutput(output string) map[string]string {
	addresses := make(map[string]string)

	// Regex to match addresses (40 hex chars with 0x prefix)
	addrRegex := regexp.MustCompile(`0x[a-fA-F0-9]{40}`)

	// Parse "== Return ==" section for struct fields
	// Format: "fieldName: 0xABC123..."
	returnSectionRegex := regexp.MustCompile(`(?s)==\s*Return\s*==\s*\n(.*?)(?:\n\n|$)`)
	if matches := returnSectionRegex.FindStringSubmatch(output); len(matches) > 1 {
		returnSection := matches[1]

		// Match field: address patterns like "protocolVersionsImpl: 0x1f734B89..."
		fieldRegex := regexp.MustCompile(`(\w+):\s*(0x[a-fA-F0-9]{40})`)
		fieldMatches := fieldRegex.FindAllStringSubmatch(returnSection, -1)
		for _, match := range fieldMatches {
			if len(match) == 3 {
				fieldName := match[1]
				addr := match[2]
				addresses[fieldName] = addr
			}
		}
	}

	// Parse "== Logs ==" section for assertion lines
	// Format: "Running chain assertions on the X implementation at 0xABC123"
	// or: "Running chain assertions on the X at 0xABC123"
	logSectionRegex := regexp.MustCompile(`(?s)==\s*Logs\s*==\s*\n(.*?)(?:\n\n|$)`)
	if matches := logSectionRegex.FindStringSubmatch(output); len(matches) > 1 {
		logSection := matches[1]

		// Match assertion lines
		assertionRegex := regexp.MustCompile(`Running chain assertions on (?:the )?(\w+)(?: implementation)? at (0x[a-fA-F0-9]{40})`)
		assertionMatches := assertionRegex.FindAllStringSubmatch(logSection, -1)
		for _, match := range assertionMatches {
			if len(match) == 3 {
				contractName := match[1]
				addr := match[2]
				// Store with the contract name as key
				addresses[contractName] = addr
			}
		}
	}

	// Also look for labeled addresses in traces like:
	// "VM::label(SuperchainConfigImpl: [0xb08Cc720...], "SuperchainConfigImpl")"
	// These appear when forge labels addresses during script execution
	labelRegex := regexp.MustCompile(`VM::label\((\w+):\s*\[(0x[a-fA-F0-9]{40})\]`)
	labelMatches := labelRegex.FindAllStringSubmatch(output, -1)
	for _, match := range labelMatches {
		if len(match) == 3 {
			labelName := match[1]
			addr := match[2]
			// Only add if not already present (prefer Return/Logs sections)
			if _, exists := addresses[labelName]; !exists {
				addresses[labelName] = addr
			}
		}
	}

	// Look for struct Output patterns in return section
	// Format: "Output({ field1: 0x..., field2: 0x... })"
	outputStructRegex := regexp.MustCompile(`Output\(\{\s*([^}]+)\s*\}\)`)
	if matches := outputStructRegex.FindStringSubmatch(output); len(matches) > 1 {
		structContent := matches[1]
		// Parse comma-separated field: value pairs
		fieldRegex := regexp.MustCompile(`(\w+):\s*(0x[a-fA-F0-9]{40})`)
		fieldMatches := fieldRegex.FindAllStringSubmatch(structContent, -1)
		for _, match := range fieldMatches {
			if len(match) == 3 {
				fieldName := match[1]
				addr := match[2]
				// Only add if not already present
				if _, exists := addresses[fieldName]; !exists {
					addresses[fieldName] = addr
				}
			}
		}
	}

	// Silence unused variable warning for addrRegex
	_ = addrRegex

	return addresses
}

// DeploySuperchainInput holds configuration for superchain deployment
type DeploySuperchainInput struct {
	ProxyAdminOwner            string `json:"proxyAdminOwner"`
	ProtocolVersionsOwner      string `json:"protocolVersionsOwner"`
	Guardian                   string `json:"guardian"`
	Paused                     bool   `json:"paused"`
	RequiredProtocolVersion    string `json:"requiredProtocolVersion"`
	RecommendedProtocolVersion string `json:"recommendedProtocolVersion"`
}

// WriteInputJSON writes input configuration to a JSON file
func WriteInputJSON(path string, input interface{}) error {
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal input: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write input file: %w", err)
	}

	return nil
}

// DeployImplementationsInput holds configuration for implementations deployment
type DeployImplementationsInput struct {
	WithdrawalDelaySeconds          uint64 `json:"withdrawalDelaySeconds"`
	MinProposalSizeBytes            uint64 `json:"minProposalSizeBytes"`
	ChallengePeriodSeconds          uint64 `json:"challengePeriodSeconds"`
	ProofMaturityDelaySeconds       uint64 `json:"proofMaturityDelaySeconds"`
	DisputeGameFinalityDelaySeconds uint64 `json:"disputeGameFinalityDelaySeconds"`
	MipsVersion                     uint64 `json:"mipsVersion"`
	L1ContractsRelease              string `json:"l1ContractsRelease"`
	SuperchainConfigProxy           string `json:"superchainConfigProxy"`
	ProtocolVersionsProxy           string `json:"protocolVersionsProxy"`
	UseInterop                      bool   `json:"useInterop"`
	StandardVersionsToml            string `json:"standardVersionsToml,omitempty"`
}

// DeployOPChainInput holds configuration for OP chain deployment
type DeployOPChainInput struct {
	OpChainProxyAdminOwner  string `json:"opChainProxyAdminOwner"`
	SystemConfigOwner       string `json:"systemConfigOwner"`
	Batcher                 string `json:"batcher"`
	UnsafeBlockSigner       string `json:"unsafeBlockSigner"`
	Proposer                string `json:"proposer"`
	Challenger              string `json:"challenger"`
	BasefeeScalar           uint64 `json:"basefeeScalar"`
	BlobBasefeeScalar       uint64 `json:"blobBasefeeScalar"`
	L2ChainId               uint64 `json:"l2ChainId"`
	Opcm                    string `json:"opcm"`
	SaltMixer               string `json:"saltMixer"`
	GasLimit                uint64 `json:"gasLimit"`
	DisputeGameType         uint32 `json:"disputeGameType"`
	DisputeAbsolutePrestate string `json:"disputeAbsolutePrestate"`
	DisputeMaxGameDepth     uint64 `json:"disputeMaxGameDepth"`
	DisputeSplitDepth       uint64 `json:"disputeSplitDepth"`
	DisputeClockExtension   uint64 `json:"disputeClockExtension"`
	DisputeMaxClockDuration uint64 `json:"disputeMaxClockDuration"`
}
