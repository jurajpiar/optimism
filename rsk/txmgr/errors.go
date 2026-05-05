package txmgr

// AlreadyPublishedErrs lists RSK-specific RPC error messages that indicate a
// transaction is already in the mempool. These map to go-ethereum's
// txpool.ErrAlreadyKnown ("already known"); RSK's RPC uses different wording.
//
// Wire into txmgr.CLIConfig.AlreadyPublishedCustomErrs (or txmgr.Config) so
// the manager treats resubmissions of an already-known tx as success rather
// than a publish failure.
var AlreadyPublishedErrs = []string{
	"pending transaction with same hash already exists",
}
