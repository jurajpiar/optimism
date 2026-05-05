// Command rsk-op-node is a thin wrapper around upstream op-node that injects
// the RSK-specific runtime overrides (RPCKindBasic, trusted RPC,
// beacon-check-ignore, conf depths, sync skip-start-check, disabled L1 epoch
// poll) on top of the operator-supplied CLI flags.
//
// It also wraps cfg.L1 in oprsk/nodecfg.RSKL1Endpoint, which installs the
// patch-0001 hooks (BlockVerifier / HeaderVerifier / ReceiptsValidator /
// TxHashesFromBlock) on the L1ClientConfig before NewL1Client runs.
//
// Run with --sequencer.enabled=true on the sequencer host or without it on a
// verifier/RPC host.
package main

import (
	"context"
	"fmt"
	"os"

	oprsknodecfg "github.com/ethereum-optimism/optimism/rsk/nodecfg"

	"github.com/urfave/cli/v2"

	"github.com/ethereum/go-ethereum/log"

	opnode "github.com/ethereum-optimism/optimism/op-node"
	"github.com/ethereum-optimism/optimism/op-node/chaincfg"
	"github.com/ethereum-optimism/optimism/op-node/cmd/genesis"
	"github.com/ethereum-optimism/optimism/op-node/cmd/interop"
	"github.com/ethereum-optimism/optimism/op-node/cmd/networks"
	"github.com/ethereum-optimism/optimism/op-node/cmd/p2p"
	"github.com/ethereum-optimism/optimism/op-node/flags"
	"github.com/ethereum-optimism/optimism/op-node/metrics"
	"github.com/ethereum-optimism/optimism/op-node/node"
	"github.com/ethereum-optimism/optimism/op-node/version"
	opservice "github.com/ethereum-optimism/optimism/op-service"
	"github.com/ethereum-optimism/optimism/op-service/cliapp"
	"github.com/ethereum-optimism/optimism/op-service/ctxinterrupt"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	"github.com/ethereum-optimism/optimism/op-service/metrics/doc"
)

var (
	GitCommit = ""
	GitDate   = ""
)

var versionWithMeta = opservice.FormatVersion(version.Version, GitCommit, GitDate, version.Meta)

func main() {
	oplog.SetupDefaults()

	app := cli.NewApp()
	app.Version = versionWithMeta
	app.Flags = cliapp.ProtectFlags(flags.Flags)
	app.Name = "rsk-op-node"
	app.Usage = "RSK Optimism Rollup Node"
	app.Description = "op-node with RSK-specific runtime overrides applied"
	app.Action = cliapp.LifecycleCmd(rskRollupNodeMain)
	app.Commands = []*cli.Command{
		{Name: "p2p", Subcommands: p2p.Subcommands},
		{Name: "genesis", Subcommands: genesis.Subcommands},
		{Name: "doc", Subcommands: doc.NewSubcommands(metrics.NewMetrics("default", nil))},
		{Name: "networks", Subcommands: networks.Subcommands},
		interop.InteropCmd,
	}

	ctx := ctxinterrupt.WithSignalWaiterMain(context.Background())
	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Crit("Application failed", "message", err)
	}
}

// rskRollupNodeMain mirrors upstream RollupNodeMain but injects
// oprsknodecfg.ApplyOpNodeRSK after NewConfig.
func rskRollupNodeMain(ctx *cli.Context, closeApp context.CancelCauseFunc) (cliapp.Lifecycle, error) {
	logCfg := oplog.ReadCLIConfig(ctx)
	l := oplog.NewLogger(oplog.AppOut(ctx), logCfg)
	oplog.SetGlobalLogHandler(l.Handler())
	opservice.ValidateEnvVars(flags.EnvVarPrefix, flags.Flags, l)
	opservice.WarnOnDeprecatedFlags(ctx, flags.DeprecatedFlags, l)
	m := metrics.NewMetrics("default", nil)

	cfg, err := opnode.NewConfig(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("unable to create the rollup node config: %w", err)
	}
	cfg.Cancel = closeApp

	// L1 chain id from rollup config (RSK: 30/31/33). Used by the chain-aware
	// adapters in oprsk/l1source so the hooks fall through to standard
	// Ethereum behavior on a non-RSK chain id.
	var l1ChainID uint64
	if cfg.Rollup.L1ChainID != nil {
		l1ChainID = cfg.Rollup.L1ChainID.Uint64()
	}
	oprsknodecfg.ApplyOpNodeRSK(cfg, l1ChainID)

	if logCfg.Format == "terminal" {
		l.Info("rollup config:\n" + cfg.Rollup.Description(chaincfg.L2ChainIDToNetworkDisplayName))
	} else {
		cfg.Rollup.LogDescription(l, chaincfg.L2ChainIDToNetworkDisplayName)
	}

	n, err := node.New(ctx.Context, cfg, l, versionWithMeta, m, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create the rollup node: %w", err)
	}
	return n, nil
}
