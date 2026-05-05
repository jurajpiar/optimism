package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"
)

const (
	// EIP170Limit is the maximum deployed bytecode size in bytes.
	EIP170Limit = 24576

	// DefaultGasLimit is the default chain block gas limit used for deployment
	// gas estimation (6.8M, matching RSK's typical limit).
	DefaultGasLimit = 6_800_000

	// Gas cost constants for deployment estimation.
	gasTxBase       = 21_000 // base transaction cost
	gasCreate       = 32_000 // CREATE/CREATE2 opcode cost
	gasPerCodeByte  = 200    // code deposit cost per deployed byte (EIP-170)
	gasPerZeroByte  = 4      // calldata cost per zero byte (EIP-2028)
	gasPerNonZero   = 16     // calldata cost per non-zero byte (EIP-2028)
)

// contractEntry defines a contract to check, mapping to its forge artifact.
type contractEntry struct {
	// Name is the contract name (matches the JSON filename inside the artifact dir).
	Name string
	// Source is the Solidity source file stem (directory name under forge-artifacts/).
	// When empty, defaults to Name.
	Source string
	// Group is the deployment phase label.
	Group string
}

// source returns the effective source file stem.
func (c contractEntry) source() string {
	if c.Source != "" {
		return c.Source
	}
	return c.Name
}

// ContractSizeInfo holds size data for a single contract.
type ContractSizeInfo struct {
	Name          string
	Group         string
	InitcodeBytes int
	DeployedBytes int
	EstimatedGas  uint64 // minimum deployment gas estimate
	ArtifactFound bool
}

// ExceedsEIP170 returns true if the deployed bytecode exceeds the limit.
func (c ContractSizeInfo) ExceedsEIP170() bool {
	return c.ArtifactFound && c.DeployedBytes > EIP170Limit
}

// ExceedsGasLimit returns true if the estimated deployment gas exceeds the given limit.
func (c ContractSizeInfo) ExceedsGasLimit(limit uint64) bool {
	return c.ArtifactFound && c.EstimatedGas > limit
}

// estimateDeployGas returns a lower-bound gas estimate for deploying a contract.
// This accounts for tx base cost, CREATE opcode, calldata (initcode), and code
// deposit cost but NOT constructor execution, so real gas will be higher.
func estimateDeployGas(initcode []byte, deployedBytes int) uint64 {
	gas := uint64(gasTxBase + gasCreate)

	// Calldata cost for initcode bytes
	for _, b := range initcode {
		if b == 0 {
			gas += gasPerZeroByte
		} else {
			gas += gasPerNonZero
		}
	}

	// Code deposit cost for deployed bytecode
	gas += uint64(deployedBytes) * gasPerCodeByte

	return gas
}

// deployedContracts returns the curated list of contracts that the tool deploys.
func deployedContracts() []contractEntry {
	return []contractEntry{
		// Superchain (DeploySuperchain.s.sol)
		{Name: "SuperchainConfig", Group: "Superchain"},
		{Name: "ProtocolVersions", Group: "Superchain"},
		{Name: "ProxyAdmin", Group: "Superchain"},
		{Name: "Proxy", Group: "Superchain"},

		// Implementations (DeployImplementations.s.sol)
		{Name: "OPContractsManager", Group: "Implementations"},
		{Name: "OPContractsManagerContractsContainer", Source: "OPContractsManager", Group: "Implementations"},
		{Name: "OPContractsManagerGameTypeAdder", Source: "OPContractsManager", Group: "Implementations"},
		{Name: "OPContractsManagerDeployer", Source: "OPContractsManager", Group: "Implementations"},
		{Name: "OPContractsManagerUpgrader", Source: "OPContractsManager", Group: "Implementations"},
		{Name: "OPContractsManagerInteropMigrator", Source: "OPContractsManager", Group: "Implementations"},
		{Name: "OPContractsManagerStandardValidator", Group: "Implementations"},
		{Name: "DelayedWETH", Group: "Implementations"},
		{Name: "OptimismPortal2", Group: "Implementations"},
		{Name: "OptimismPortalInterop", Group: "Implementations"},
		{Name: "EthLockbox", Group: "Implementations"},
		{Name: "PreimageOracle", Group: "Implementations"},
		{Name: "MIPS64", Group: "Implementations"},
		{Name: "SystemConfig", Group: "Implementations"},
		{Name: "L1CrossDomainMessenger", Group: "Implementations"},
		{Name: "L1ERC721Bridge", Group: "Implementations"},
		{Name: "L1StandardBridge", Group: "Implementations"},
		{Name: "OptimismMintableERC20Factory", Group: "Implementations"},
		{Name: "DisputeGameFactory", Group: "Implementations"},
		{Name: "AnchorStateRegistry", Group: "Implementations"},
		{Name: "FaultDisputeGame", Group: "Implementations"},
		{Name: "PermissionedDisputeGame", Group: "Implementations"},
	}
}

// forgeArtifact is the minimal structure we need from a Foundry artifact JSON.
type forgeArtifact struct {
	Bytecode         forgeArtifactBytecode `json:"bytecode"`
	DeployedBytecode forgeArtifactBytecode `json:"deployedBytecode"`
}

type forgeArtifactBytecode struct {
	Object string `json:"object"`
}

// bytecodeSize returns the byte count from a hex-encoded bytecode string (0x-prefixed).
func bytecodeSize(hexStr string) int {
	s := strings.TrimPrefix(hexStr, "0x")
	return len(s) / 2
}

// decodeHexBytes decodes a 0x-prefixed hex string into raw bytes.
func decodeHexBytes(hexStr string) []byte {
	s := strings.TrimPrefix(hexStr, "0x")
	if len(s)%2 != 0 {
		s = "0" + s
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		out[i] = hexDigit(s[2*i])<<4 | hexDigit(s[2*i+1])
	}
	return out
}

func hexDigit(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

// readContractSizes reads artifact JSONs and returns size info for each contract.
func readContractSizes(artifactsDir string, contracts []contractEntry) []ContractSizeInfo {
	sizes := make([]ContractSizeInfo, len(contracts))
	for i, c := range contracts {
		sizes[i] = ContractSizeInfo{
			Name:  c.Name,
			Group: c.Group,
		}

		path := filepath.Join(artifactsDir, c.source()+".sol", c.Name+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue // ArtifactFound stays false
		}

		var artifact forgeArtifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			continue
		}

		initcodeRaw := decodeHexBytes(artifact.Bytecode.Object)
		deployedBytes := bytecodeSize(artifact.DeployedBytecode.Object)

		sizes[i].ArtifactFound = true
		sizes[i].InitcodeBytes = len(initcodeRaw)
		sizes[i].DeployedBytes = deployedBytes
		sizes[i].EstimatedGas = estimateDeployGas(initcodeRaw, deployedBytes)
	}
	return sizes
}

// sizeReport holds the results of printContractSizes.
type sizeReport struct {
	EIP170Violations int
	GasViolations    int
}

// printContractSizes prints a formatted table of contract sizes and estimated
// deployment gas to stdout. gasLimit is the chain block gas limit to check
// against (0 disables the gas check).
func printContractSizes(sizes []ContractSizeInfo, gasLimit uint64) sizeReport {
	printStatus("═══════════════════════════════════════════════════════════════════════════")
	printStatus("Contract Sizes & Estimated Deployment Gas")
	if gasLimit > 0 {
		printStatus("Gas limit: %s", formatGas(gasLimit))
	}
	printStatus("═══════════════════════════════════════════════════════════════════════════")

	var report sizeReport
	currentGroup := ""
	maxNameLen := 0
	for _, s := range sizes {
		if len(s.Name) > maxNameLen {
			maxNameLen = len(s.Name)
		}
	}

	for _, s := range sizes {
		if s.Group != currentGroup {
			currentGroup = s.Group
			fmt.Println()
			printStatus("%s:", currentGroup)
		}

		if !s.ArtifactFound {
			fmt.Printf("  %-*s  %s(not found)%s\n", maxNameLen, s.Name, colorCyan, colorReset)
			continue
		}

		// Size check
		sizePct := s.DeployedBytes * 100 / EIP170Limit
		sizeMarker := ""
		sizeColor := colorReset
		if s.DeployedBytes > EIP170Limit {
			sizeMarker = " EXCEEDS"
			sizeColor = colorRed
			report.EIP170Violations++
		} else if sizePct >= 90 {
			sizeMarker = " !"
			sizeColor = colorYellow
		}

		// Gas check
		gasMarker := ""
		gasColor := colorReset
		if gasLimit > 0 && s.EstimatedGas > gasLimit {
			gasMarker = " EXCEEDS"
			gasColor = colorRed
			report.GasViolations++
		} else if gasLimit > 0 && s.EstimatedGas*100/gasLimit >= 90 {
			gasMarker = " !"
			gasColor = colorYellow
		}

		fmt.Printf("  %-*s  %s%6d / %d bytes (%3d%%)%s%s  %sgas >= %s%s%s\n",
			maxNameLen, s.Name,
			sizeColor, s.DeployedBytes, EIP170Limit, sizePct, sizeMarker, colorReset,
			gasColor, formatGas(s.EstimatedGas), gasMarker, colorReset,
		)
	}

	fmt.Println()
	if report.EIP170Violations > 0 {
		printError("%d contract(s) exceed the EIP-170 limit (%d bytes)", report.EIP170Violations, EIP170Limit)
	} else {
		printStatus("All contracts within EIP-170 limit (%d bytes)", EIP170Limit)
	}
	if gasLimit > 0 {
		if report.GasViolations > 0 {
			printError("%d contract(s) estimated to exceed gas limit (%s)", report.GasViolations, formatGas(gasLimit))
		} else {
			printStatus("All contracts estimated within gas limit (%s)", formatGas(gasLimit))
		}
	}
	printStatus("═══════════════════════════════════════════════════════════════════════════")

	return report
}

// formatGas formats a gas value with comma separators (e.g. 6,800,000).
func formatGas(gas uint64) string {
	s := fmt.Sprintf("%d", gas)
	// Insert commas from the right
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	lead := n % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(s[:lead])
	for i := lead; i < n; i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// logContractSizes writes contract sizes to the step logger (for log files).
func logContractSizes(logger *StepLogger, sizes []ContractSizeInfo, gasLimit uint64) {
	logger.Printf("\n=== Contract Sizes & Estimated Deployment Gas ===\n")
	for _, s := range sizes {
		if !s.ArtifactFound {
			logger.Printf("  %-40s  (not found)\n", s.Name)
			continue
		}
		pct := s.DeployedBytes * 100 / EIP170Limit
		logger.Printf("  %-40s  %6d / %d bytes  (%3d%%)  gas >= %s\n",
			s.Name, s.DeployedBytes, EIP170Limit, pct, formatGas(s.EstimatedGas))
	}
	if gasLimit > 0 {
		logger.Printf("\nChain gas limit: %s\n", formatGas(gasLimit))
	}
}

// runEstimateSizes is the action for --estimate-sizes mode.
// It reports contract sizes from forge artifacts, only rebuilding when sources have changed.
func runEstimateSizes(cliCtx *cli.Context) error {
	workspaceRoot, err := findWorkspaceRoot()
	if err != nil {
		return fmt.Errorf("failed to find workspace root: %w", err)
	}

	contractsDir := filepath.Join(workspaceRoot, "optimism", "packages", "contracts-bedrock")
	buildHashPath := filepath.Join(contractsDir, buildHashFileName)

	// Check if a rebuild is needed by comparing source hashes
	needsBuild := true
	currentHash, err := hashContractsDir(contractsDir)
	if err == nil {
		storedHash, err := readStoredBuildHash(buildHashPath)
		if err == nil && storedHash != "" && currentHash == storedHash {
			needsBuild = false
		}
	}

	if needsBuild {
		printStep("Building contracts (sources changed or no prior build)...")
		env := os.Environ()
		if profile := cliCtx.String("foundry-profile"); profile != "" {
			env = append(env, "FOUNDRY_PROFILE="+profile)
			printStatus("Using Foundry profile: %s", profile)
		}

		buildCmd := exec.CommandContext(context.Background(), "just", "forge-build")
		buildCmd.Dir = contractsDir
		buildCmd.Env = env
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			return fmt.Errorf("forge build failed: %w", err)
		}

		// Update build hash after successful build
		if currentHash != "" {
			_ = writeStoredBuildHash(buildHashPath, currentHash)
		}
	} else {
		printStatus("Contracts unchanged, using cached build artifacts")
	}

	// Read and print sizes
	artifactsDir := filepath.Join(contractsDir, "forge-artifacts")
	gasLimit := uint64(cliCtx.Int("gas-limit"))
	sizes := readContractSizes(artifactsDir, deployedContracts())
	report := printContractSizes(sizes, gasLimit)

	total := report.EIP170Violations + report.GasViolations
	if total > 0 {
		return fmt.Errorf("%d violation(s) found", total)
	}
	return nil
}
