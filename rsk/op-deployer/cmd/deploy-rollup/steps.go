package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Protocol versions - OPStackSupport from params.go
// Encoding: [8 bytes unused][8 bytes build][4 bytes major][4 bytes minor][4 bytes patch][4 bytes prerelease]
// For OPStackSupport: Major=9, Minor=0, Patch=0, PreRelease=0
const (
	OPStackSupport = "0x0000000000000000000000000000000000000009000000000000000000000000"

	// buildHashFileName is the name of the file storing the last successful build hash under contracts dir.
	buildHashFileName = ".deploy-rollup-build-hash"
)

// dirs excluded from content hash so build outputs and VCS do not affect the cache
var hashExcludeDirs = map[string]bool{
	".git":            true,
	"forge-artifacts": true,
	"cache":           true,
	"broadcast":       true,
	"out":             true,
	"artifacts":       true, // build-info output from foundry.toml build_info_path
}

// hashContractsDir returns a deterministic SHA256 hex hash of all relevant files under contractsDir.
// Excludes .git, forge-artifacts, cache, broadcast, out, and the build hash file itself.
func hashContractsDir(contractsDir string) (string, error) {
	var entries []string
	err := filepath.WalkDir(contractsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(contractsDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(path)
		if d.IsDir() {
			if hashExcludeDirs[base] || base == buildHashFileName {
				return filepath.SkipDir
			}
			return nil
		}
		if base == buildHashFileName {
			return nil
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	h := sha256.New()
	for _, rel := range entries {
		full := filepath.Join(contractsDir, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readStoredBuildHash reads the stored build hash from path. Returns empty string if file is missing or unreadable.
func readStoredBuildHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// writeStoredBuildHash writes the build hash to path (single line with newline).
func writeStoredBuildHash(path, hash string) error {
	return os.WriteFile(path, []byte(hash+"\n"), 0644)
}

// buildContracts runs the contract build step with the appropriate Foundry profile
func (r *Runner) buildContracts(ctx context.Context, logger *StepLogger) error {
	contractsDir := r.cfg.ContractsDir()

	// Build environment with optional FOUNDRY_PROFILE
	env := os.Environ()
	if r.cfg.FoundryProfile != "" {
		env = append(env, "FOUNDRY_PROFILE="+r.cfg.FoundryProfile)
		logger.Printf("Using Foundry profile: %s\n", r.cfg.FoundryProfile)
	}

	// Run: just clean
	logger.Printf("Running: just clean\n")
	cleanCmd := exec.CommandContext(ctx, "just", "clean")
	cleanCmd.Dir = contractsDir
	cleanCmd.Env = env
	cleanCmd.Stdout = logger
	cleanCmd.Stderr = logger
	if err := cleanCmd.Run(); err != nil {
		// Clean might fail if files don't exist, which is fine
		logger.Printf("Warning: just clean returned error (continuing): %v\n", err)
	}

	// Run: just forge-build (skips lint-fix which can be slow)
	logger.Printf("Running: just forge-build\n")
	buildCmd := exec.CommandContext(ctx, "just", "forge-build")
	buildCmd.Dir = contractsDir
	buildCmd.Env = env
	buildCmd.Stdout = logger
	buildCmd.Stderr = logger
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("just forge-build failed: %w", err)
	}

	hash, err := hashContractsDir(contractsDir)
	if err != nil {
		logger.Printf("Warning: could not compute contracts hash (continuing): %v\n", err)
	} else if err := writeStoredBuildHash(r.cfg.ContractsBuildHashPath(), hash); err != nil {
		logger.Printf("Warning: could not write build hash file (continuing): %v\n", err)
	}

	// Report contract sizes after a successful build
	artifactsDir := filepath.Join(contractsDir, "forge-artifacts")
	sizes := readContractSizes(artifactsDir, deployedContracts())
	printContractSizes(sizes, uint64(r.cfg.GasLimit))
	logContractSizes(logger, sizes, uint64(r.cfg.GasLimit))

	logger.Printf("Contract build completed successfully\n")
	return nil
}

// computeBatchInboxAddress computes the batch inbox address for a given L2 chain ID
// The batch inbox address is deterministically computed from the chain ID
// Format: 0x00 + keccak256("optimism.batch_inbox_address" + chainID)[12:]
// For simplicity, we use a deterministic format based on chain ID
func computeBatchInboxAddress(l2ChainID uint64) string {
	// Standard OP Stack batch inbox address computation
	// Uses format: 0xff69000000000000000000000000000000<chainID as hex>
	return fmt.Sprintf("0xff69000000000000000000000000000000%06x", l2ChainID)
}

// extractSuperchainAddresses parses the broadcast JSON and returns ProxyAdmin, Proxy addresses (in order), and implementation addresses
func extractSuperchainAddresses(broadcast map[string]interface{}) (proxyAdmin string, proxies []string, impls map[string]string) {
	impls = make(map[string]string)

	transactions, ok := broadcast["transactions"].([]interface{})
	if !ok {
		return
	}

	for _, tx := range transactions {
		txMap, ok := tx.(map[string]interface{})
		if !ok {
			continue
		}

		txType, _ := txMap["transactionType"].(string)
		name, _ := txMap["contractName"].(string)
		addr, _ := txMap["contractAddress"].(string)

		if txType != "CREATE" && txType != "CREATE2" {
			continue
		}

		if addr == "" {
			continue
		}

		switch name {
		case "ProxyAdmin":
			proxyAdmin = addr
		case "Proxy":
			// Proxies are deployed in order, so we track them sequentially
			proxies = append(proxies, addr)
		default:
			// Any other contract is likely an implementation
			if name != "" {
				impls[name] = addr
			}
		}
	}

	return
}

// deploySuperchain deploys the superchain contracts using DeploySuperchain.s.sol
func (r *Runner) deploySuperchain(ctx context.Context, logger *StepLogger) error {
	logger.Printf("Deploying Superchain contracts...\n")
	logger.Printf("Guardian: %s\n", r.cfg.Guardian)
	logger.Printf("ProtocolVersionsOwner: %s\n", r.cfg.ProtocolVersionsOwner)
	logger.Printf("SuperchainProxyAdminOwner: %s\n", r.cfg.SuperchainProxyAdminOwner)

	// Create input file for the script (in work dir to avoid polluting contracts dir hash)
	inputDir := filepath.Join(r.cfg.WorkDir, "deploy-inputs")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		return fmt.Errorf("failed to create input directory: %w", err)
	}

	inputFile := filepath.Join(inputDir, fmt.Sprintf("deploy-superchain-%d.json", r.cfg.L1ChainID))
	input := map[string]interface{}{
		"guardian":                   r.cfg.Guardian,
		"protocolVersionsOwner":      r.cfg.ProtocolVersionsOwner,
		"superchainProxyAdminOwner":  r.cfg.SuperchainProxyAdminOwner,
		"paused":                     false,
		"requiredProtocolVersion":    OPStackSupport,
		"recommendedProtocolVersion": OPStackSupport,
	}

	if err := WriteInputJSON(inputFile, input); err != nil {
		return fmt.Errorf("failed to write input file: %w", err)
	}
	logger.Printf("Input file: %s\n", inputFile)

	// Run the forge script
	result, err := r.forge.RunScript(ctx, ForgeScriptInput{
		Script: "scripts/deploy/DeploySuperchain.s.sol:DeploySuperchain",
		Sig:    "run((address,address,address,bool,bytes32,bytes32))",
		SigArgs: []string{
			fmt.Sprintf("(%s,%s,%s,%t,%s,%s)",
				r.cfg.Guardian,
				r.cfg.ProtocolVersionsOwner,
				r.cfg.SuperchainProxyAdminOwner,
				false, // paused
				OPStackSupport,
				OPStackSupport,
			),
		},
	}, logger)

	if err != nil {
		return err
	}

	// Parse broadcast to extract addresses
	broadcast, err := r.forge.ParseBroadcastJSON(result.BroadcastDir, result.SigName)
	if err != nil {
		logger.Printf("Warning: failed to parse broadcast: %v\n", err)
		logger.Printf("Will try to extract addresses from script output...\n")
	}

	// Extract addresses with order awareness (for Proxy contracts)
	proxyAdmin, proxies, impls := extractSuperchainAddresses(broadcast)

	// Parse script output for implementation addresses (more reliable than broadcast)
	// The == Return == section contains the DeploySuperchainOutput struct with all addresses
	outputAddrs := ParseScriptOutput(result.Output)
	logger.Printf("\nAddresses from script output:\n")
	for name, addr := range outputAddrs {
		logger.Printf("  %s: %s\n", name, addr)
	}

	logger.Printf("\nDeployed contracts from broadcast:\n")
	logger.Printf("  ProxyAdmin: %s\n", proxyAdmin)
	for i, proxy := range proxies {
		logger.Printf("  Proxy[%d]: %s\n", i, proxy)
	}
	for name, addr := range impls {
		logger.Printf("  %s: %s\n", name, addr)
	}

	// Superchain deployment order:
	// 1. ProxyAdmin
	// 2. SuperchainConfig Proxy
	// 3. SuperchainConfig Impl (via upgradeAndCall)
	// 4. ProtocolVersions Proxy
	// 5. ProtocolVersions Impl (via upgradeAndCall)
	var superchainConfigProxy, protocolVersionsProxy string
	if len(proxies) >= 1 {
		superchainConfigProxy = proxies[0]
	}
	if len(proxies) >= 2 {
		protocolVersionsProxy = proxies[1]
	}

	// Use script output for implementation addresses if available
	// Fall back to broadcast-extracted addresses
	superchainConfigImpl := impls["SuperchainConfig"]
	if addr, ok := outputAddrs["superchainConfigImpl"]; ok && addr != "" {
		superchainConfigImpl = addr
	}

	protocolVersionsImpl := impls["ProtocolVersions"]
	if addr, ok := outputAddrs["protocolVersionsImpl"]; ok && addr != "" {
		protocolVersionsImpl = addr
	}

	// Also use script output for proxies if available
	if addr, ok := outputAddrs["superchainProxyAdmin"]; ok && addr != "" && proxyAdmin == "" {
		proxyAdmin = addr
	}
	if addr, ok := outputAddrs["superchainConfigProxy"]; ok && addr != "" && superchainConfigProxy == "" {
		superchainConfigProxy = addr
	}
	if addr, ok := outputAddrs["protocolVersionsProxy"]; ok && addr != "" && protocolVersionsProxy == "" {
		protocolVersionsProxy = addr
	}

	// Update state with deployed addresses
	r.state.SetSuperchainAddresses(&SuperchainAddresses{
		SuperchainProxyAdmin:  proxyAdmin,
		SuperchainConfigImpl:  superchainConfigImpl,
		SuperchainConfigProxy: superchainConfigProxy,
		ProtocolVersionsImpl:  protocolVersionsImpl,
		ProtocolVersionsProxy: protocolVersionsProxy,
	})

	logger.Printf("\nSuperchain deployment complete!\n")
	logger.Printf("  SuperchainProxyAdmin: %s\n", r.state.Superchain.SuperchainProxyAdmin)
	logger.Printf("  SuperchainConfigProxy: %s\n", r.state.Superchain.SuperchainConfigProxy)
	logger.Printf("  ProtocolVersionsProxy: %s\n", r.state.Superchain.ProtocolVersionsProxy)

	return nil
}

// deployImplementations deploys the implementation contracts using DeployImplementations.s.sol
func (r *Runner) deployImplementations(ctx context.Context, logger *StepLogger) error {
	if r.state.Superchain == nil {
		return fmt.Errorf("superchain not deployed yet")
	}

	logger.Printf("Deploying Implementation contracts...\n")
	logger.Printf("SuperchainConfigProxy: %s\n", r.state.Superchain.SuperchainConfigProxy)
	logger.Printf("ProtocolVersionsProxy: %s\n", r.state.Superchain.ProtocolVersionsProxy)
	logger.Printf("SuperchainProxyAdmin: %s\n", r.state.Superchain.SuperchainProxyAdmin)

	// Build the ABI-encoded input struct for DeployImplementations.Input
	// Struct fields:
	// - withdrawalDelaySeconds: uint256
	// - minProposalSizeBytes: uint256
	// - challengePeriodSeconds: uint256
	// - proofMaturityDelaySeconds: uint256
	// - disputeGameFinalityDelaySeconds: uint256
	// - mipsVersion: uint256
	// - devFeatureBitmap: bytes32
	// - faultGameV2MaxGameDepth: uint256
	// - faultGameV2SplitDepth: uint256
	// - faultGameV2ClockExtension: uint256
	// - faultGameV2MaxClockDuration: uint256
	// - superchainConfigProxy: address
	// - protocolVersionsProxy: address
	// - superchainProxyAdmin: address
	// - l1ProxyAdminOwner: address
	// - challenger: address
	// - skipFaultProofs: bool (RSK compatibility - skip large contracts)

	// Log if skipFaultProofs is enabled
	if r.cfg.SkipFaultProofs {
		logger.Printf("SkipFaultProofs enabled: will skip deploying OptimismPortalInterop, FaultDisputeGame, PermissionedDisputeGame, OPCMStandardValidator\n")
	}

	// Use cast abi-encode to encode the struct
	abiEncodeCmd := exec.CommandContext(ctx, "cast", "abi-encode",
		"f((uint256,uint256,uint256,uint256,uint256,uint256,bytes32,uint256,uint256,uint256,uint256,address,address,address,address,address,bool))",
		fmt.Sprintf("(%d,%d,%d,%d,%d,%d,%s,%d,%d,%d,%d,%s,%s,%s,%s,%s,%t)",
			r.cfg.WithdrawalDelaySeconds,          // withdrawalDelaySeconds
			126000,                            // minProposalSizeBytes
			r.cfg.ChallengePeriodSeconds,      // challengePeriodSeconds
			r.cfg.ProofMaturityDelaySeconds,       // proofMaturityDelaySeconds
			r.cfg.DisputeGameFinalityDelaySeconds, // disputeGameFinalityDelaySeconds
			8,                                     // mipsVersion (MIPS64)
			"0x0000000000000000000000000000000000000000000000000000000000000000", // devFeatureBitmap
			73,                    // faultGameV2MaxGameDepth
			30,                    // faultGameV2SplitDepth
			r.cfg.ClockExtension,  // faultGameV2ClockExtension
			r.cfg.MaxClockDuration, // faultGameV2MaxClockDuration
			r.state.Superchain.SuperchainConfigProxy,
			r.state.Superchain.ProtocolVersionsProxy,
			r.state.Superchain.SuperchainProxyAdmin,
			r.cfg.EOAAddress,      // l1ProxyAdminOwner
			r.cfg.Challenger,      // challenger
			r.cfg.SkipFaultProofs, // skipFaultProofs (RSK compatibility)
		),
	)
	encodedInputBytes, err := abiEncodeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to ABI-encode input: %w", err)
	}
	encodedInput := strings.TrimSpace(string(encodedInputBytes))
	logger.Printf("Encoded input: %s\n", encodedInput[:66]+"...")

	// Run the forge script with runWithBytes
	result, err := r.forge.RunScript(ctx, ForgeScriptInput{
		Script:  "scripts/deploy/DeployImplementations.s.sol:DeployImplementations",
		Sig:     "runWithBytes(bytes)",
		SigArgs: []string{encodedInput},
	}, logger)

	if err != nil {
		return err
	}

	// Parse broadcast to extract addresses
	broadcast, err := r.forge.ParseBroadcastJSON(result.BroadcastDir, result.SigName)
	if err != nil {
		logger.Printf("Warning: failed to parse broadcast: %v\n", err)
	}

	addresses, err := ExtractAllContractAddresses(broadcast)
	if err != nil {
		return fmt.Errorf("failed to extract addresses: %w", err)
	}

	// Parse script output for implementation addresses (more reliable than broadcast)
	// The == Logs == section contains "Running chain assertions on the X implementation at 0x..."
	// The == Return == section contains the DeployImplementationsOutput struct
	outputAddrs := ParseScriptOutput(result.Output)
	logger.Printf("\nAddresses from script output:\n")
	for name, addr := range outputAddrs {
		logger.Printf("  %s: %s\n", name, addr)
	}

	logger.Printf("\nDeployed implementation contracts from broadcast:\n")
	for name, addr := range addresses {
		logger.Printf("  %s: %s\n", name, addr)
	}

	// Helper to get address preferring script output over broadcast
	// Try multiple possible key names for each address
	getAddr := func(keys ...string) string {
		for _, key := range keys {
			if addr, ok := outputAddrs[key]; ok && addr != "" {
				return addr
			}
			if addr, ok := addresses[key]; ok && addr != "" {
				return addr
			}
		}
		return ""
	}

	// Update state with all implementation addresses
	// Map script output field names to our state struct
	// Script output uses various naming conventions
	r.state.SetImplementationAddresses(&ImplementationAddresses{
		// In op-batcher/v1.16.7+ the OPCM v1 fields are deprecated (always zero) and
		// the new manager is OPCM v2. DeployOPChain.s.sol casts _input.opcm directly
		// to IOPContractsManagerV2, so the v2 address goes into the OPCM slot.
		OPCM:                          getAddr("OPContractsManagerV2", "opcmV2", "OPContractsManager", "opcmAddress", "OPCM"),
		OpcmContractsContainerImpl:    getAddr("OPContractsManagerBPImplsContainerImpl", "OpcmContractsContainerImpl"),
		OpcmGameTypeAdderImpl:         getAddr("OPContractsManagerGameTypeAdderImpl", "OpcmGameTypeAdderImpl"),
		OpcmDeployerImpl:              getAddr("OPContractsManagerDeployerImpl", "OpcmDeployerImpl"),
		OpcmUpgraderImpl:              getAddr("OPContractsManagerUpgraderImpl", "OpcmUpgraderImpl"),
		OpcmInteropMigratorImpl:       getAddr("OPContractsManagerInteropMigratorImpl", "OpcmInteropMigratorImpl"),
		OpcmStandardValidatorImpl:     getAddr("OPContractsManagerStandardValidatorStubImpl", "OPContractsManagerStandardValidatorImpl", "OpcmStandardValidatorImpl"),
		DelayedWETHImpl:               getAddr("DelayedWETHImpl", "DelayedWETH"),
		OptimismPortalImpl:            getAddr("OptimismPortalImpl", "OptimismPortal2", "OptimismPortal"),
		OptimismPortalInteropImpl:     getAddr("OptimismPortalInteropImpl"),
		EthLockboxImpl:                getAddr("ETHLockboxImpl", "ETHLockbox", "EthLockboxImpl"),
		PreimageOracleSingleton:       getAddr("PreimageOracleSingleton", "PreimageOracle"),
		MipsSingleton:                 getAddr("MIPSSingleton", "MIPS", "Mips"),
		SystemConfigImpl:              getAddr("SystemConfigImpl", "SystemConfig"),
		L1CrossDomainMessengerImpl:    getAddr("L1CrossDomainMessengerImpl", "L1CrossDomainMessenger"),
		L1ERC721BridgeImpl:            getAddr("L1ERC721BridgeImpl", "L1ERC721Bridge"),
		L1StandardBridgeImpl:          getAddr("L1StandardBridgeImpl", "L1StandardBridge"),
		OptimismMintableERC20Factory:  getAddr("OptimismMintableERC20FactoryImpl", "OptimismMintableERC20Factory"),
		DisputeGameFactoryImpl:        getAddr("DisputeGameFactoryImpl", "DisputeGameFactory"),
		AnchorStateRegistryImpl:       getAddr("AnchorStateRegistryImpl", "AnchorStateRegistry"),
		FaultDisputeGameV2Impl:        getAddr("FaultDisputeGameV2Impl", "FaultDisputeGameV2"),
		PermissionedDisputeGameV2Impl: getAddr("PermissionedDisputeGameV2Impl", "PermissionedDisputeGameV2"),
	})

	logger.Printf("\nImplementations deployment complete!\n")
	logger.Printf("  OPCM: %s\n", r.state.Implementations.OPCM)

	return nil
}

// deployOPChain deploys the OP chain contracts using DeployOPChain.s.sol
func (r *Runner) deployOPChain(ctx context.Context, logger *StepLogger) error {
	if r.state.Implementations == nil || r.state.Implementations.OPCM == "" {
		return fmt.Errorf("implementations not deployed yet")
	}

	logger.Printf("Deploying OP Chain contracts...\n")
	logger.Printf("OPCM: %s\n", r.state.Implementations.OPCM)
	logger.Printf("L2 Chain ID: %d\n", r.cfg.L2ChainID)

	// Salt mixer is L2 chain ID as string
	saltMixer := fmt.Sprintf("%d", r.cfg.L2ChainID)

	// Run the forge script
	// The new struct includes superchainConfig and useCustomGasToken fields
	result, err := r.forge.RunScript(ctx, ForgeScriptInput{
		Script: "scripts/deploy/DeployOPChain.s.sol:DeployOPChain",
		Sig:    "run((address,address,address,address,address,address,uint32,uint32,uint256,address,string,uint64,uint32,bytes32,uint256,uint256,uint64,uint64,bool,uint32,uint64,address,bool))",
		SigArgs: []string{
			fmt.Sprintf("(%s,%s,%s,%s,%s,%s,%d,%d,%d,%s,%s,%d,%d,%s,%d,%d,%d,%d,%t,%d,%d,%s,%t)",
				r.cfg.EOAAddress, // opChainProxyAdminOwner
				r.cfg.EOAAddress, // systemConfigOwner
				r.cfg.Batcher,    // batcher
				r.cfg.EOAAddress, // unsafeBlockSigner
				r.cfg.Proposer,   // proposer
				r.cfg.Challenger, // challenger
				1000000,          // basefeeScalar
				1000000,          // blobBaseFeeScalar
				r.cfg.L2ChainID,  // l2ChainId
				r.state.Implementations.OPCM,
				saltMixer,
				30000000, // gasLimit
				1,        // disputeGameType (CANNON)
				"0x0000000000000000000000000000000000000000000000000000000000000001", // disputeAbsolutePrestate (test value)
				73,                                       // disputeMaxGameDepth
				30,                                       // disputeSplitDepth
				r.cfg.ClockExtension,                     // disputeClockExtension
				r.cfg.MaxClockDuration,                   // disputeMaxClockDuration
				false,                                    // allowCustomDisputeParameters
				0,                                        // operatorFeeScalar
				0,                                        // operatorFeeConstant
				r.state.Superchain.SuperchainConfigProxy, // superchainConfig
				false,                                    // useCustomGasToken
			),
		},
	}, logger)

	if err != nil {
		return err
	}

	// Parse broadcast to extract addresses from the returns field
	broadcast, err := r.forge.ParseBroadcastJSON(result.BroadcastDir, result.SigName)
	if err != nil {
		logger.Printf("Warning: failed to parse broadcast: %v\n", err)
		return fmt.Errorf("failed to parse broadcast: %w", err)
	}

	// Use the new extraction function that parses the returns tuple
	addresses, err := ExtractOPChainAddressesFromReturns(broadcast)
	if err != nil {
		return fmt.Errorf("failed to extract addresses from returns: %w", err)
	}

	logger.Printf("\nDeployed OP Chain contracts:\n")
	for name, addr := range addresses {
		logger.Printf("  %s: %s\n", name, addr)
	}

	// Update state with OP chain addresses
	r.state.SetOpChainAddresses(&OpChainAddresses{
		ProxyAdmin:                   addresses["ProxyAdmin"],
		AddressManager:               addresses["AddressManager"],
		L1ERC721BridgeProxy:          addresses["L1ERC721BridgeProxy"],
		SystemConfigProxy:            addresses["SystemConfigProxy"],
		OptimismMintableERC20Factory: addresses["OptimismMintableERC20FactoryProxy"],
		L1StandardBridgeProxy:        addresses["L1StandardBridgeProxy"],
		L1CrossDomainMessengerProxy:  addresses["L1CrossDomainMessengerProxy"],
		OptimismPortalProxy:          addresses["OptimismPortalProxy"],
		EthLockboxProxy:              addresses["EthLockboxProxy"],
		DisputeGameFactoryProxy:      addresses["DisputeGameFactoryProxy"],
		AnchorStateRegistryProxy:     addresses["AnchorStateRegistryProxy"],
		FaultDisputeGame:             addresses["FaultDisputeGame"],
		PermissionedDisputeGame:      addresses["PermissionedDisputeGame"],
		DelayedWETHPermissionedProxy: addresses["DelayedWETHPermissionedGameProxy"],
		DelayedWETHProxy:             addresses["DelayedWETHPermissionlessGameProxy"],
	})

	// The OPCM v1 deploy path registers PermissionedDisputeGame in the factory
	// but does not populate the output struct field. Read it back from the factory.
	if err := r.resolveDisputeGamesFromFactory(ctx, addresses, logger); err != nil {
		logger.Printf("Warning: failed to resolve dispute games from factory: %v\n", err)
	}

	logger.Printf("\nOP Chain deployment complete!\n")
	logger.Printf("  SystemConfigProxy: %s\n", r.state.OpChain.SystemConfigProxy)
	logger.Printf("  OptimismPortalProxy: %s\n", r.state.OpChain.OptimismPortalProxy)

	return nil
}

// deployOPChainSplit deploys the OP chain contracts using split deployment for
// RSK gas limit compatibility.
//
// The script lives out-of-tree at oprsk-contracts/script/RSKDeployOPChain.s.sol
// and decomposes OPContractsManagerV2.deploy(config) into a splitter contract
// deploy + 5 stage transactions, each fitting under RSKj's RSKIP144 6.8M
// per-tx sublist gas cap. See oprsk-contracts/PLAN.md for the design and
// RSK_COMPATIBILITY_BLOCKERS.md (F1) for the upstream blocker this resolves.
func (r *Runner) deployOPChainSplit(ctx context.Context, logger *StepLogger) error {
	if r.state.Implementations == nil || r.state.Implementations.OPCM == "" {
		return fmt.Errorf("implementations not deployed yet")
	}

	logger.Printf("Deploying OP Chain contracts (split mode for RSK compatibility)...\n")
	logger.Printf("OPCM: %s\n", r.state.Implementations.OPCM)
	logger.Printf("L2 Chain ID: %d\n", r.cfg.L2ChainID)
	logger.Printf("This will create 8 separate transactions (1 splitter deploy + 7 stages)\n")
	logger.Printf("each fitting under RSKj's RSKIP144 6.8M per-tx sublist gas cap.\n")

	// Salt mixer is L2 chain ID as string
	saltMixer := fmt.Sprintf("%d", r.cfg.L2ChainID)

	// Run the out-of-tree RSK driver script. The input ABI matches upstream
	// `Types.DeployOPChainInput` byte-for-byte so the same SigArgs encoder
	// works unchanged.
	result, err := r.forge.RunScript(ctx, ForgeScriptInput{
		WorkingDir: r.cfg.OPRSKContractsDir(),
		Script:     "script/RSKDeployOPChain.s.sol:RSKDeployOPChain",
		Sig:        "runSplit((address,address,address,address,address,address,uint32,uint32,uint256,address,string,uint64,uint32,bytes32,uint256,uint256,uint64,uint64,bool,uint32,uint64,address,bool))",
		SigArgs: []string{
			fmt.Sprintf("(%s,%s,%s,%s,%s,%s,%d,%d,%d,%s,%s,%d,%d,%s,%d,%d,%d,%d,%t,%d,%d,%s,%t)",
				r.cfg.EOAAddress, // opChainProxyAdminOwner
				r.cfg.EOAAddress, // systemConfigOwner
				r.cfg.Batcher,    // batcher
				r.cfg.EOAAddress, // unsafeBlockSigner
				r.cfg.Proposer,   // proposer
				r.cfg.Challenger, // challenger
				1000000,          // basefeeScalar
				1000000,          // blobBaseFeeScalar
				r.cfg.L2ChainID,  // l2ChainId
				r.state.Implementations.OPCM,
				saltMixer,
				30000000, // gasLimit
				1,        // disputeGameType (CANNON)
				"0x0000000000000000000000000000000000000000000000000000000000000001", // disputeAbsolutePrestate (test value)
				73,                                       // disputeMaxGameDepth
				30,                                       // disputeSplitDepth
				r.cfg.ClockExtension,                     // disputeClockExtension
				r.cfg.MaxClockDuration,                   // disputeMaxClockDuration
				false,                                    // allowCustomDisputeParameters
				0,                                        // operatorFeeScalar
				0,                                        // operatorFeeConstant
				r.state.Superchain.SuperchainConfigProxy, // superchainConfig
				false,                                    // useCustomGasToken
			),
		},
	}, logger)

	if err != nil {
		return err
	}

	// Parse broadcast to extract addresses from the returns field
	broadcast, err := r.forge.ParseBroadcastJSON(result.BroadcastDir, result.SigName)
	if err != nil {
		logger.Printf("Warning: failed to parse broadcast: %v\n", err)
		return fmt.Errorf("failed to parse broadcast: %w", err)
	}

	// Use the new extraction function that parses the returns tuple
	addresses, err := ExtractOPChainAddressesFromReturns(broadcast)
	if err != nil {
		return fmt.Errorf("failed to extract addresses from returns: %w", err)
	}

	logger.Printf("\nDeployed OP Chain contracts (split mode):\n")
	for name, addr := range addresses {
		logger.Printf("  %s: %s\n", name, addr)
	}

	// Update state with OP chain addresses
	r.state.SetOpChainAddresses(&OpChainAddresses{
		ProxyAdmin:                   addresses["ProxyAdmin"],
		AddressManager:               addresses["AddressManager"],
		L1ERC721BridgeProxy:          addresses["L1ERC721BridgeProxy"],
		SystemConfigProxy:            addresses["SystemConfigProxy"],
		OptimismMintableERC20Factory: addresses["OptimismMintableERC20FactoryProxy"],
		L1StandardBridgeProxy:        addresses["L1StandardBridgeProxy"],
		L1CrossDomainMessengerProxy:  addresses["L1CrossDomainMessengerProxy"],
		OptimismPortalProxy:          addresses["OptimismPortalProxy"],
		EthLockboxProxy:              addresses["EthLockboxProxy"],
		DisputeGameFactoryProxy:      addresses["DisputeGameFactoryProxy"],
		AnchorStateRegistryProxy:     addresses["AnchorStateRegistryProxy"],
		FaultDisputeGame:             addresses["FaultDisputeGame"],
		PermissionedDisputeGame:      addresses["PermissionedDisputeGame"],
		DelayedWETHPermissionedProxy: addresses["DelayedWETHPermissionedGameProxy"],
		DelayedWETHProxy:             addresses["DelayedWETHPermissionlessGameProxy"],
	})

	// The OPCM v1 deploy path registers PermissionedDisputeGame in the factory
	// but does not populate the output struct field. Read it back from the factory.
	if err := r.resolveDisputeGamesFromFactory(ctx, addresses, logger); err != nil {
		logger.Printf("Warning: failed to resolve dispute games from factory: %v\n", err)
	}

	logger.Printf("\nOP Chain deployment (split mode) complete!\n")
	logger.Printf("  SystemConfigProxy: %s\n", r.state.OpChain.SystemConfigProxy)
	logger.Printf("  OptimismPortalProxy: %s\n", r.state.OpChain.OptimismPortalProxy)

	return nil
}

// generateL2Allocs generates the L2 genesis allocations using L2Genesis.s.sol
func (r *Runner) generateL2Allocs(ctx context.Context, logger *StepLogger) error {
	logger.Printf("Generating L2 genesis allocations...\n")

	allocsPath := filepath.Join(r.cfg.WorkDir, "l2-allocs.json")

	// Run the L2Genesis script — this MUST succeed for a functional rollup.
	// The script sets up L2 predeploys with the correct L1 contract addresses
	// (CrossDomainMessenger, StandardBridge, etc.) baked into the genesis state.
	err := r.runL2GenesisScript(ctx, allocsPath, logger)
	if err != nil {
		return fmt.Errorf("L2 genesis allocs generation failed (deployment cannot continue): %w", err)
	}

	r.state.L2AllocsGenerated = true
	logger.Printf("L2 allocs written to: %s\n", allocsPath)

	return nil
}

// runL2GenesisScript runs the L2Genesis.s.sol script to generate L2 genesis allocations
func (r *Runner) runL2GenesisScript(ctx context.Context, outputPath string, logger *StepLogger) error {
	// Build input struct for L2Genesis script
	input := &L2GenesisInput{
		L1ChainID:                                uint64(r.cfg.L1ChainID),
		L2ChainID:                                uint64(r.cfg.L2ChainID),
		L1CrossDomainMessengerProxy:              r.state.OpChain.L1CrossDomainMessengerProxy,
		L1StandardBridgeProxy:                    r.state.OpChain.L1StandardBridgeProxy,
		L1ERC721BridgeProxy:                      r.state.OpChain.L1ERC721BridgeProxy,
		OpChainProxyAdminOwner:                   r.cfg.EOAAddress,
		SequencerFeeVaultRecipient:               r.cfg.EOAAddress,
		SequencerFeeVaultMinimumWithdrawalAmount: 10000000000000000000, // 10 ether
		SequencerFeeVaultWithdrawalNetwork:       0,
		BaseFeeVaultRecipient:                    r.cfg.EOAAddress,
		BaseFeeVaultMinimumWithdrawalAmount:      10000000000000000000, // 10 ether
		BaseFeeVaultWithdrawalNetwork:            0,
		L1FeeVaultRecipient:                      r.cfg.EOAAddress,
		L1FeeVaultMinimumWithdrawalAmount:        10000000000000000000, // 10 ether
		L1FeeVaultWithdrawalNetwork:              0,
		OperatorFeeVaultRecipient:                r.cfg.EOAAddress,
		OperatorFeeVaultMinimumWithdrawalAmount:  10000000000000000000, // 10 ether
		OperatorFeeVaultWithdrawalNetwork:        0,
		GovernanceTokenOwner:                     r.cfg.EOAAddress,
		Fork:                                     8, // Holocene
		EnableGovernance:                         false,
		FundDevAccounts:                          true,
		UseRevenueShare:                          false,
		ChainFeesRecipient:                       r.cfg.EOAAddress,
		L1FeesDepositor:                          r.cfg.EOAAddress,
		UseCustomGasToken:                        false,
		UseInterop:                               false,
		GasPayingTokenName:                       "",
		GasPayingTokenSymbol:                     "",
		NativeAssetLiquidityAmount:               0,
		LiquidityControllerOwner:                 r.cfg.EOAAddress,
		DevFeatureBitmap:                         "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	logger.Printf("Running L2Genesis script...\n")
	logger.Printf("  L1 Chain ID: %d\n", r.cfg.L1ChainID)
	logger.Printf("  L2 Chain ID: %d\n", r.cfg.L2ChainID)
	logger.Printf("  L1CrossDomainMessengerProxy: %s\n", r.state.OpChain.L1CrossDomainMessengerProxy)
	logger.Printf("  L1StandardBridgeProxy: %s\n", r.state.OpChain.L1StandardBridgeProxy)
	logger.Printf("  L1ERC721BridgeProxy: %s\n", r.state.OpChain.L1ERC721BridgeProxy)
	logger.Printf("  Output path: %s\n", outputPath)

	// Run the forge script
	result, err := r.forge.RunL2GenesisScript(ctx, input, outputPath, logger)
	if err != nil {
		return fmt.Errorf("L2Genesis script failed: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("L2Genesis script failed")
	}

	// Verify the output file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return fmt.Errorf("state dump file was not created at %s", outputPath)
	}

	return nil
}

// generateGenesis generates the genesis.json and rollup.json files
func (r *Runner) generateGenesis(ctx context.Context, logger *StepLogger) error {
	logger.Printf("Generating genesis and rollup configuration...\n")

	// Generate op-deployer compatible state files (intent.toml, state.json) as
	// useful deployment artifacts, but don't fail if this doesn't work.
	logger.Printf("Generating op-deployer state files...\n")
	if err := WriteOpDeployerFiles(ctx, r.cfg, r.state); err != nil {
		logger.Printf("Warning: Failed to generate op-deployer state files: %v\n", err)
	} else {
		logger.Printf("intent.toml and state.json written to: %s\n", r.cfg.WorkDir)
	}

	// Build genesis.json and rollup.json from the L2 allocs + deployment state
	return r.generateMinimalGenesis(ctx, logger)
}

// generateMinimalGenesis creates minimal genesis.json and rollup.json as fallback
func (r *Runner) generateMinimalGenesis(ctx context.Context, logger *StepLogger) error {
	logger.Printf("Generating genesis and rollup configuration...\n")

	// Try to load L2 allocs from the generated file
	allocsPath := filepath.Join(r.cfg.WorkDir, "l2-allocs.json")
	var allocs map[string]interface{}
	if allocsData, err := os.ReadFile(allocsPath); err == nil {
		if err := json.Unmarshal(allocsData, &allocs); err != nil {
			logger.Printf("Warning: Failed to parse l2-allocs.json: %v\n", err)
			allocs = map[string]interface{}{}
		} else {
			logger.Printf("Loaded %d accounts from l2-allocs.json\n", len(allocs))
		}
	} else {
		logger.Printf("Warning: Could not read l2-allocs.json: %v\n", err)
		allocs = map[string]interface{}{}
	}

	// Get L1 block info for genesis reference
	l1Block, err := getLatestL1Block(ctx, r.cfg.RPCURL)
	if err != nil {
		logger.Printf("Warning: Failed to get L1 block: %v\n", err)
		l1Block = &StartBlock{
			Hash:      "0x0000000000000000000000000000000000000000000000000000000000000000",
			Number:    "0x0",
			Timestamp: "0x0",
		}
	}

	// Create genesis.json with proper L2 allocs
	genesisPath := filepath.Join(r.cfg.WorkDir, "genesis.json")
	genesis := map[string]interface{}{
		"config": map[string]interface{}{
			"chainId":                 r.cfg.L2ChainID,
			"homesteadBlock":          0,
			"eip150Block":             0,
			"eip155Block":             0,
			"eip158Block":             0,
			"byzantiumBlock":          0,
			"constantinopleBlock":     0,
			"petersburgBlock":         0,
			"istanbulBlock":           0,
			"muirGlacierBlock":        0,
			"berlinBlock":             0,
			"londonBlock":             0,
			"arrowGlacierBlock":       0,
			"grayGlacierBlock":        0,
			"mergeNetsplitBlock":      0,
			"shanghaiTime":            uint64(18446744073709551615),
			"cancunTime":              uint64(18446744073709551615),
			"pragueTime":              uint64(18446744073709551615),
			"bedrockBlock":            0,
			"regolithTime":            uint64(18446744073709551615),
			"canyonTime":              uint64(18446744073709551615),
			"ecotoneTime":             uint64(18446744073709551615),
			"fjordTime":               uint64(18446744073709551615),
			"graniteTime":             uint64(18446744073709551615),
			"holoceneTime":            uint64(18446744073709551615),
			"isthmusTime":             uint64(18446744073709551615),
			"terminalTotalDifficulty": 0,
			"depositContractAddress":  "0x0000000000000000000000000000000000000000",
			"optimism": map[string]interface{}{
				"eip1559Elasticity":        6,
				"eip1559Denominator":       50,
				"eip1559DenominatorCanyon": 250,
			},
		},
		"nonce":      "0x0",
		"timestamp":  l1Block.Timestamp,
		"extraData":  "0x00000000fa00000006", // Standard OP Stack extraData
		"gasLimit":   "0x3938700",            // 60000000 (same as reference)
		"difficulty": "0x0",
		"mixHash":    "0x0000000000000000000000000000000000000000000000000000000000000000",
		"coinbase":   "0x4200000000000000000000000000000000000011",
		"alloc":      allocs,
	}

	genesisData, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal genesis: %w", err)
	}

	if err := os.WriteFile(genesisPath, genesisData, 0644); err != nil {
		return fmt.Errorf("failed to write genesis file: %w", err)
	}

	logger.Printf("Genesis written to: %s\n", genesisPath)

	// Compute L2 genesis block hash from the genesis.json we just wrote
	l2GenesisHash := "0x0000000000000000000000000000000000000000000000000000000000000000"
	computedHash, err := ComputeGenesisBlockHash(genesisPath)
	if err != nil {
		logger.Printf("Warning: Failed to compute L2 genesis hash: %v\n", err)
	} else {
		l2GenesisHash = computedHash
		logger.Printf("Computed L2 genesis hash: %s\n", l2GenesisHash)
	}

	// Parse L1 block number from hex
	l1BlockNumber := int64(0)
	if l1Block.Number != "" && l1Block.Number != "0x0" {
		if _, err := fmt.Sscanf(l1Block.Number, "0x%x", &l1BlockNumber); err != nil {
			logger.Printf("Warning: Failed to parse L1 block number: %v\n", err)
		}
	}

	// Parse L1 timestamp from hex
	l1Timestamp := int64(0)
	if l1Block.Timestamp != "" && l1Block.Timestamp != "0x0" {
		if _, err := fmt.Sscanf(l1Block.Timestamp, "0x%x", &l1Timestamp); err != nil {
			logger.Printf("Warning: Failed to parse L1 timestamp: %v\n", err)
		}
	}

	// Compute batch inbox address based on L2 chain ID
	// Format: 0x00e5b74a... where the last bytes encode the chain ID
	batchInboxAddress := computeBatchInboxAddress(uint64(r.cfg.L2ChainID))

	// Get protocol versions address
	protocolVersionsAddress := ""
	if r.state.Superchain != nil {
		protocolVersionsAddress = r.state.Superchain.ProtocolVersionsProxy
	}

	// Create rollup.json with L1 genesis block info and computed L2 genesis hash
	rollupPath := filepath.Join(r.cfg.WorkDir, "rollup.json")
	rollup := map[string]interface{}{
		"genesis": map[string]interface{}{
			"l1": map[string]interface{}{
				"hash":   l1Block.Hash,
				"number": l1BlockNumber,
			},
			"l2": map[string]interface{}{
				"hash":   l2GenesisHash,
				"number": 0,
			},
			"l2_time": l1Timestamp,
			"system_config": map[string]interface{}{
				"batcherAddr":          r.cfg.Batcher,
				"overhead":             "0x0000000000000000000000000000000000000000000000000000000000000000",
				"scalar":               "0x010000000000000000000000000000000000000000000000000c3c9d00000558",
				"gasLimit":             60000000,
				"eip1559Params":        "0x0000000000000000",
				"operatorFeeParams":    "0x0000000000000000000000000000000000000000000000000000000000000000",
				"minBaseFee":           0,
				"daFootprintGasScalar": 0,
			},
		},
		"block_time":                r.cfg.L2BlockTime,
		"max_sequencer_drift":       600,
		"seq_window_size":           3600,
		"channel_timeout":           300,
		"l1_chain_id":               r.cfg.L1ChainID,
		"l2_chain_id":               r.cfg.L2ChainID,
		"regolith_time":             uint64(18446744073709551615),
		"canyon_time":               uint64(18446744073709551615),
		"delta_time":                uint64(18446744073709551615),
		"ecotone_time":              uint64(18446744073709551615),
		"fjord_time":                uint64(18446744073709551615),
		"granite_time":              uint64(18446744073709551615),
		"holocene_time":             uint64(18446744073709551615),
		"isthmus_time":              uint64(18446744073709551615),
		"batch_inbox_address":       batchInboxAddress,
		"deposit_contract_address":  r.state.OpChain.OptimismPortalProxy,
		"l1_system_config_address":  r.state.OpChain.SystemConfigProxy,
		"protocol_versions_address": protocolVersionsAddress,
		"chain_op_config": map[string]interface{}{
			"eip1559Elasticity":        6,
			"eip1559Denominator":       50,
			"eip1559DenominatorCanyon": 250,
		},
	}

	rollupData, err := json.MarshalIndent(rollup, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal rollup: %w", err)
	}

	if err := os.WriteFile(rollupPath, rollupData, 0644); err != nil {
		return fmt.Errorf("failed to write rollup file: %w", err)
	}

	logger.Printf("Rollup config written to: %s\n", rollupPath)

	r.state.GenesisGenerated = true

	return nil
}

// extractConfigs extracts and generates configuration files
func (r *Runner) extractConfigs(ctx context.Context, logger *StepLogger) error {
	logger.Printf("Extracting configuration files...\n")

	// Generate l1.json with all L1 contract addresses
	l1Path := filepath.Join(r.cfg.WorkDir, "l1.json")
	l1Addresses := map[string]interface{}{}

	// Superchain contracts
	if r.state.Superchain != nil {
		l1Addresses["SuperchainProxyAdminImpl"] = r.state.Superchain.SuperchainProxyAdmin
		l1Addresses["SuperchainConfigProxy"] = r.state.Superchain.SuperchainConfigProxy
		l1Addresses["SuperchainConfigImpl"] = r.state.Superchain.SuperchainConfigImpl
		l1Addresses["ProtocolVersionsProxy"] = r.state.Superchain.ProtocolVersionsProxy
		l1Addresses["ProtocolVersionsImpl"] = r.state.Superchain.ProtocolVersionsImpl
	}

	// Implementation contracts
	if r.state.Implementations != nil {
		l1Addresses["OpcmImpl"] = r.state.Implementations.OPCM
		l1Addresses["OpcmContractsContainerImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["OpcmGameTypeAdderImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["OpcmDeployerImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["OpcmUpgraderImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["OpcmInteropMigratorImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["OpcmStandardValidatorImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["DelayedWethImpl"] = r.state.Implementations.DelayedWETHImpl
		l1Addresses["OptimismPortalImpl"] = r.state.Implementations.OptimismPortalImpl
		l1Addresses["OptimismPortalInteropImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["EthLockboxImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["PreimageOracleImpl"] = r.state.Implementations.PreimageOracleSingleton
		l1Addresses["MipsImpl"] = r.state.Implementations.MipsSingleton
		l1Addresses["SystemConfigImpl"] = r.state.Implementations.SystemConfigImpl
		l1Addresses["L1CrossDomainMessengerImpl"] = r.state.Implementations.L1CrossDomainMessengerImpl
		l1Addresses["L1Erc721BridgeImpl"] = r.state.Implementations.L1ERC721BridgeImpl
		l1Addresses["L1StandardBridgeImpl"] = r.state.Implementations.L1StandardBridgeImpl
		l1Addresses["OptimismMintableErc20FactoryImpl"] = r.state.Implementations.OptimismMintableERC20Factory
		l1Addresses["DisputeGameFactoryImpl"] = r.state.Implementations.DisputeGameFactoryImpl
		l1Addresses["AnchorStateRegistryImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["FaultDisputeGameV2Impl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["PermissionedDisputeGameV2Impl"] = "0x0000000000000000000000000000000000000000"
	}

	// OP Chain contracts
	if r.state.OpChain != nil {
		l1Addresses["OpChainProxyAdminImpl"] = r.state.OpChain.ProxyAdmin
		l1Addresses["OptimismPortalProxy"] = r.state.OpChain.OptimismPortalProxy
		l1Addresses["AddressManagerImpl"] = r.state.OpChain.AddressManager
		l1Addresses["L1Erc721BridgeProxy"] = r.state.OpChain.L1ERC721BridgeProxy
		l1Addresses["SystemConfigProxy"] = r.state.OpChain.SystemConfigProxy
		l1Addresses["OptimismMintableErc20FactoryProxy"] = r.state.OpChain.OptimismMintableERC20Factory
		l1Addresses["L1StandardBridgeProxy"] = r.state.OpChain.L1StandardBridgeProxy
		l1Addresses["L1CrossDomainMessengerProxy"] = r.state.OpChain.L1CrossDomainMessengerProxy
		l1Addresses["EthLockboxProxy"] = r.state.OpChain.EthLockboxProxy
		l1Addresses["DisputeGameFactoryProxy"] = r.state.OpChain.DisputeGameFactoryProxy
		l1Addresses["AnchorStateRegistryProxy"] = r.state.OpChain.AnchorStateRegistryProxy
		l1Addresses["FaultDisputeGameImpl"] = r.state.OpChain.FaultDisputeGame
		l1Addresses["FaultDisputeGameCannonKonaImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["PermissionedDisputeGameImpl"] = r.state.OpChain.PermissionedDisputeGame
		l1Addresses["DelayedWethPermissionedGameProxy"] = r.state.OpChain.DelayedWETHPermissionedProxy
		l1Addresses["DelayedWethPermissionlessGameProxy"] = r.state.OpChain.DelayedWETHProxy
		l1Addresses["AltDAChallengeProxy"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["AltDAChallengeImpl"] = "0x0000000000000000000000000000000000000000"
		l1Addresses["L2OutputOracleProxy"] = "0x0000000000000000000000000000000000000000"
	}

	l1Data, err := json.MarshalIndent(l1Addresses, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal l1 addresses: %w", err)
	}

	if err := os.WriteFile(l1Path, l1Data, 0644); err != nil {
		return fmt.Errorf("failed to write l1.json: %w", err)
	}

	logger.Printf("L1 addresses written to: %s\n", l1Path)

	// Generate deploy-config.json
	deployConfigPath := filepath.Join(r.cfg.WorkDir, "deploy-config.json")
	batchInbox := computeBatchInboxAddress(uint64(r.cfg.L2ChainID))
	deployConfig := map[string]interface{}{
		"l1ChainID":                                r.cfg.L1ChainID,
		"l2ChainID":                                r.cfg.L2ChainID,
		"l1BlockTime":                              r.cfg.L1BlockTime,
		"l2BlockTime":                              r.cfg.L2BlockTime,
		"maxSequencerDrift":                        600,
		"sequencerWindowSize":                      3600,
		"channelTimeout":                           300,
		"p2pSequencerAddress":                      r.cfg.EOAAddress,
		"batchInboxAddress":                        batchInbox,
		"batchSenderAddress":                       r.cfg.Batcher,
		"l2OutputOracleSubmissionInterval":         120,
		"l2OutputOracleStartingBlockNumber":        0,
		"l2OutputOracleStartingTimestamp":          0,
		"l2OutputOracleProposer":                   r.cfg.Proposer,
		"l2OutputOracleChallenger":                 r.cfg.Challenger,
		"finalizationPeriodSeconds":                12,
		"proxyAdminOwner":                          r.cfg.EOAAddress,
		"baseFeeVaultRecipient":                    r.cfg.EOAAddress,
		"l1FeeVaultRecipient":                      r.cfg.EOAAddress,
		"sequencerFeeVaultRecipient":               r.cfg.EOAAddress,
		"finalSystemOwner":                         r.cfg.EOAAddress,
		"superchainConfigGuardian":                 r.cfg.Guardian,
		"portalGuardian":                           r.cfg.Guardian,
		"controller":                               r.cfg.EOAAddress,
		"baseFeeVaultMinimumWithdrawalAmount":      "0x8ac7230489e80000",
		"l1FeeVaultMinimumWithdrawalAmount":        "0x8ac7230489e80000",
		"sequencerFeeVaultMinimumWithdrawalAmount": "0x8ac7230489e80000",
		"baseFeeVaultWithdrawalNetwork":            0,
		"l1FeeVaultWithdrawalNetwork":              0,
		"sequencerFeeVaultWithdrawalNetwork":       0,
		"gasPriceOracleOverhead":                   2100,
		"gasPriceOracleScalar":                     1000000,
		"enableGovernance":                         true,
		"governanceTokenSymbol":                    "OP",
		"governanceTokenName":                      "Optimism",
		"governanceTokenOwner":                     r.cfg.EOAAddress,
		"l2GenesisBlockGasLimit":                   "0x1c9c380",
		"l2GenesisBlockBaseFeePerGas":              "0x3b9aca00",
		"eip1559Denominator":                       50,
		"eip1559DenominatorCanyon":                 250,
		"eip1559Elasticity":                        6,
		"l2GenesisRegolithTimeOffset":              "0x0",
		"systemConfigStartBlock":                   0,
		"requiredProtocolVersion":                  "0x0000000000000000000000000000000000000000000000000000000000000000",
		"recommendedProtocolVersion":               "0x0000000000000000000000000000000000000000000000000000000000000000",
		"faultGameAbsolutePrestate":                "0x0000000000000000000000000000000000000000000000000000000000000000",
		"faultGameMaxDepth":                        73,
		"faultGameSplitDepth":                      30,
		"faultGameClockExtension":                  r.cfg.ClockExtension,
		"faultGameMaxClockDuration":                r.cfg.MaxClockDuration,
		"faultGameGenesisBlock":                    0,
		"faultGameGenesisOutputRoot":               "0x0000000000000000000000000000000000000000000000000000000000000000",
		"faultGameWithdrawalDelay":                 r.cfg.WithdrawalDelaySeconds,
		"preimageOracleMinProposalSize":            126000,
		"preimageOracleChallengePeriod":            r.cfg.ChallengePeriodSeconds,
	}

	deployConfigData, err := json.MarshalIndent(deployConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal deploy config: %w", err)
	}

	if err := os.WriteFile(deployConfigPath, deployConfigData, 0644); err != nil {
		return fmt.Errorf("failed to write deploy-config.json: %w", err)
	}

	logger.Printf("Deploy config written to: %s\n", deployConfigPath)

	// Generate l1-chainconfig.json for rollup-node
	// This is needed because RSK chains are not known chains in op-node
	l1ChainConfigPath := filepath.Join(r.cfg.WorkDir, "l1-chainconfig.json")
	l1ChainConfig := map[string]interface{}{
		"chainId":             r.cfg.L1ChainID,
		"homesteadBlock":      0,
		"eip150Block":         0,
		"eip155Block":         0,
		"eip158Block":         0,
		"byzantiumBlock":      0,
		"constantinopleBlock": 0,
		"petersburgBlock":     0,
		"istanbulBlock":       0,
		"muirGlacierBlock":    0,
		"berlinBlock":         0,
		"londonBlock":         0,
	}

	l1ChainConfigData, err := json.MarshalIndent(l1ChainConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal l1 chain config: %w", err)
	}

	if err := os.WriteFile(l1ChainConfigPath, l1ChainConfigData, 0644); err != nil {
		return fmt.Errorf("failed to write l1-chainconfig.json: %w", err)
	}

	logger.Printf("L1 chain config written to: %s\n", l1ChainConfigPath)

	return nil
}

// resolveDisputeGamesFromFactory reads PermissionedDisputeGame back from the
// DisputeGameFactory on-chain. The OPCM v1 deploy path registers game
// implementations in the factory but does not return them in the output struct,
// so the script returns zero addresses. We query the factory directly and update
// the deployment state accordingly.
func (r *Runner) resolveDisputeGamesFromFactory(ctx context.Context, addresses map[string]string, logger *StepLogger) error {
	factoryAddr := r.state.OpChain.DisputeGameFactoryProxy
	if factoryAddr == "" {
		return fmt.Errorf("DisputeGameFactoryProxy not set in state")
	}

	// gameImpls(uint32) returns address — game type 1 is PERMISSIONED_CANNON
	cmd := exec.CommandContext(ctx, "cast", "call",
		"--rpc-url", r.cfg.RPCURL,
		factoryAddr,
		"gameImpls(uint32)(address)",
		"1",
	)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("cast call gameImpls(1) failed: %w", err)
	}
	addr := strings.TrimSpace(string(out))

	if addr != "" && addr != "0x0000000000000000000000000000000000000000" {
		logger.Printf("Resolved PermissionedDisputeGame from factory: %s\n", addr)
		r.state.OpChain.PermissionedDisputeGame = addr
		addresses["PermissionedDisputeGame"] = addr
	} else {
		logger.Printf("No PermissionedDisputeGame registered in factory\n")
	}

	return nil
}

// deployDisputeGameStub deploys the PermissionedDisputeGameStub contract to an existing chain,
// registers it in the DisputeGameFactory, and sets the respected game type.
func (r *Runner) deployDisputeGameStub(ctx context.Context, logger *StepLogger) error {
	// Validate that required OP chain addresses exist in state
	if r.state.OpChain == nil {
		return fmt.Errorf("no OP chain state found - run full deployment first or ensure workdir has deployment_state.json")
	}
	if r.state.OpChain.DisputeGameFactoryProxy == "" {
		return fmt.Errorf("DisputeGameFactoryProxy not found in state")
	}
	if r.state.OpChain.AnchorStateRegistryProxy == "" {
		return fmt.Errorf("AnchorStateRegistryProxy not found in state")
	}
	if r.state.OpChain.DelayedWETHPermissionedProxy == "" {
		return fmt.Errorf("DelayedWETHPermissionedProxy not found in state")
	}

	logger.Printf("Deploying PermissionedDisputeGameStub for chain %d...\n", r.cfg.L2ChainID)
	logger.Printf("  DisputeGameFactoryProxy: %s\n", r.state.OpChain.DisputeGameFactoryProxy)
	logger.Printf("  AnchorStateRegistryProxy: %s\n", r.state.OpChain.AnchorStateRegistryProxy)
	logger.Printf("  DelayedWETHPermissionedProxy: %s\n", r.state.OpChain.DelayedWETHPermissionedProxy)
	logger.Printf("  Proposer: %s\n", r.cfg.Proposer)
	logger.Printf("  Challenger: %s\n", r.cfg.Challenger)

	// Step 1: Deploy the PermissionedDisputeGameStub implementation
	logger.Printf("\n=== Step 1: Deploy PermissionedDisputeGameStub implementation ===\n")

	// Use cast abi-encode to encode the DeployDisputeGame.Input struct
	abiType := "f((string,string,uint32,bytes32,uint256,uint256,uint64,uint64,address,address,address,uint256,address,address))"
	abiValue := fmt.Sprintf("(%s,%s,%d,%s,%d,%d,%d,%d,%s,%s,%s,%d,%s,%s)",
		"develop",                     // release
		"PermissionedDisputeGameStub", // gameKind
		1,                             // gameType (PERMISSIONED_CANNON)
		"0x0000000000000000000000000000000000000000000000000000000000000001", // absolutePrestate (placeholder)
		0, // maxGameDepth (not used by stub)
		0, // splitDepth (not used by stub)
		0, // clockExtension (not used by stub)
		0, // maxClockDuration (not used by stub)
		r.state.OpChain.DelayedWETHPermissionedProxy, // delayedWethProxy
		r.state.OpChain.AnchorStateRegistryProxy,     // anchorStateRegistryProxy
		"0x0000000000000000000000000000000000000000", // vmAddress (not used by stub)
		r.cfg.L2ChainID,  // l2ChainId
		r.cfg.Proposer,   // proposer
		r.cfg.Challenger, // challenger
	)

	abiEncodeCmd := exec.CommandContext(ctx, "cast", "abi-encode", abiType, abiValue)
	encodedInputBytes, err := abiEncodeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to ABI-encode DeployDisputeGame input: %w", err)
	}
	encodedInput := strings.TrimSpace(string(encodedInputBytes))
	logger.Printf("Encoded input: %s\n", encodedInput[:66]+"...")

	result, err := r.forge.RunScript(ctx, ForgeScriptInput{
		Script:  "scripts/deploy/DeployDisputeGame.s.sol:DeployDisputeGame",
		Sig:     "runWithBytes(bytes)",
		SigArgs: []string{encodedInput},
	}, logger)
	if err != nil {
		return fmt.Errorf("DeployDisputeGame script failed: %w", err)
	}

	// Extract the deployed address from script output
	outputAddrs := ParseScriptOutput(result.Output)
	logger.Printf("\nAddresses from script output:\n")
	for name, addr := range outputAddrs {
		logger.Printf("  %s: %s\n", name, addr)
	}

	// Also try to extract from broadcast
	var disputeGameImplAddr string
	if result.BroadcastDir != "" {
		broadcast, err := r.forge.ParseBroadcastJSON(result.BroadcastDir, result.SigName)
		if err != nil {
			logger.Printf("Warning: failed to parse broadcast: %v\n", err)
		} else {
			addresses, err := ExtractAllContractAddresses(broadcast)
			if err != nil {
				logger.Printf("Warning: failed to extract addresses: %v\n", err)
			} else {
				for name, addr := range addresses {
					logger.Printf("  Broadcast: %s: %s\n", name, addr)
				}
				if addr, ok := addresses["PermissionedDisputeGameStub"]; ok {
					disputeGameImplAddr = addr
				}
			}
		}
	}

	// Prefer script output over broadcast
	if addr, ok := outputAddrs["disputeGameImpl"]; ok && addr != "" {
		disputeGameImplAddr = addr
	}
	if addr, ok := outputAddrs["PermissionedDisputeGameStubImpl"]; ok && addr != "" {
		disputeGameImplAddr = addr
	}

	if disputeGameImplAddr == "" {
		return fmt.Errorf("failed to extract PermissionedDisputeGameStub address from deployment output")
	}

	logger.Printf("\nPermissionedDisputeGameStub deployed at: %s\n", disputeGameImplAddr)

	// Step 2: Register the game in DisputeGameFactory and set respected game type
	logger.Printf("\n=== Step 2: Register in DisputeGameFactory and set respected game type ===\n")

	// Build gameArgs for setImplementation using abi.encodePacked layout (tightly packed, NOT ABI-padded).
	// Layout: bytes32(absolutePrestate) || address(vm) || address(anchorStateRegistry) || address(weth) || uint256(l2ChainId) || address(proposer) || address(challenger)
	// Total: 32 + 20 + 20 + 20 + 32 + 20 + 20 = 164 bytes (matches LibGameArgs.PERMISSIONED_ARGS_LENGTH)
	trimAddr := func(addr string) string {
		return strings.ToLower(strings.TrimPrefix(addr, "0x"))
	}
	gameArgs := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000001" + // absolutePrestate (32 bytes)
		"0000000000000000000000000000000000000000" + // vm address (20 bytes, placeholder)
		trimAddr(r.state.OpChain.AnchorStateRegistryProxy) + // anchorStateRegistry (20 bytes)
		trimAddr(r.state.OpChain.DelayedWETHPermissionedProxy) + // weth (20 bytes)
		fmt.Sprintf("%064x", r.cfg.L2ChainID) + // l2ChainId (32 bytes)
		trimAddr(r.cfg.Proposer) + // proposer (20 bytes)
		trimAddr(r.cfg.Challenger) // challenger (20 bytes)

	// Use SetDisputeGameImpl.s.sol to register the game
	// We need to run this with the factory, impl, gameType, and gameArgs
	result, err = r.forge.RunScript(ctx, ForgeScriptInput{
		Script: "scripts/deploy/SetDisputeGameImpl.s.sol:SetDisputeGameImpl",
		Sig:    "run(address,address,uint32,address,bytes)",
		SigArgs: []string{
			r.state.OpChain.DisputeGameFactoryProxy,
			disputeGameImplAddr,
			"1", // GameType PERMISSIONED_CANNON
			r.state.OpChain.AnchorStateRegistryProxy,
			gameArgs,
		},
	}, logger)
	if err != nil {
		// The SetDisputeGameImpl script might have a different interface.
		// Fall back to using cast send directly.
		logger.Printf("SetDisputeGameImpl script failed, falling back to direct cast send: %v\n", err)

		// Call setImplementation(uint32,address,bytes) on the factory
		logger.Printf("Calling DisputeGameFactory.setImplementation...\n")
		gasPrice, gpErr := getGasPrice(ctx, r.cfg.RPCURL)
		if gpErr != nil {
			return fmt.Errorf("failed to get gas price: %w", gpErr)
		}
		setImplCmd := exec.CommandContext(ctx, "cast", "send",
			"--rpc-url", r.cfg.RPCURL,
			"--private-key", r.cfg.PrivateKeyWith0x(),
			"--legacy",
			"--gas-price", gasPrice,
			r.state.OpChain.DisputeGameFactoryProxy,
			"setImplementation(uint32,address,bytes)",
			"1", // GameType PERMISSIONED_CANNON
			disputeGameImplAddr,
			gameArgs,
		)
		setImplCmd.Stdout = logger
		setImplCmd.Stderr = logger
		if err := setImplCmd.Run(); err != nil {
			return fmt.Errorf("failed to call setImplementation: %w", err)
		}
		logger.Printf("setImplementation called successfully\n")

		// Set respected game type in AnchorStateRegistry
		logger.Printf("Calling AnchorStateRegistry.setRespectedGameType...\n")
		gasPrice2, gpErr2 := getGasPrice(ctx, r.cfg.RPCURL)
		if gpErr2 != nil {
			return fmt.Errorf("failed to get gas price: %w", gpErr2)
		}
		setGameTypeCmd := exec.CommandContext(ctx, "cast", "send",
			"--rpc-url", r.cfg.RPCURL,
			"--private-key", r.cfg.PrivateKeyWith0x(),
			"--legacy",
			"--gas-price", gasPrice2,
			r.state.OpChain.AnchorStateRegistryProxy,
			"setRespectedGameType(uint32)",
			"1", // GameType PERMISSIONED_CANNON
		)
		setGameTypeCmd.Stdout = logger
		setGameTypeCmd.Stderr = logger
		if err := setGameTypeCmd.Run(); err != nil {
			return fmt.Errorf("failed to call setRespectedGameType: %w", err)
		}
		logger.Printf("setRespectedGameType called successfully\n")
	}

	// Update state
	if r.state.OpChain != nil {
		r.state.OpChain.PermissionedDisputeGame = disputeGameImplAddr
	}

	logger.Printf("\n=== Dispute Game Stub deployment complete! ===\n")
	logger.Printf("  PermissionedDisputeGameStub: %s\n", disputeGameImplAddr)
	logger.Printf("  Registered in DisputeGameFactory: %s\n", r.state.OpChain.DisputeGameFactoryProxy)
	logger.Printf("  GameType: 1 (PERMISSIONED_CANNON)\n")

	return nil
}

// skipDisputeGame checks if the dispute game stub is already deployed
func (r *Runner) skipDisputeGame(ctx context.Context) (bool, string, error) {
	if r.state.OpChain == nil || r.state.OpChain.PermissionedDisputeGame == "" {
		return false, "", nil
	}

	hasCode, err := r.checker.HasCode(ctx, r.state.OpChain.PermissionedDisputeGame)
	if err != nil {
		return false, "", err
	}

	if hasCode {
		return true, fmt.Sprintf("PermissionedDisputeGame at %s has code", r.state.OpChain.PermissionedDisputeGame), nil
	}
	return false, "", nil
}
