// Command rsk-op-batcher is a thin wrapper around the upstream op-batcher
// service that injects the RSK-specific runtime overrides (legacy tx, RSK
// gas estimator, RSK error matchers, no throttling, calldata-only DA) on
// top of the operator-supplied CLI flags.
//
// Deployment-wise this is the production batcher binary; the in-process
// batcher inside cmd/rollup-node is dev-only.
package main

import (
	"context"
	"fmt"
	"os"

	oprsknodecfg "github.com/ethereum-optimism/optimism/rsk/nodecfg"

	"github.com/urfave/cli/v2"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-batcher/batcher"
	"github.com/ethereum-optimism/optimism/op-batcher/flags"
	"github.com/ethereum-optimism/optimism/op-batcher/metrics"
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
	app.Name = "rsk-op-batcher"
	app.Usage = "RSK Batch Submitter Service"
	app.Description = "op-batcher with RSK-specific runtime overrides applied"
	app.Action = cliapp.LifecycleCmd(rskBatcherMain(Version))
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

// rskBatcherMain mirrors batcher.Main but applies oprsknodecfg.ApplyBatcherRSK
// after NewConfig and before Check, so the RSK overrides participate in
// validation just like upstream defaults do.
func rskBatcherMain(version string) cliapp.LifecycleAction {
	return func(cliCtx *cli.Context, closeApp context.CancelCauseFunc) (cliapp.Lifecycle, error) {
		if err := flags.CheckRequired(cliCtx); err != nil {
			return nil, err
		}
		cfg := batcher.NewConfig(cliCtx)
		oprsknodecfg.ApplyBatcherRSK(cfg)
		if err := cfg.Check(); err != nil {
			return nil, fmt.Errorf("invalid CLI flags: %w", err)
		}

		l := oplog.NewLogger(oplog.AppOut(cliCtx), cfg.LogConfig)
		oplog.SetGlobalLogHandler(l.Handler())
		opservice.ValidateEnvVars(flags.EnvVarPrefix, flags.Flags, l)

		l.Info("Initializing RSK Batch Submitter")
		return batcher.BatcherServiceFromCLIConfig(cliCtx.Context, closeApp, version, cfg, l)
	}
}
