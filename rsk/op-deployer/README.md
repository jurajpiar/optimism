# op-deployer (RSK)

RSK-specific deploy orchestrator for OP Stack chains. Sibling to upstream's
`optimism/op-deployer/` — they coexist; this one is purposefully *not*
upstreamable, it only targets RSK and lives inside the RSK fork forever.

## Why a separate deployer?

Upstream `op-deployer` collapses chain deployment into a single
`OPCMv2.deploy(config)` call. That tx requires ~10 M gas, which can never
fit under RSKj's RSKIP144 6.8 M per-transaction sublist cap. This tool
instead drives the multi-stage `RSKOPCMSplitter` (see
`packages/contracts-rootstock/`) plus the rest of the deploy pipeline
(superchain, implementations, L2 genesis, runtime configs).

## Layout

```
rsk/op-deployer/
├── README.md                          # this file
├── cmd/deploy-rollup/                 # the binary (5K LoC)
│   ├── main.go
│   ├── runner.go                      # orchestration / step pipeline
│   ├── steps.go                       # per-step deploy logic
│   ├── forge.go                       # forge subprocess driver
│   ├── config.go                      # CLI flags + workdir resolution
│   ├── state.go                       # resumable deployment state
│   └── ...                            # genesis, sizes, create2, etc.
├── keyderive/                         # deterministic batcher/proposer key derivation
│   └── derive.go                      # keccak256(masterKey || role)
└── redact/                            # secret-string redaction for logs
    └── redact.go
```

`keyderive` and `redact` are intentionally **public** packages (not under
`internal/`) so that the rsk-aware `op-node`, `op-batcher`, and
`op-proposer` binaries — and any external rollup-monitoring / dashboard
tooling — can derive matching role keys and consistent log redaction
without re-implementing them.

## Build

```bash
go build -o bin/deploy-rollup \
  github.com/ethereum-optimism/optimism/rsk/op-deployer/cmd/deploy-rollup
```

Or `go install` (no clone required, given network access to the fork):

```bash
go install github.com/ethereum-optimism/optimism/rsk/op-deployer/cmd/deploy-rollup@latest
```

## Usage

See:

- `cmd/deploy-rollup/README.md` for the full CLI flag reference.
- `../../packages/contracts-rootstock/DEPLOY.md` for the underlying Forge-only
  runbook that this tool automates end-to-end.

A typical regtest invocation looks like:

```bash
deploy-rollup \
  --l1-chain-id 33 --l2-chain-id 200133 \
  --rpc-url http://127.0.0.1:4444 \
  --private-key <deployer-key> \
  --eoa-address <deployer-addr> \
  --funder-private-key <prefunded-cow-key> \
  --deploy-create2 --skip-fault-proofs --use-split-deploy --fund-roles \
  --gas-limit 10000000 --workdir ./my-deploy
```

## Why not patch upstream `op-deployer` instead?

Considered and explicitly rejected. See
`../../patches/optimism/SERIES.md` for the design discussion of why the
RSK fork is permanent and not upstreamed. Forking `op-deployer` would
add ~250 LoC of changes to a moving upstream target; carrying our own
~5K-LoC tool that we control is a better trade.
