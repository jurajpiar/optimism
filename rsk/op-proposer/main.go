// Command rsk-op-proposer is a thin wrapper around the upstream op-proposer
// service that injects the RSK-specific runtime overrides (legacy tx, RSK
// gas estimator, RSK error matchers, AllowNonFinalized=true) on top of the
// operator-supplied CLI flags.
package main

import (
	"context"
	"fmt"
	"os"

	oprsknodecfg "github.com/ethereum-optimism/optimism/rsk/nodecfg"

	"github.com/urfave/cli/v2"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-proposer/flags"
	"github.com/ethereum-optimism/optimism/op-proposer/metrics"
	"github.com/ethereum-optimism/optimism/op-proposer/proposer"
	opservice "github.com/ethereum-optimism/optimism/op-service"
	"github.com/ethereum-optimism/optimism/op-service/cliapp"
	"github.com/ethereum-optimism/optimism/op-service/ctxinterrupt"
	oplog "github.com/ethereum-optimism/optimism/op-service/log"
	"github.com/ethereum-optimism/optimism/op-service/metrics/doc"
)

var (
	Version   = "v0.0.0"
	GitCommit = ""
	GitDate   = ""
)

func main() {
	oplog.SetupDefaults()

	app := cli.NewApp()
	app.Flags = cliapp.ProtectFlags(flags.Flags)
	app.Version = opservice.FormatVersion(Version, GitCommit, GitDate, "")
	app.Name = "rsk-op-proposer"
	app.Usage = "RSK L2 Output Submitter"
	app.Description = "op-proposer with RSK-specific runtime overrides applied"
	app.Action = cliapp.LifecycleCmd(rskProposerMain(Version))
	app.Commands = []*cli.Command{
		{
			Name:        "doc",
			Subcommands: doc.NewSubcommands(metrics.NewMetrics("default")),
		},
	}

	ctx := ctxinterrupt.WithSignalWaiterMain(context.Background())
	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Crit("Application failed", "message", err)
	}
}

// rskProposerMain mirrors proposer.Main but applies
// oprsknodecfg.ApplyProposerRSK after NewConfig and before Check.
func rskProposerMain(version string) cliapp.LifecycleAction {
	return func(cliCtx *cli.Context, _ context.CancelCauseFunc) (cliapp.Lifecycle, error) {
		if err := flags.CheckRequired(cliCtx); err != nil {
			return nil, err
		}
		cfg := proposer.NewConfig(cliCtx)
		oprsknodecfg.ApplyProposerRSK(cfg)
		if err := cfg.Check(); err != nil {
			return nil, fmt.Errorf("invalid CLI flags: %w", err)
		}

		l := oplog.NewLogger(oplog.AppOut(cliCtx), cfg.LogConfig)
		oplog.SetGlobalLogHandler(l.Handler())
		opservice.ValidateEnvVars(flags.EnvVarPrefix, flags.Flags, l)

		l.Info("Initializing RSK L2Output Submitter")
		return proposer.ProposerServiceFromCLIConfig(cliCtx.Context, version, cfg, l)
	}
}
