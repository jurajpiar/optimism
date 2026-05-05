# oprsk-contracts — RSK-side Solidity carry-on

Out-of-tree Solidity carry-on for RSK, parallels `oprsk/` (the Go side). Lives outside `optimism/packages/contracts-bedrock/` so the upstream contracts package stays close to its release-tag content and our changes remain a thin, traceable add-on.

This package currently exists for one purpose: **a forge script that drives initial OP Chain deployment under RSKj's RSKIP144 6.8M per-tx sublist gas cap**, replacing upstream's monolithic `opcmV2.deploy(config)` call (≈10M gas, can never fit). See `PLAN.md` for the design.
