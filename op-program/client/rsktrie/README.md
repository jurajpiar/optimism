# RSK Binary Unitrie Support for op-program

This package enables the OP Stack fault proof system to work with RSK as L1, by replacing Ethereum's Merkle Patricia Trie (MPT) with RSK's binary unitrie (RSKIP-107) for L1 data verification.

## Background

The fault proof program (`op-program`) runs inside Cannon's MIPS VM to verify L2 state transitions. It reads L1 data (block headers, transactions, receipts) by reconstructing Merkle tries from block header roots. RSK uses a binary unitrie instead of MPT, so the standard trie reconstruction fails.

### What changed

The L2 side is unaffected — op-geth uses stock Ethereum EVM with standard MPT. The PreimageOracle Solidity contract and MIPS.sol also don't need changes; they just store/retrieve keccak256-keyed data. All trie-specific logic lives in op-program (Go code running in the MIPS VM).

### What didn't change

- `PreimageOracle.sol` — stores hash-keyed preimage data (trie-agnostic)
- `MIPS.sol` — verifies single MIPS instructions (trie-agnostic)
- `FaultDisputeGame.sol` — bisection logic (trie-agnostic)
- L2 state proofs — stock Ethereum MPT
- op-geth L2 execution — stock EVM

## Architecture

RSK-specific code paths are gated on `IsRSKChain(l1ChainID)`, detected automatically at boot time. No fork of op-program is needed.

```
op-program client (MIPS VM):
  l1/oracle.go
    ├─ HeaderByBlockHash()       → RSK: decode RSK header
    ├─ TransactionsByBlockHash() → RSK: rsktrie.ReadTrie + DecodeRSKTransactions
    └─ ReceiptsByBlockHash()     → RSK: rsktrie.ReadTrie + DecodeRSKReceipts

op-program host (prefetcher):
  prefetcher.go
    ├─ storeTransactions() → RSK: EncodeRSKTransactions + rsktrie.WriteTrie
    └─ storeReceipts()     → RSK: EncodeRSKReceipts + rsktrie.WriteTrie
```

## Package contents

| File | Purpose |
|------|---------|
| `trie.go` | `ReadTrie` / `WriteTrie` — binary unitrie equivalents of `mpt.ReadTrie` / `mpt.WriteTrie` |
| `block_info.go` | `RSKBlockInfo` — implements `eth.BlockInfo` for RSK block headers |
| `codec.go` | Encode/decode between go-ethereum types and RSK RLP format for txs and receipts |
| `chain.go` | RSK chain ID detection (mainnet=30, testnet=31, regtest=33) |

## How the binary unitrie differs from MPT

| | RSK Binary Unitrie | Ethereum MPT |
|---|---|---|
| Branching | Binary (2 children) | Hexary (16 children) |
| Key granularity | 1 bit per level | 4 bits (nibble) per level |
| Path compression | Shared paths at node level | Extension nodes |
| Serialization | RSKIP-107 flags+path+refs | RLP list encoding |
| Hash input | `keccak256(serialized)` | `keccak256(rlp_encoded)` |
| Embedded nodes | Inlined if < 44 bytes | N/A |
| Long values | Stored separately if > 32 bytes | Always inline |

Both use keccak256 for node hashing, so the preimage oracle key scheme (`keccak256(data) -> data`) works for both.

## Dependencies

This package imports `gorsk/rsktrie` and `gorsk/rskblocks` from the gorsk module (pure Go, no CGO). The gorsk module is added to `optimism/go.mod` via a `replace` directive pointing to the sibling `../gorsk` directory.

MIPS cross-compilation (`GOOS=linux GOARCH=mips64 GOMIPS64=softfloat`) is verified.

## Changes to gorsk

The following changes were made to gorsk as part of this work:

- **`rsktrie/trie.go`**: Implemented long value retrieval via `TrieStore.RetrieveValue()` (was TODO)
- **`rskblocks/block_header.go`**: Added `DecodeRLPBlockHeader()` for RLP decoding of RSK headers
- **`rskblocks/transaction.go`**: Added `RawSignatureValues()` accessor
- **`rskblocks/*.go`**: Fixed stale import paths (`github.com/ethereum-optimism/...` → `gorsk/...`)

## Changes to op-program

- **`client/l1/oracle.go`**: Added `isRSK` field and `SetRSKMode()`. Header decoding, transaction/receipt reading conditionally dispatch to RSK or Ethereum paths.
- **`client/l1/cache.go`**: `SetRSKMode()` delegation to underlying oracle.
- **`client/program.go`**: Auto-detects RSK L1 from `bootInfo.RollupConfig.L1ChainID` and calls `SetRSKMode(true)`.
- **`host/prefetcher/prefetcher.go`**: Added `isRSK` field and `NewPrefetcherWithOpts()`. Transaction/receipt storage conditionally uses RSK encoding + binary unitrie.
- **`host/host.go`**: Detects RSK chain ID from rollup config, passes `isRSK` to prefetcher.

## Testing

Run the rsktrie tests:

```bash
cd optimism
go test ./op-program/client/rsktrie/ -v
```

Tests cover:
- Trie write/read round-trip (arbitrary data)
- Single value and empty trie edge cases
- Transaction codec round-trip (go-ethereum ↔ RSK RLP)
- Receipt codec round-trip (go-ethereum ↔ RSK RLP)
- Full pipeline: encode txs → build RSK trie → read trie → decode txs

## Remaining work

- **Host-side RSK header RLP**: The L1 fetcher returns headers as `types.Header` which loses RSK-specific fields. For full correctness, the host needs to store raw RSK header RLP from the RPC response rather than re-encoding from `types.Header`. Currently the RSK header decoder on the client side handles the fields needed for fault proofs (TxTrieRoot, ReceiptTrieRoot).
- **End-to-end with Cannon**: Run op-program inside Cannon against real RSK L1 data and verify correct L2 output root computation.
- **op-challenger integration**: Configure op-challenger to run against an RSK-backed OP Stack chain with FaultDisputeGame (game type 0).
