package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum-optimism/optimism/rsk/op-deployer/keyderive"
	"github.com/ethereum-optimism/optimism/rsk/op-deployer/redact"
	"github.com/urfave/cli/v2"
)

// Config holds all configuration for the deployment process
type Config struct {
	// Chain configuration
	L1ChainID   int
	L2ChainID   int
	L1BlockTime int
	L2BlockTime int
	RPCURL      string

	// Credentials
	PrivateKey string
	EOAAddress string

	// Role addresses (default to EOAAddress, or derived from PrivateKey)
	SuperchainProxyAdminOwner string
	ProtocolVersionsOwner     string
	Guardian                  string
	UpgradeController         string
	Challenger                string
	Proposer                  string
	Batcher                   string

	// Derived private keys for batcher and proposer (hex, no 0x prefix).
	// Populated when addresses are derived from the master private key.
	BatcherPrivateKey  string
	ProposerPrivateKey string

	// Paths
	WorkspaceFolder string
	WorkDir         string

	// Funding options
	FunderPrivateKey string
	FundAmount       string
	MinBalance       string

	// Options
	DeployCREATE2     bool
	Verbose           bool
	ForceRedeploy     bool
	FundRoles         bool   // Fund derived batcher/proposer accounts before deployment
	FoundryProfile    string // Foundry profile for contract compilation (e.g., "rsk")
	UseSplitDeploy    bool   // Use split deployment for RSK compatibility
	SkipFaultProofs   bool   // Skip deploying large fault proof contracts (RSK compatibility)
	DeployDisputeGame bool   // Deploy dispute game stub to existing chain
	GasLimit          int    // Chain block gas limit for deployment gas estimation

	// Fault-proof withdrawal delays (seconds). -1 means "use auto" (0 for testnet, mainnet defaults otherwise).
	ProofMaturityDelaySeconds       int
	DisputeGameFinalityDelaySeconds int
	WithdrawalDelaySeconds          int

	// Dispute game clock parameters (seconds).
	MaxClockDuration       int
	ClockExtension         int
	ChallengePeriodSeconds int

	// Internal
	masterLogFile *os.File
}

// validateDeployFlags checks that flags required for deployment are set.
// These were previously marked Required on the flag definitions but are now
// optional so that --estimate-sizes can run without them.
func validateDeployFlags(cliCtx *cli.Context) error {
	required := []string{"l1-chain-id", "l2-chain-id", "rpc-url", "private-key", "eoa-address"}
	for _, name := range required {
		if !cliCtx.IsSet(name) {
			return fmt.Errorf("required flag %q not set", name)
		}
	}
	return nil
}

// NewConfigFromCLI creates a Config from CLI context
func NewConfigFromCLI(cliCtx *cli.Context) (*Config, error) {
	if err := validateDeployFlags(cliCtx); err != nil {
		return nil, err
	}

	// Get workspace folder - find the rollup root by looking for go.mod
	workspaceFolder, err := findWorkspaceRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to find workspace root: %w", err)
	}

	// Validate RPC URL
	rpcURL := cliCtx.String("rpc-url")
	if !regexp.MustCompile(`^https?://`).MatchString(rpcURL) {
		return nil, fmt.Errorf("RPC_URL must start with http:// or https://, got: %s", rpcURL)
	}

	// Ensure private key has proper format (normalize to without 0x prefix for storage)
	privateKey := cliCtx.String("private-key")
	privateKey = strings.TrimPrefix(privateKey, "0x")

	eoaAddress := cliCtx.String("eoa-address")

	// Build work directory path
	workDir := cliCtx.String("workdir")
	if workDir == "" {
		dateToday := time.Now().Format("02_01_2006")
		folderName := fmt.Sprintf("%d_%s_%d", cliCtx.Int("l1-chain-id"), dateToday, cliCtx.Int("l2-chain-id"))
		workDir = filepath.Join(workspaceFolder, folderName)
	}

	// Derive batcher/proposer addresses from the master private key when not
	// explicitly set. This gives each role its own L1 nonce space.
	batcherAddr := cliCtx.String("batcher")
	proposerAddr := cliCtx.String("proposer")
	var batcherPrivKey, proposerPrivKey string

	if batcherAddr == "" {
		key, addr, err := keyderive.DeriveKey(privateKey, keyderive.RoleBatcher)
		if err != nil {
			return nil, fmt.Errorf("failed to derive batcher key: %w", err)
		}
		batcherAddr = addr.Hex()
		batcherPrivKey = key
	}
	if proposerAddr == "" {
		key, addr, err := keyderive.DeriveKey(privateKey, keyderive.RoleProposer)
		if err != nil {
			return nil, fmt.Errorf("failed to derive proposer key: %w", err)
		}
		proposerAddr = addr.Hex()
		proposerPrivKey = key
	}

	cfg := &Config{
		L1ChainID:   cliCtx.Int("l1-chain-id"),
		L2ChainID:   cliCtx.Int("l2-chain-id"),
		L1BlockTime: cliCtx.Int("l1-block-time"),
		L2BlockTime: cliCtx.Int("l2-block-time"),
		RPCURL:      rpcURL,
		PrivateKey:  privateKey,
		EOAAddress:  eoaAddress,

		// Role addresses default to EOA, except batcher/proposer which are derived
		SuperchainProxyAdminOwner: getOrDefault(cliCtx.String("superchain-proxy-admin-owner"), eoaAddress),
		ProtocolVersionsOwner:     getOrDefault(cliCtx.String("protocol-versions-owner"), eoaAddress),
		Guardian:                  getOrDefault(cliCtx.String("guardian"), eoaAddress),
		UpgradeController:         getOrDefault(cliCtx.String("upgrade-controller"), eoaAddress),
		Challenger:                getOrDefault(cliCtx.String("challenger"), eoaAddress),
		Proposer:                  proposerAddr,
		Batcher:                   batcherAddr,

		BatcherPrivateKey:  batcherPrivKey,
		ProposerPrivateKey: proposerPrivKey,

		WorkspaceFolder: workspaceFolder,
		WorkDir:         workDir,

		// Funding options
		FunderPrivateKey: strings.TrimPrefix(cliCtx.String("funder-private-key"), "0x"),
		FundAmount:       cliCtx.String("fund-amount"),
		MinBalance:       cliCtx.String("min-balance"),

		DeployCREATE2:     cliCtx.Bool("deploy-create2"),
		Verbose:           cliCtx.Bool("verbose"),
		ForceRedeploy:     cliCtx.Bool("force-redeploy"),
		FundRoles:         cliCtx.Bool("fund-roles"),
		FoundryProfile:    cliCtx.String("foundry-profile"),
		UseSplitDeploy:    cliCtx.Bool("use-split-deploy"),
		SkipFaultProofs:   cliCtx.Bool("skip-fault-proofs"),
		DeployDisputeGame: cliCtx.Bool("deploy-dispute-game"),
		GasLimit:          cliCtx.Int("gas-limit"),

		ProofMaturityDelaySeconds:       cliCtx.Int("proof-maturity-delay"),
		DisputeGameFinalityDelaySeconds: cliCtx.Int("dispute-game-finality-delay"),
		WithdrawalDelaySeconds:          cliCtx.Int("withdrawal-delay"),

		MaxClockDuration:       cliCtx.Int("max-clock-duration"),
		ClockExtension:         cliCtx.Int("clock-extension"),
		ChallengePeriodSeconds: cliCtx.Int("challenge-period"),
	}

	// Auto-detect RSK testnet (chain ID 31) and set appropriate defaults
	if cfg.L1ChainID == 31 || cfg.L1ChainID == 33 {
		if cfg.FoundryProfile == "" {
			cfg.FoundryProfile = "rsk"
		}
		if !cliCtx.IsSet("use-split-deploy") {
			cfg.UseSplitDeploy = true
		}
		// RSK testnet accounts have limited balances; use conservative
		// funding defaults so the deployer keeps enough for gas.
		if !cliCtx.IsSet("fund-amount") {
			cfg.FundAmount = "0.01ether"
		}
		if !cliCtx.IsSet("min-balance") {
			cfg.MinBalance = "0.01ether"
		}
		// Testnet: near-instant withdrawal finalization unless explicitly overridden.
		// Minimum is 1 because the Solidity scripts reject 0 as "not set".
		if !cliCtx.IsSet("proof-maturity-delay") {
			cfg.ProofMaturityDelaySeconds = 60
		}
		if !cliCtx.IsSet("dispute-game-finality-delay") {
			cfg.DisputeGameFinalityDelaySeconds = 60
		}
		if !cliCtx.IsSet("withdrawal-delay") {
			cfg.WithdrawalDelaySeconds = 60
		}
		if !cliCtx.IsSet("max-clock-duration") {
			cfg.MaxClockDuration = 300 // 5 minutes
		}
		if !cliCtx.IsSet("clock-extension") {
			cfg.ClockExtension = 60 // 1 minute
		}
		if !cliCtx.IsSet("challenge-period") {
			cfg.ChallengePeriodSeconds = 120 // 2 minutes
		}
	} else {
		// Non-testnet: use mainnet-safe defaults unless explicitly overridden
		if !cliCtx.IsSet("proof-maturity-delay") {
			cfg.ProofMaturityDelaySeconds = 604800 // 7 days
		}
		if !cliCtx.IsSet("dispute-game-finality-delay") {
			cfg.DisputeGameFinalityDelaySeconds = 302400 // 3.5 days
		}
		if !cliCtx.IsSet("withdrawal-delay") {
			cfg.WithdrawalDelaySeconds = 604800 // 7 days
		}
		if !cliCtx.IsSet("max-clock-duration") {
			cfg.MaxClockDuration = 302400 // 3.5 days
		}
		if !cliCtx.IsSet("clock-extension") {
			cfg.ClockExtension = 10800 // 3 hours
		}
		if !cliCtx.IsSet("challenge-period") {
			cfg.ChallengePeriodSeconds = 86400 // 1 day
		}
	}

	// Validate: maxClockDuration must be >= max(clockExtension*2, clockExtension+challengePeriod).
	// FaultDisputeGame.initialize() enforces this and reverts with InvalidClockExtension otherwise.
	splitDepthExt := cfg.ClockExtension * 2
	maxGameDepthExt := cfg.ClockExtension + cfg.ChallengePeriodSeconds
	maxExt := splitDepthExt
	if maxGameDepthExt > maxExt {
		maxExt = maxGameDepthExt
	}
	if cfg.MaxClockDuration < maxExt {
		return nil, fmt.Errorf(
			"max-clock-duration (%d) must be >= max(clock-extension*2, clock-extension+challenge-period) = %d",
			cfg.MaxClockDuration, maxExt,
		)
	}

	return cfg, nil
}

// ContractsDir returns the path to the contracts-bedrock directory.
//
// WorkspaceFolder is the optimism repo root (this binary lives under
// rsk/op-deployer/), so contracts paths resolve relative to it without
// an extra "optimism" segment.
func (c *Config) ContractsDir() string {
	return filepath.Join(c.WorkspaceFolder, "packages", "contracts-bedrock")
}

// OPRSKContractsDir returns the path to the RSK Solidity package
// (carries the OPCMv2 split-deploy splitter contract + Forge driver
// script). See packages/contracts-rootstock/PLAN.md.
func (c *Config) OPRSKContractsDir() string {
	return filepath.Join(c.WorkspaceFolder, "packages", "contracts-rootstock")
}

// ContractsBuildHashPath returns the path to the file storing the last successful contracts build hash.
func (c *Config) ContractsBuildHashPath() string {
	return filepath.Join(c.ContractsDir(), ".deploy-rollup-build-hash")
}

// OpNodeDir returns the path to the op-node directory.
func (c *Config) OpNodeDir() string {
	return filepath.Join(c.WorkspaceFolder, "op-node")
}

// OptimismDir returns the optimism repo root (== WorkspaceFolder).
func (c *Config) OptimismDir() string {
	return c.WorkspaceFolder
}

// LogPath returns the path to a log file
func (c *Config) LogPath(filename string) string {
	return filepath.Join(c.WorkDir, "logs", filename)
}

// MasterLogPath returns the path to the master log file
func (c *Config) MasterLogPath() string {
	return c.LogPath("setup.log")
}

// DeploymentStatePath returns the path to deployment_state.json
func (c *Config) DeploymentStatePath() string {
	return filepath.Join(c.WorkDir, "deployment_state.json")
}

// BroadcastDir returns the path to forge broadcast directory
func (c *Config) BroadcastDir() string {
	return filepath.Join(c.ContractsDir(), "broadcast")
}

// PrivateKeyWith0x returns the private key with 0x prefix
func (c *Config) PrivateKeyWith0x() string {
	if strings.HasPrefix(c.PrivateKey, "0x") {
		return c.PrivateKey
	}
	return "0x" + c.PrivateKey
}

// FunderPrivateKeyWith0x returns the funder private key with 0x prefix
func (c *Config) FunderPrivateKeyWith0x() string {
	if c.FunderPrivateKey == "" {
		return ""
	}
	if strings.HasPrefix(c.FunderPrivateKey, "0x") {
		return c.FunderPrivateKey
	}
	return "0x" + c.FunderPrivateKey
}

// HasFunder returns true if a funder private key is configured
func (c *Config) HasFunder() bool {
	return c.FunderPrivateKey != ""
}

// L2ChainIDHexPadded returns L2 chain ID as hex padded to 64 characters (32 bytes)
func (c *Config) L2ChainIDHexPadded() string {
	return fmt.Sprintf("0x%064x", c.L2ChainID)
}

// InitMasterLog initializes the master log file
func (c *Config) InitMasterLog() error {
	f, err := os.Create(c.MasterLogPath())
	if err != nil {
		return fmt.Errorf("failed to create master log: %w", err)
	}
	c.masterLogFile = f

	// Write header
	c.LogToMaster("=== Deploy Rollup started at %s ===", time.Now().Format(time.RFC3339))
	c.LogToMaster("L1_CHAIN_ID: %d", c.L1ChainID)
	c.LogToMaster("L2_CHAIN_ID: %d", c.L2ChainID)
	c.LogToMaster("L1_BLOCK_TIME: %d", c.L1BlockTime)
	c.LogToMaster("L2_BLOCK_TIME: %d", c.L2BlockTime)
	c.LogToMaster("RPC_URL: %s", redact.URL(c.RPCURL))
	c.LogToMaster("EOA_ADDRESS: %s", c.EOAAddress)
	c.LogToMaster("")

	return nil
}

// LogToMaster writes to the master log file
func (c *Config) LogToMaster(format string, args ...interface{}) {
	if c.masterLogFile != nil {
		msg := fmt.Sprintf(format, args...)
		fmt.Fprintln(c.masterLogFile, msg)
	}
}

// Close closes any open resources
func (c *Config) Close() {
	if c.masterLogFile != nil {
		c.masterLogFile.Close()
	}
}

// String returns a human-readable representation of the config with secrets
// redacted. This prevents accidental leaking via fmt.Printf("%v", cfg) etc.
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{L1ChainID:%d L2ChainID:%d RPCURL:%s EOAAddress:%s PrivateKey:%s FunderPrivateKey:%s WorkDir:%s}",
		c.L1ChainID, c.L2ChainID, redact.URL(c.RPCURL), c.EOAAddress,
		redact.Key(c.PrivateKey), redact.Key(c.FunderPrivateKey), c.WorkDir,
	)
}

// getOrDefault returns the value if non-empty, otherwise the default
func getOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// findWorkspaceRoot finds the optimism repo root.
//
// This binary lives at optimism/rsk/op-deployer/cmd/deploy-rollup, so the
// "workspace" we care about is the optimism repo itself: it's where
// packages/contracts-bedrock and packages/contracts-rootstock are rooted.
func findWorkspaceRoot() (string, error) {
	startDirs := []string{}
	if cwd, err := os.Getwd(); err == nil {
		startDirs = append(startDirs, cwd)
	}
	if execPath, err := os.Executable(); err == nil {
		startDirs = append(startDirs, filepath.Dir(execPath))
	}
	for _, startDir := range startDirs {
		if root := walkUpToFindRoot(startDir); root != "" {
			return root, nil
		}
	}
	return os.Getwd()
}

// walkUpToFindRoot walks up the directory tree until it finds the
// optimism repo root: a directory whose go.mod declares
// `module github.com/ethereum-optimism/optimism` AND that contains
// packages/contracts-bedrock. Both checks are required because the
// optimism module path is also imported by sibling go.mod files
// inside the repo (testdata fixtures, etc).
func walkUpToFindRoot(startDir string) string {
	dir := startDir
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			content, err := os.ReadFile(goModPath)
			if err == nil && strings.Contains(string(content), "module github.com/ethereum-optimism/optimism\n") {
				if info, err := os.Stat(filepath.Join(dir, "packages", "contracts-bedrock")); err == nil && info.IsDir() {
					return dir
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
