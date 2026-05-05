package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "deploy-rollup",
		Usage: "Deploy OP Stack rollup contracts using Foundry directly (bypasses op-deployer)",
		Description: `This tool deploys OP Stack contracts directly using forge scripts,
avoiding the nonce simulation issues that can occur with op-deployer on RSK.

The deployment process:
  1. Deploy Superchain contracts (SuperchainConfig, ProtocolVersions)
  2. Deploy Implementation contracts (OPCM, all implementations)
  3. Deploy OP Chain contracts (proxies via OPCM)
  4. Generate L2 genesis allocations
  5. Generate genesis.json and rollup.json
  6. Extract configuration files

Features:
  - Resumable: Skips already-deployed contracts
  - RSK Compatible: Uses --legacy and --slow flags for all forge commands
  - Logging: Per-step logs and master log file`,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "l1-chain-id",
				Usage:   "L1 chain ID",
				EnvVars: []string{"L1_CHAIN_ID"},
			},
			&cli.IntFlag{
				Name:    "l2-chain-id",
				Usage:   "L2 chain ID",
				EnvVars: []string{"L2_CHAIN_ID"},
			},
			&cli.StringFlag{
				Name:    "rpc-url",
				Usage:   "L1 RPC URL (must start with http:// or https://)",
				EnvVars: []string{"RPC_URL"},
			},
			&cli.StringFlag{
				Name:    "private-key",
				Usage:   "Private key for deployment (with or without 0x prefix)",
				EnvVars: []string{"PRIVATE_KEY"},
			},
			&cli.StringFlag{
				Name:    "eoa-address",
				Usage:   "EOA address for ownership roles",
				EnvVars: []string{"EOA_ADDRESS"},
			},
			&cli.BoolFlag{
				Name:    "deploy-create2",
				Usage:   "Deploy CREATE2 factory if not present (for regtest/private networks)",
				Value:   false,
				EnvVars: []string{"DEPLOY_CREATE2"},
			},
			&cli.StringFlag{
				Name:    "funder-private-key",
				Usage:   "Private key of a pre-funded account to fund EOA (e.g., RSK cow account)",
				EnvVars: []string{"FUNDER_PRIVATE_KEY"},
			},
			&cli.StringFlag{
				Name:    "fund-amount",
				Usage:   "Amount to fund EOA with (e.g., '10ether', '1000000000000000000')",
				Value:   "10ether",
				EnvVars: []string{"FUND_AMOUNT"},
			},
			&cli.StringFlag{
				Name:    "min-balance",
				Usage:   "Minimum balance EOA should have before skipping funding (e.g., '1ether')",
				Value:   "1ether",
				EnvVars: []string{"MIN_BALANCE"},
			},
			&cli.StringFlag{
				Name:    "superchain-proxy-admin-owner",
				Usage:   "Superchain proxy admin owner address (defaults to EOA_ADDRESS)",
				EnvVars: []string{"SUPERCHAIN_PROXY_ADMIN_OWNER"},
			},
			&cli.StringFlag{
				Name:    "protocol-versions-owner",
				Usage:   "Protocol versions owner address (defaults to EOA_ADDRESS)",
				EnvVars: []string{"PROTOCOL_VERSIONS_OWNER"},
			},
			&cli.StringFlag{
				Name:    "guardian",
				Usage:   "Guardian address (defaults to EOA_ADDRESS)",
				EnvVars: []string{"GUARDIAN"},
			},
			&cli.StringFlag{
				Name:    "upgrade-controller",
				Usage:   "Upgrade controller address (defaults to EOA_ADDRESS)",
				EnvVars: []string{"UPGRADE_CONTROLLER"},
			},
			&cli.StringFlag{
				Name:    "challenger",
				Usage:   "Challenger address (defaults to EOA_ADDRESS)",
				EnvVars: []string{"CHALLENGER"},
			},
			&cli.StringFlag{
				Name:    "proposer",
				Usage:   "Proposer address (derived from --private-key if not set)",
				EnvVars: []string{"PROPOSER"},
			},
			&cli.StringFlag{
				Name:    "batcher",
				Usage:   "Batcher address (derived from --private-key if not set)",
				EnvVars: []string{"BATCHER"},
			},
			&cli.BoolFlag{
				Name:    "fund-roles",
				Usage:   "Fund derived batcher/proposer accounts from the deployer key before deployment",
				Value:   false,
				EnvVars: []string{"FUND_ROLES"},
			},
			&cli.StringFlag{
				Name:    "workdir",
				Usage:   "Working directory (defaults to auto-generated based on chain IDs and date)",
				EnvVars: []string{"WORKDIR"},
			},
			&cli.IntFlag{
				Name:    "l1-block-time",
				Usage:   "L1 block time in seconds (default: 12 for Ethereum, 30 for RSK)",
				Value:   12,
				EnvVars: []string{"L1_BLOCK_TIME"},
			},
			&cli.IntFlag{
				Name:    "l2-block-time",
				Usage:   "L2 block time in seconds",
				Value:   2,
				EnvVars: []string{"L2_BLOCK_TIME"},
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Enable verbose output",
				Value:   false,
				EnvVars: []string{"VERBOSE"},
			},
			&cli.BoolFlag{
				Name:    "force-redeploy",
				Usage:   "Force redeployment even if contracts already exist on-chain",
				Value:   false,
				EnvVars: []string{"FORCE_REDEPLOY"},
			},
			&cli.StringFlag{
				Name:    "foundry-profile",
				Usage:   "Foundry profile for contract compilation (auto-detects 'rsk' for L1 chain 31)",
				EnvVars: []string{"FOUNDRY_PROFILE"},
			},
			&cli.BoolFlag{
				Name:    "use-split-deploy",
				Usage:   "Use split deployment for RSK gas limit compatibility (auto-enabled for L1 chain 31)",
				Value:   false,
				EnvVars: []string{"USE_SPLIT_DEPLOY"},
			},
			&cli.BoolFlag{
				Name:    "skip-fault-proofs",
				Usage:   "Skip deploying large fault proof contracts for RSK compatibility",
				Value:   false,
				EnvVars: []string{"SKIP_FAULT_PROOFS"},
			},
			&cli.BoolFlag{
				Name:    "deploy-dispute-game",
				Usage:   "Deploy PermissionedDisputeGameStub to an existing chain (requires existing workdir with l1.json)",
				Value:   false,
				EnvVars: []string{"DEPLOY_DISPUTE_GAME"},
			},
			&cli.BoolFlag{
				Name:    "estimate-sizes",
				Usage:   "Build contracts and report deployed bytecode sizes (does not deploy)",
				Value:   false,
				EnvVars: []string{"ESTIMATE_SIZES"},
			},
			&cli.IntFlag{
				Name:    "gas-limit",
				Usage:   "Chain block gas limit for deployment gas estimation (0 to disable gas check)",
				Value:   6_800_000,
				EnvVars: []string{"GAS_LIMIT"},
			},
			&cli.IntFlag{
				Name:    "proof-maturity-delay",
				Usage:   "Proof maturity delay in seconds before withdrawal finalization (default: 0 for RSK testnet, 604800 otherwise)",
				EnvVars: []string{"PROOF_MATURITY_DELAY"},
			},
			&cli.IntFlag{
				Name:    "dispute-game-finality-delay",
				Usage:   "Dispute game finality delay in seconds (default: 0 for RSK testnet, 302400 otherwise)",
				EnvVars: []string{"DISPUTE_GAME_FINALITY_DELAY"},
			},
			&cli.IntFlag{
				Name:    "withdrawal-delay",
				Usage:   "Withdrawal delay in seconds for DelayedWETH (default: 0 for RSK testnet, 604800 otherwise)",
				EnvVars: []string{"WITHDRAWAL_DELAY"},
			},
			&cli.IntFlag{
				Name:    "max-clock-duration",
				Usage:   "Dispute game max clock duration in seconds (default: 300 for RSK testnet, 302400 otherwise)",
				EnvVars: []string{"MAX_CLOCK_DURATION"},
			},
			&cli.IntFlag{
				Name:    "clock-extension",
				Usage:   "Dispute game clock extension in seconds (default: 60 for RSK testnet, 10800 otherwise)",
				EnvVars: []string{"CLOCK_EXTENSION"},
			},
			&cli.IntFlag{
				Name:    "challenge-period",
				Usage:   "PreimageOracle challenge period in seconds (default: 120 for RSK testnet, 86400 otherwise)",
				EnvVars: []string{"CHALLENGE_PERIOD"},
			},
		},
		Action: runDeploy,
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runDeploy(cliCtx *cli.Context) error {
	if cliCtx.Bool("estimate-sizes") {
		return runEstimateSizes(cliCtx)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		printError("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Build configuration from CLI flags
	cfg, err := NewConfigFromCLI(cliCtx)
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Create runner
	runner := NewRunner(cfg)

	// Run the deployment
	return runner.Run(ctx)
}
