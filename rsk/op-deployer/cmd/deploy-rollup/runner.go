package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum-optimism/optimism/rsk/op-deployer/redact"
)

// Runner orchestrates the deployment process
type Runner struct {
	cfg       *Config
	state     *DeploymentState
	checker   *ContractChecker
	forge     *ForgeExecutor
	startTime time.Time
}

// NewRunner creates a new deployment runner
func NewRunner(cfg *Config) *Runner {
	return &Runner{
		cfg:   cfg,
		forge: NewForgeExecutor(cfg),
	}
}

// DeploymentStep represents a single deployment step
type DeploymentStep struct {
	Name    string
	LogFile string
	Run     func(ctx context.Context, logger *StepLogger) error
	Skip    func(ctx context.Context) (bool, string, error) // Returns (shouldSkip, reason, error)
}

// Run executes the full deployment process
func (r *Runner) Run(ctx context.Context) error {
	r.startTime = time.Now()

	// Print header
	printStatus("═══════════════════════════════════════════════════════════")
	printStatus("OP Stack Rollup Deployment (Foundry Direct Mode)")
	printStatus("═══════════════════════════════════════════════════════════")
	printStatus("L1 Chain ID: %d", r.cfg.L1ChainID)
	printStatus("L2 Chain ID: %d", r.cfg.L2ChainID)
	printStatus("RPC URL: %s", redact.URL(r.cfg.RPCURL))
	printStatus("EOA Address: %s", r.cfg.EOAAddress)
	printStatus("Deploy CREATE2: %v", r.cfg.DeployCREATE2)
	printStatus("Force Redeploy: %v", r.cfg.ForceRedeploy)
	if r.cfg.HasFunder() {
		printStatus("Funder configured: yes (will fund EOA with %s if below %s)", r.cfg.FundAmount, r.cfg.MinBalance)
	}
	if r.cfg.Batcher != r.cfg.EOAAddress {
		printStatus("Batcher address: %s (derived)", r.cfg.Batcher)
	} else {
		printStatus("Batcher address: %s (same as EOA)", r.cfg.Batcher)
	}
	if r.cfg.Proposer != r.cfg.EOAAddress {
		printStatus("Proposer address: %s (derived)", r.cfg.Proposer)
	} else {
		printStatus("Proposer address: %s (same as EOA)", r.cfg.Proposer)
	}
	if r.cfg.FundRoles {
		printStatus("Fund roles: yes (will fund batcher/proposer with %s if below %s)", r.cfg.FundAmount, r.cfg.MinBalance)
	}
	printStatus("Work Directory: %s", r.cfg.WorkDir)
	printStatus("═══════════════════════════════════════════════════════════")

	// Initialize work directory
	if err := r.initWorkDir(); err != nil {
		return fmt.Errorf("failed to initialize work directory: %w", err)
	}

	// Initialize master log
	if err := r.cfg.InitMasterLog(); err != nil {
		return fmt.Errorf("failed to initialize master log: %w", err)
	}
	defer r.cfg.Close()

	// Load deployment state
	var err error
	r.state, err = LoadDeploymentState(r.cfg.DeploymentStatePath())
	if err != nil {
		return fmt.Errorf("failed to load deployment state: %w", err)
	}
	r.state.L2ChainID = fmt.Sprintf("%d", r.cfg.L2ChainID)

	// Initialize contract checker
	r.checker, err = NewContractChecker(r.cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("failed to create contract checker: %w", err)
	}
	defer r.checker.Close()

	// Check required tools
	printStep("Checking required tools...")
	if err := checkRequiredTools(r.cfg.Verbose); err != nil {
		return fmt.Errorf("tool check failed: %w", err)
	}

	// Fund EOA if funder is configured
	if r.cfg.HasFunder() {
		printStep("Checking/funding EOA account...")
		if err := fundEOAIfNeeded(ctx, r.cfg); err != nil {
			return fmt.Errorf("failed to fund EOA: %w", err)
		}
	}

	// Fund derived role accounts if --fund-roles is set
	if r.cfg.FundRoles {
		printStep("Checking/funding role accounts (batcher, proposer)...")
		if err := fundRoleAccountsIfNeeded(ctx, r.cfg); err != nil {
			return fmt.Errorf("failed to fund role accounts: %w", err)
		}
	}

	// Deploy CREATE2 factory if needed
	if r.cfg.DeployCREATE2 {
		printStep("Checking/deploying CREATE2 factory...")
		if err := deployCREATE2IfNeeded(ctx, r.cfg); err != nil {
			return fmt.Errorf("failed to deploy CREATE2 factory: %w", err)
		}
	}

	// Build deployment steps
	steps := r.buildSteps()

	// Execute steps
	for i, step := range steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		printProgress(i+1, len(steps))
		printStep("Running: %s", step.Name)

		// Check if step should be skipped
		if step.Skip != nil && !r.cfg.ForceRedeploy {
			shouldSkip, reason, err := step.Skip(ctx)
			if err != nil {
				printError("Failed to check skip condition: %v", err)
			} else if shouldSkip {
				printSkipped("Skipping %s: %s", step.Name, reason)
				continue
			}
		}

		// Create step logger
		logPath := r.cfg.LogPath(step.LogFile)
		logFile, err := os.Create(logPath)
		if err != nil {
			return fmt.Errorf("failed to create log file %s: %w", logPath, err)
		}

		logger := NewStepLogger(logFile, r.cfg.masterLogFile, step.Name)

		printStatus("Log file: %s", logPath)
		r.cfg.LogToMaster("=== Step: %s ===", step.Name)

		startTime := time.Now()
		err = step.Run(ctx, logger)
		elapsed := time.Since(startTime)

		logger.Close()

		if err != nil {
			printError("✗ %s failed (%.1fs)", step.Name, elapsed.Seconds())
			printError("Check log file: %s", logPath)

			// Show last few lines of log
			r.showLogTail(logPath, 10)

			return fmt.Errorf("step '%s' failed: %w", step.Name, err)
		}

		printStatus("✓ %s completed successfully (%.1fs)", step.Name, elapsed.Seconds())

		// Save state after each successful step
		if err := r.state.Save(r.cfg.DeploymentStatePath()); err != nil {
			printError("Warning: failed to save deployment state: %v", err)
		}
	}

	// Print summary
	r.printSummary(len(steps))

	return nil
}

// buildSteps creates the list of deployment steps
func (r *Runner) buildSteps() []DeploymentStep {
	// Deploy dispute game mode: only build + deploy the dispute game stub
	if r.cfg.DeployDisputeGame {
		return []DeploymentStep{
			{
				Name:    "Build Contracts",
				LogFile: "00_build_contracts.log",
				Run:     r.buildContracts,
				Skip:    r.skipBuildContracts,
			},
			{
				Name:    "Deploy Dispute Game Stub",
				LogFile: "07_deploy_dispute_game.log",
				Run:     r.deployDisputeGameStub,
				Skip:    r.skipDisputeGame,
			},
			{
				Name:    "Extract Configuration Files",
				LogFile: "06_extract_configs.log",
				Run:     r.extractConfigs,
				Skip:    nil, // Always run to update l1.json
			},
		}
	}

	steps := []DeploymentStep{
		{
			Name:    "Build Contracts",
			LogFile: "00_build_contracts.log",
			Run:     r.buildContracts,
			Skip:    r.skipBuildContracts,
		},
		{
			Name:    "Deploy Superchain",
			LogFile: "01_deploy_superchain.log",
			Run:     r.deploySuperchain,
			Skip:    r.skipSuperchain,
		},
		{
			Name:    "Deploy Implementations",
			LogFile: "02_deploy_implementations.log",
			Run:     r.deployImplementations,
			Skip:    r.skipImplementations,
		},
	}

	// Use split deployment for RSK gas limit compatibility
	if r.cfg.UseSplitDeploy {
		steps = append(steps, DeploymentStep{
			Name:    "Deploy OP Chain (Split)",
			LogFile: "03_deploy_opchain_split.log",
			Run:     r.deployOPChainSplit,
			Skip:    r.skipOPChain,
		})
	} else {
		steps = append(steps, DeploymentStep{
			Name:    "Deploy OP Chain",
			LogFile: "03_deploy_opchain.log",
			Run:     r.deployOPChain,
			Skip:    r.skipOPChain,
		})
	}

	// When fault proofs are skipped (e.g. RSK), auto-deploy the lightweight
	// PermissionedDisputeGameStub so withdrawals work out of the box.
	if r.cfg.SkipFaultProofs {
		steps = append(steps, DeploymentStep{
			Name:    "Deploy Dispute Game Stub",
			LogFile: "07_deploy_dispute_game.log",
			Run:     r.deployDisputeGameStub,
			Skip:    r.skipDisputeGame,
		})
	}

	steps = append(steps,
		DeploymentStep{
			Name:    "Generate L2 Genesis Allocs",
			LogFile: "04_generate_l2_allocs.log",
			Run:     r.generateL2Allocs,
			Skip:    r.skipL2Allocs,
		},
		DeploymentStep{
			Name:    "Generate Genesis and Rollup Config",
			LogFile: "05_generate_genesis.log",
			Run:     r.generateGenesis,
			Skip:    r.skipGenesis,
		},
		DeploymentStep{
			Name:    "Extract Configuration Files",
			LogFile: "06_extract_configs.log",
			Run:     r.extractConfigs,
			Skip:    nil, // Always run to ensure configs are up to date
		},
	)

	return steps
}

func (r *Runner) initWorkDir() error {
	// Create directories
	dirs := []string{
		r.cfg.WorkDir,
		filepath.Join(r.cfg.WorkDir, "logs"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	printStatus("Created work directory: %s", r.cfg.WorkDir)
	return nil
}

func (r *Runner) showLogTail(logPath string, lines int) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return
	}

	content := string(data)
	logLines := splitLines(content)

	startIdx := len(logLines) - lines
	if startIdx < 0 {
		startIdx = 0
	}

	printError("Last %d lines of log:", lines)
	for _, line := range logLines[startIdx:] {
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func (r *Runner) printSummary(totalSteps int) {
	elapsed := time.Since(r.startTime)
	minutes := int(elapsed.Minutes())
	seconds := int(elapsed.Seconds()) % 60

	fmt.Println()
	printStatus("═══════════════════════════════════════════════════════════")
	printStatus("All %d steps completed successfully!", totalSteps)
	printStatus("Total time: %dm %ds", minutes, seconds)
	printStatus("Work directory: %s", r.cfg.WorkDir)
	printStatus("Master log: %s", r.cfg.MasterLogPath())
	printStatus("═══════════════════════════════════════════════════════════")
	printStatus("")
	printStatus("Output files:")
	printStatus("  - genesis.json")
	printStatus("  - rollup.json")
	printStatus("  - l1.json")
	printStatus("  - deploy-config.json")
	printStatus("  - deployment_state.json")
	printStatus("═══════════════════════════════════════════════════════════")

	// Write completion to master log
	r.cfg.LogToMaster("")
	r.cfg.LogToMaster("=== Deployment completed successfully at %s ===", time.Now().Format(time.RFC3339))
	r.cfg.LogToMaster("Total time: %dm %ds", minutes, seconds)
}

// Skip functions
func (r *Runner) skipBuildContracts(ctx context.Context) (bool, string, error) {
	current, err := hashContractsDir(r.cfg.ContractsDir())
	if err != nil {
		return false, "", err
	}
	stored, err := readStoredBuildHash(r.cfg.ContractsBuildHashPath())
	if err != nil {
		return false, "", err
	}
	if stored != "" && current == stored {
		return true, "contracts unchanged (hash match)", nil
	}
	return false, "", nil
}

func (r *Runner) skipSuperchain(ctx context.Context) (bool, string, error) {
	deployed, err := r.state.IsSuperchainDeployed(ctx, r.checker)
	if err != nil {
		return false, "", err
	}
	if deployed {
		return true, fmt.Sprintf("SuperchainConfigProxy at %s has code", r.state.Superchain.SuperchainConfigProxy), nil
	}
	return false, "", nil
}

func (r *Runner) skipImplementations(ctx context.Context) (bool, string, error) {
	deployed, err := r.state.IsImplementationsDeployed(ctx, r.checker)
	if err != nil {
		return false, "", err
	}
	if deployed {
		return true, fmt.Sprintf("OPCM at %s has code", r.state.Implementations.OPCM), nil
	}
	return false, "", nil
}

func (r *Runner) skipOPChain(ctx context.Context) (bool, string, error) {
	deployed, err := r.state.IsOpChainDeployed(ctx, r.checker)
	if err != nil {
		return false, "", err
	}
	if deployed {
		return true, fmt.Sprintf("SystemConfigProxy at %s has code", r.state.OpChain.SystemConfigProxy), nil
	}
	return false, "", nil
}

func (r *Runner) skipL2Allocs(ctx context.Context) (bool, string, error) {
	if r.state.L2AllocsGenerated {
		allocsPath := filepath.Join(r.cfg.WorkDir, "l2-allocs.json")
		if _, err := os.Stat(allocsPath); err == nil {
			return true, "l2-allocs.json already exists", nil
		}
	}
	return false, "", nil
}

func (r *Runner) skipGenesis(ctx context.Context) (bool, string, error) {
	if r.state.GenesisGenerated {
		genesisPath := filepath.Join(r.cfg.WorkDir, "genesis.json")
		rollupPath := filepath.Join(r.cfg.WorkDir, "rollup.json")
		_, err1 := os.Stat(genesisPath)
		_, err2 := os.Stat(rollupPath)
		if err1 == nil && err2 == nil {
			return true, "genesis.json and rollup.json already exist", nil
		}
	}
	return false, "", nil
}
