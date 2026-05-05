# deploy-rollup

A Go CLI tool for deploying OP Stack rollup contracts directly using Foundry, bypassing `op-deployer`. Designed specifically for RSK compatibility but works on any EVM chain.

## Overview

This tool deploys the complete OP Stack contract suite in 6 steps:

1. **Deploy Superchain** - SuperchainConfig, ProtocolVersions, and ProxyAdmin
2. **Deploy Implementations** - All implementation contracts including OPCM (OPContractsManager)
3. **Deploy OP Chain** - All proxy contracts via OPCM
4. **Generate L2 Allocs** - L2 genesis allocations
5. **Generate Genesis** - genesis.json and rollup.json configuration
6. **Extract Configs** - Output configuration files for op-node and op-geth

## Features

- **Resumable deployments**: Automatically skips already-deployed contracts
- **Build cache**: Skips the contract build step when the contracts directory content hash is unchanged (stored in `contracts-bedrock/.deploy-rollup-build-hash`; delete that file to force a rebuild)
- **RSK compatible**: Uses `--legacy` and `--slow` flags for all forge commands
- **CREATE2 factory deployment**: Can deploy the deterministic deployer for private networks
- **Contract size estimation**: Report deployed bytecode sizes and flag EIP-170 violations
- **Detailed logging**: Per-step log files and master log
- **Progress tracking**: Visual progress bar and step status

## Prerequisites

- Go 1.21+
- [Foundry](https://book.getfoundry.sh/) (`forge` and `cast` must be in PATH)
- Access to the Optimism monorepo (included as submodule at `./optimism`)

## Installation

Single-command install (no clone required):

```bash
go install github.com/ethereum-optimism/optimism/rsk/op-deployer/cmd/deploy-rollup@latest
```

Or from a clone of the optimism fork:

```bash
go build -o bin/deploy-rollup \
  ./rsk/op-deployer/cmd/deploy-rollup    # from the optimism repo root
```

## Usage

### Basic Usage

```bash
./bin/deploy-rollup \
  --l1-chain-id 33 \
  --l2-chain-id 200133 \
  --rpc-url http://localhost:8545 \
  --private-key <YOUR_PRIVATE_KEY> \
  --eoa-address <YOUR_ADDRESS>
```

### Full Example (RSK Regtest)

```bash
./bin/deploy-rollup \
  --l1-chain-id 33 \
  --l2-chain-id 200133 \
  --rpc-url http://localhost:4444 \
  --private-key c85ef7d79691fe79573b1a7064c19c1a9819ebdbd1faaab1a8ec92344438aaf4 \
  --eoa-address 0xcd2a3d9f938e13cd947ec05abc7fe734df8dd826 \
  --deploy-create2 \
  --verbose
```

### Using Environment Variables

All flags can be set via environment variables:

```bash
export L1_CHAIN_ID=33
export L2_CHAIN_ID=200133
export RPC_URL=http://localhost:4444
export PRIVATE_KEY=<your_private_key>
export EOA_ADDRESS=<your_address>
export DEPLOY_CREATE2=true

./bin/deploy-rollup
```

## Command Line Flags

### Required Flags (for deployment)

These flags are required when deploying but not when using `--estimate-sizes`.

| Flag | Environment Variable | Description |
|------|---------------------|-------------|
| `--l1-chain-id` | `L1_CHAIN_ID` | L1 chain ID |
| `--l2-chain-id` | `L2_CHAIN_ID` | L2 chain ID |
| `--rpc-url` | `RPC_URL` | L1 RPC URL (must start with `http://` or `https://`) |
| `--private-key` | `PRIVATE_KEY` | Private key for deployment (with or without `0x` prefix) |
| `--eoa-address` | `EOA_ADDRESS` | EOA address for ownership roles |

### Optional Flags

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--deploy-create2` | `DEPLOY_CREATE2` | `false` | Deploy CREATE2 factory if not present |
| `--funder-private-key` | `FUNDER_PRIVATE_KEY` | - | Private key of pre-funded account to fund EOA |
| `--fund-amount` | `FUND_AMOUNT` | `10ether` | Amount to fund EOA with |
| `--min-balance` | `MIN_BALANCE` | `1ether` | Minimum balance before skipping funding |
| `--workdir` | `WORKDIR` | Auto-generated | Working directory for outputs |
| `--verbose`, `-v` | `VERBOSE` | `false` | Enable verbose output |
| `--force-redeploy` | `FORCE_REDEPLOY` | `false` | Force redeployment even if contracts exist |
| `--estimate-sizes` | `ESTIMATE_SIZES` | `false` | Build contracts and report deployed bytecode sizes (does not deploy) |
| `--gas-limit` | `GAS_LIMIT` | `6800000` | Chain block gas limit for deployment gas estimation (0 to disable) |

### Role Address Flags

Most role addresses default to the `--eoa-address` value. **Batcher and proposer** are special: when not explicitly set, their addresses are deterministically derived from `--private-key` using `keccak256(keyBytes || role)`. This gives each role its own L1 nonce space and avoids nonce contention.

| Flag | Environment Variable | Default | Description |
| --- | --- | --- | --- |
| `--superchain-proxy-admin-owner` | `SUPERCHAIN_PROXY_ADMIN_OWNER` | EOA | Superchain proxy admin owner |
| `--protocol-versions-owner` | `PROTOCOL_VERSIONS_OWNER` | EOA | Protocol versions owner |
| `--guardian` | `GUARDIAN` | EOA | Guardian address |
| `--upgrade-controller` | `UPGRADE_CONTROLLER` | EOA | Upgrade controller address |
| `--challenger` | `CHALLENGER` | EOA | Challenger address |
| `--proposer` | `PROPOSER` | Derived | Proposer address |
| `--batcher` | `BATCHER` | Derived | Batcher address |
| `--fund-roles` | `FUND_ROLES` | `false` | Fund derived batcher/proposer from deployer key |

When `--fund-roles` is set, the tool sends `--fund-amount` (default `10ether`) to each derived role account before deployment, using the deployer key. Use `rollup-node --master-private-key` at runtime so the node derives the same signing keys.

### Contract Size Estimation

Use `--estimate-sizes` to build contracts and report their deployed bytecode sizes without deploying. This does not require any deployment flags (RPC, private key, etc.).

```bash
# Check contract sizes with default Foundry profile
./bin/deploy-rollup --estimate-sizes

# Check with RSK Foundry profile
./bin/deploy-rollup --estimate-sizes --foundry-profile rsk

# Check against a custom gas limit (e.g. 10M)
./bin/deploy-rollup --estimate-sizes --gas-limit 10000000

# Disable gas limit check (only check EIP-170)
./bin/deploy-rollup --estimate-sizes --gas-limit 0
```

The output shows each contract's deployed bytecode size relative to the EIP-170 limit (24,576 bytes) and a lower-bound deployment gas estimate checked against the chain gas limit (default 6.8M, matching RSK). Contracts above 90% of either limit are flagged with `!`, and those exceeding a limit are marked `EXCEEDS`. The command exits with code 1 if any violation is found, making it suitable for CI checks.

The gas estimate accounts for transaction base cost, CREATE opcode, initcode calldata, and code deposit cost. Actual deployment gas will be higher due to constructor execution.

Contract sizes are also printed automatically after the build step during normal deployments.

## Output Files

The tool creates a work directory (default: `<l1-chain-id>_<date>_<l2-chain-id>/`) containing:

```
33_03_02_2026_200133/
├── deployment_state.json    # Deployed contract addresses and state
├── genesis.json             # L2 genesis configuration
├── rollup.json              # Rollup configuration for op-node
├── l1.json                  # L1 contract addresses
├── deploy-config.json       # Deploy configuration
├── l2-allocs.json           # L2 genesis allocations
└── logs/
    ├── setup.log            # Master log file
    ├── 01_deploy_superchain.log
    ├── 02_deploy_implementations.log
    ├── 03_deploy_opchain.log
    ├── 04_generate_l2_allocs.log
    ├── 05_generate_genesis.log
    └── 06_extract_configs.log
```

## Resumability

The tool tracks deployment state in `deployment_state.json`. If a deployment is interrupted:

1. Re-run the same command
2. Already-deployed contracts are detected by checking on-chain code
3. Completed steps are skipped automatically

To force a fresh deployment, use `--force-redeploy`. This also forces the contract build step to run (disables all step skips, including the build cache).

## RSK-Specific Notes

### Gas Limits

RSK has dynamic block gas limits. For deploying the full OP Stack, ensure your RSK node is configured with:

- **Block gas limit**: At least 10M (20M recommended)
- **Transaction gas limit**: At least 8M

The `OPContractsManagerStandardValidator` contract requires ~7.7M gas to deploy.

### CREATE2 Factory

Private RSK networks (regtest) don't have the CREATE2 factory pre-deployed. Use `--deploy-create2` to deploy it automatically.

### Legacy Transactions

All forge commands use `--legacy` flag for RSK compatibility (no EIP-1559 support).

## Troubleshooting

### "transaction's gas limit is higher than the block's gas limit"

Your RSK node's block or transaction gas limit is too low. Increase it in the node configuration.

### "CREATE2 Deployer not found"

Use the `--deploy-create2` flag to deploy the deterministic deployer factory.

### "failed to decode private key"

Ensure the private key is a valid 64-character hex string (with or without `0x` prefix).

### Deployment Interrupted

Simply re-run the command. The tool will resume from where it left off.

## Development

### Building

```bash
go build -o bin/deploy-rollup ./cmd/deploy-rollup
```

### Project Structure

```
cmd/deploy-rollup/
├── main.go      # CLI entry point and flag definitions
├── config.go    # Configuration handling
├── runner.go    # Main deployment orchestration
├── steps.go     # Individual deployment step implementations
├── forge.go     # Foundry (forge/cast) execution wrapper
├── state.go     # Deployment state persistence
├── sizes.go     # Contract size estimation from forge artifacts
├── output.go    # Console output formatting
├── tools.go     # Tool availability checks
└── create2.go   # CREATE2 factory deployment
```

## License

See the main repository LICENSE file.
