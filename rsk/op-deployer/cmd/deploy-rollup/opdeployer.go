package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpDeployerState represents the state.json format expected by op-deployer
type OpDeployerState struct {
	Version                   int                        `json:"version"`
	Create2Salt               string                     `json:"create2Salt"`
	AppliedIntent             *OpDeployerIntent          `json:"appliedIntent,omitempty"`
	SuperchainContracts       *SuperchainContracts       `json:"superchainContracts"`
	SuperchainRoles           *SuperchainRoles           `json:"superchainRoles"`
	ImplementationsDeployment *ImplementationsDeployment `json:"implementationsDeployment"`
	OpChainDeployments        []OpChainDeployment        `json:"opChainDeployments"`
	InteropDepSet             *InteropDepSet             `json:"interopDepSet,omitempty"`
	PrestateManifest          interface{}                `json:"prestateManifest"`
	L1StateDump               interface{}                `json:"l1StateDump,omitempty"`
	DeploymentCalldata        interface{}                `json:"DeploymentCalldata,omitempty"`
}

// SuperchainContracts holds superchain contract addresses
type SuperchainContracts struct {
	SuperchainProxyAdminImpl string `json:"SuperchainProxyAdminImpl"`
	SuperchainConfigProxy    string `json:"SuperchainConfigProxy"`
	SuperchainConfigImpl     string `json:"SuperchainConfigImpl"`
	ProtocolVersionsProxy    string `json:"ProtocolVersionsProxy"`
	ProtocolVersionsImpl     string `json:"ProtocolVersionsImpl"`
}

// SuperchainRoles holds superchain role addresses
type SuperchainRoles struct {
	SuperchainProxyAdminOwner string `json:"SuperchainProxyAdminOwner"`
	SuperchainGuardian        string `json:"SuperchainGuardian"`
	ProtocolVersionsOwner     string `json:"ProtocolVersionsOwner"`
	Challenger                string `json:"Challenger"`
}

// ImplementationsDeployment holds implementation contract addresses
type ImplementationsDeployment struct {
	OpcmImpl                         string `json:"OpcmImpl"`
	OpcmContractsContainerImpl       string `json:"OpcmContractsContainerImpl"`
	OpcmGameTypeAdderImpl            string `json:"OpcmGameTypeAdderImpl"`
	OpcmDeployerImpl                 string `json:"OpcmDeployerImpl"`
	OpcmUpgraderImpl                 string `json:"OpcmUpgraderImpl"`
	OpcmInteropMigratorImpl          string `json:"OpcmInteropMigratorImpl"`
	OpcmStandardValidatorImpl        string `json:"OpcmStandardValidatorImpl"`
	DelayedWethImpl                  string `json:"DelayedWethImpl"`
	OptimismPortalImpl               string `json:"OptimismPortalImpl"`
	OptimismPortalInteropImpl        string `json:"OptimismPortalInteropImpl"`
	EthLockboxImpl                   string `json:"EthLockboxImpl"`
	PreimageOracleImpl               string `json:"PreimageOracleImpl"`
	MipsImpl                         string `json:"MipsImpl"`
	SystemConfigImpl                 string `json:"SystemConfigImpl"`
	L1CrossDomainMessengerImpl       string `json:"L1CrossDomainMessengerImpl"`
	L1Erc721BridgeImpl               string `json:"L1Erc721BridgeImpl"`
	L1StandardBridgeImpl             string `json:"L1StandardBridgeImpl"`
	OptimismMintableErc20FactoryImpl string `json:"OptimismMintableErc20FactoryImpl"`
	DisputeGameFactoryImpl           string `json:"DisputeGameFactoryImpl"`
	AnchorStateRegistryImpl          string `json:"AnchorStateRegistryImpl"`
	FaultDisputeGameV2Impl           string `json:"FaultDisputeGameV2Impl"`
	PermissionedDisputeGameV2Impl    string `json:"PermissionedDisputeGameV2Impl"`
}

// StartBlock holds L1 block info for genesis
type StartBlock struct {
	Hash       string `json:"hash"`
	ParentHash string `json:"parentHash"`
	Number     string `json:"number"`
	Timestamp  string `json:"timestamp"`
}

// OpChainDeployment holds OP chain deployment addresses
type OpChainDeployment struct {
	ID                                 string      `json:"id"`
	OpChainProxyAdminImpl              string      `json:"OpChainProxyAdminImpl"`
	OptimismPortalProxy                string      `json:"OptimismPortalProxy"`
	AddressManagerImpl                 string      `json:"AddressManagerImpl"`
	L1Erc721BridgeProxy                string      `json:"L1Erc721BridgeProxy"`
	SystemConfigProxy                  string      `json:"SystemConfigProxy"`
	OptimismMintableErc20FactoryProxy  string      `json:"OptimismMintableErc20FactoryProxy"`
	L1StandardBridgeProxy              string      `json:"L1StandardBridgeProxy"`
	L1CrossDomainMessengerProxy        string      `json:"L1CrossDomainMessengerProxy"`
	EthLockboxProxy                    string      `json:"EthLockboxProxy"`
	DisputeGameFactoryProxy            string      `json:"DisputeGameFactoryProxy"`
	AnchorStateRegistryProxy           string      `json:"AnchorStateRegistryProxy"`
	FaultDisputeGameImpl               string      `json:"FaultDisputeGameImpl"`
	FaultDisputeGameCannonKonaImpl     string      `json:"FaultDisputeGameCannonKonaImpl"`
	PermissionedDisputeGameImpl        string      `json:"PermissionedDisputeGameImpl"`
	DelayedWethPermissionedGameProxy   string      `json:"DelayedWethPermissionedGameProxy"`
	DelayedWethPermissionlessGameProxy string      `json:"DelayedWethPermissionlessGameProxy"`
	AltDAChallengeProxy                string      `json:"AltDAChallengeProxy"`
	AltDAChallengeImpl                 string      `json:"AltDAChallengeImpl"`
	L2OutputOracleProxy                string      `json:"L2OutputOracleProxy"`
	AdditionalDisputeGames             interface{} `json:"additionalDisputeGames"`
	StartBlock                         *StartBlock `json:"startBlock"`
	Allocs                             string      `json:"allocs,omitempty"`
}

// gzipBase64Encode compresses data with gzip and encodes it as base64
// This matches op-deployer's GzipData format
func gzipBase64Encode(data interface{}) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to JSON encode data: %w", err)
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(jsonData); err != nil {
		return "", fmt.Errorf("failed to gzip data: %w", err)
	}
	if err := gw.Close(); err != nil {
		return "", fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// InteropDepSet holds interop dependency set
type InteropDepSet struct {
	Dependencies map[string]interface{} `json:"dependencies"`
}

// OpDeployerIntent represents the intent structure
type OpDeployerIntent struct {
	ConfigType            string           `json:"configType"`
	L1ChainID             uint64           `json:"l1ChainID"`
	OpcmAddress           interface{}      `json:"opcmAddress"`
	SuperchainConfigProxy interface{}      `json:"superchainConfigProxy"`
	SuperchainRoles       *SuperchainRoles `json:"superchainRoles"`
	FundDevAccounts       bool             `json:"fundDevAccounts"`
	L1ContractsLocator    string           `json:"l1ContractsLocator"`
	L2ContractsLocator    string           `json:"l2ContractsLocator"`
	Chains                []ChainIntent    `json:"chains"`
	GlobalDeployOverrides interface{}      `json:"globalDeployOverrides"`
	L1DevGenesisParams    interface{}      `json:"l1DevGenesisParams"`
}

// ChainIntent represents a chain in the intent
type ChainIntent struct {
	ID                              string       `json:"id"`
	BaseFeeVaultRecipient           string       `json:"baseFeeVaultRecipient"`
	L1FeeVaultRecipient             string       `json:"l1FeeVaultRecipient"`
	SequencerFeeVaultRecipient      string       `json:"sequencerFeeVaultRecipient"`
	Eip1559DenominatorCanyon        uint64       `json:"eip1559DenominatorCanyon"`
	Eip1559Denominator              uint64       `json:"eip1559Denominator"`
	Eip1559Elasticity               uint64       `json:"eip1559Elasticity"`
	GasLimit                        uint64       `json:"gasLimit"`
	OperatorFeeScalar               uint64       `json:"operatorFeeScalar"`
	OperatorFeeConstant             uint64       `json:"operatorFeeConstant"`
	MinBaseFee                      uint64       `json:"minBaseFee"`
	DaFootprintGasScalar            uint64       `json:"daFootprintGasScalar"`
	Roles                           *ChainRoles  `json:"roles"`
	DeployOverrides                 interface{}  `json:"deployOverrides"`
	DangerousAltDAConfig            *AltDAConfig `json:"dangerousAltDAConfig"`
	DangerousAdditionalDisputeGames interface{}  `json:"dangerousAdditionalDisputeGames"`
}

// ChainRoles holds chain-specific role addresses
type ChainRoles struct {
	L1ProxyAdminOwner string `json:"l1ProxyAdminOwner"`
	L2ProxyAdminOwner string `json:"l2ProxyAdminOwner"`
	SystemConfigOwner string `json:"systemConfigOwner"`
	UnsafeBlockSigner string `json:"unsafeBlockSigner"`
	Batcher           string `json:"batcher"`
	Proposer          string `json:"proposer"`
	Challenger        string `json:"challenger"`
}

// AltDAConfig holds alternative DA configuration
type AltDAConfig struct {
	UseAltDA                   bool   `json:"useAltDA"`
	DaCommitmentType           string `json:"daCommitmentType"`
	DaChallengeWindow          uint64 `json:"daChallengeWindow"`
	DaResolveWindow            uint64 `json:"daResolveWindow"`
	DaBondSize                 uint64 `json:"daBondSize"`
	DaResolverRefundPercentage uint64 `json:"daResolverRefundPercentage"`
}

// GenerateOpDeployerState generates state.json in op-deployer format from our deployment state
func GenerateOpDeployerState(ctx context.Context, cfg *Config, state *DeploymentState, rpcURL string) (*OpDeployerState, error) {
	// Convert L2 chain ID to bytes32 hex format
	l2ChainID, _ := new(big.Int).SetString(state.L2ChainID, 10)
	chainIDHex := fmt.Sprintf("0x%064x", l2ChainID)

	// Get current L1 block for startBlock
	startBlock, err := getLatestL1Block(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get L1 block: %w", err)
	}

	opState := &OpDeployerState{
		Version:     1,
		Create2Salt: chainIDHex,
		SuperchainContracts: &SuperchainContracts{
			SuperchainProxyAdminImpl: orZero(state.Superchain.SuperchainProxyAdmin),
			SuperchainConfigProxy:    orZero(state.Superchain.SuperchainConfigProxy),
			SuperchainConfigImpl:     orZero(state.Superchain.SuperchainConfigImpl),
			ProtocolVersionsProxy:    orZero(state.Superchain.ProtocolVersionsProxy),
			ProtocolVersionsImpl:     orZero(state.Superchain.ProtocolVersionsImpl),
		},
		SuperchainRoles: &SuperchainRoles{
			SuperchainProxyAdminOwner: orZero(cfg.EOAAddress),
			SuperchainGuardian:        orZero(cfg.Guardian),
			ProtocolVersionsOwner:     orZero(cfg.ProtocolVersionsOwner),
			Challenger:                orZero(cfg.Challenger),
		},
		ImplementationsDeployment: &ImplementationsDeployment{
			OpcmImpl:                         orZero(state.Implementations.OPCM),
			OpcmContractsContainerImpl:       orZero(state.Implementations.OpcmContractsContainerImpl),
			OpcmGameTypeAdderImpl:            orZero(state.Implementations.OpcmGameTypeAdderImpl),
			OpcmDeployerImpl:                 orZero(state.Implementations.OpcmDeployerImpl),
			OpcmUpgraderImpl:                 orZero(state.Implementations.OpcmUpgraderImpl),
			OpcmInteropMigratorImpl:          orZero(state.Implementations.OpcmInteropMigratorImpl),
			OpcmStandardValidatorImpl:        orZero(state.Implementations.OpcmStandardValidatorImpl),
			DelayedWethImpl:                  orZero(state.Implementations.DelayedWETHImpl),
			OptimismPortalImpl:               orZero(state.Implementations.OptimismPortalImpl),
			OptimismPortalInteropImpl:        orZero(state.Implementations.OptimismPortalInteropImpl),
			EthLockboxImpl:                   orZero(state.Implementations.EthLockboxImpl),
			PreimageOracleImpl:               orZero(state.Implementations.PreimageOracleSingleton),
			MipsImpl:                         orZero(state.Implementations.MipsSingleton),
			SystemConfigImpl:                 orZero(state.Implementations.SystemConfigImpl),
			L1CrossDomainMessengerImpl:       orZero(state.Implementations.L1CrossDomainMessengerImpl),
			L1Erc721BridgeImpl:               orZero(state.Implementations.L1ERC721BridgeImpl),
			L1StandardBridgeImpl:             orZero(state.Implementations.L1StandardBridgeImpl),
			OptimismMintableErc20FactoryImpl: orZero(state.Implementations.OptimismMintableERC20Factory),
			DisputeGameFactoryImpl:           orZero(state.Implementations.DisputeGameFactoryImpl),
			AnchorStateRegistryImpl:          orZero(state.Implementations.AnchorStateRegistryImpl),
			FaultDisputeGameV2Impl:           orZero(state.Implementations.FaultDisputeGameV2Impl),
			PermissionedDisputeGameV2Impl:    orZero(state.Implementations.PermissionedDisputeGameV2Impl),
		},
		OpChainDeployments: []OpChainDeployment{
			{
				ID:                                 chainIDHex,
				OpChainProxyAdminImpl:              orZero(state.OpChain.ProxyAdmin),
				OptimismPortalProxy:                orZero(state.OpChain.OptimismPortalProxy),
				AddressManagerImpl:                 orZero(state.OpChain.AddressManager),
				L1Erc721BridgeProxy:                orZero(state.OpChain.L1ERC721BridgeProxy),
				SystemConfigProxy:                  orZero(state.OpChain.SystemConfigProxy),
				OptimismMintableErc20FactoryProxy:  orZero(state.OpChain.OptimismMintableERC20Factory),
				L1StandardBridgeProxy:              orZero(state.OpChain.L1StandardBridgeProxy),
				L1CrossDomainMessengerProxy:        orZero(state.OpChain.L1CrossDomainMessengerProxy),
				EthLockboxProxy:                    orZero(state.OpChain.EthLockboxProxy),
				DisputeGameFactoryProxy:            orZero(state.OpChain.DisputeGameFactoryProxy),
				AnchorStateRegistryProxy:           orZero(state.OpChain.AnchorStateRegistryProxy),
				FaultDisputeGameImpl:               orZero(state.OpChain.FaultDisputeGame),
				FaultDisputeGameCannonKonaImpl:     zeroAddress(),
				PermissionedDisputeGameImpl:        orZero(state.OpChain.PermissionedDisputeGame),
				DelayedWethPermissionedGameProxy:   orZero(state.OpChain.DelayedWETHPermissionedProxy),
				DelayedWethPermissionlessGameProxy: orZero(state.OpChain.DelayedWETHProxy),
				AltDAChallengeProxy:                zeroAddress(),
				AltDAChallengeImpl:                 zeroAddress(),
				L2OutputOracleProxy:                zeroAddress(),
				AdditionalDisputeGames:             nil,
				StartBlock:                         startBlock,
				Allocs:                             "", // Will be populated later with gzip+base64 encoded allocs
			},
		},
		InteropDepSet: &InteropDepSet{
			Dependencies: map[string]interface{}{
				state.L2ChainID: map[string]interface{}{},
			},
		},
		PrestateManifest: nil,
	}

	return opState, nil
}

// GenerateOpDeployerIntent generates intent.toml content in op-deployer format
func GenerateOpDeployerIntent(cfg *Config, state *DeploymentState) string {
	// Convert L2 chain ID to bytes32 hex format
	l2ChainID, _ := new(big.Int).SetString(state.L2ChainID, 10)
	chainIDHex := fmt.Sprintf("0x%064x", l2ChainID)

	var sb strings.Builder
	sb.WriteString("configType = \"custom\"\n")
	sb.WriteString(fmt.Sprintf("l1ChainID = %d\n", cfg.L1ChainID))
	sb.WriteString("fundDevAccounts = false\n")
	sb.WriteString("l1ContractsLocator = \"embedded\"\n")
	sb.WriteString("l2ContractsLocator = \"embedded\"\n")
	sb.WriteString("\n")
	sb.WriteString("[superchainRoles]\n")
	sb.WriteString(fmt.Sprintf("  SuperchainProxyAdminOwner = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString(fmt.Sprintf("  SuperchainGuardian = \"%s\"\n", cfg.Guardian))
	sb.WriteString(fmt.Sprintf("  ProtocolVersionsOwner = \"%s\"\n", cfg.ProtocolVersionsOwner))
	sb.WriteString(fmt.Sprintf("  Challenger = \"%s\"\n", cfg.Challenger))
	sb.WriteString("\n")
	sb.WriteString("[[chains]]\n")
	sb.WriteString(fmt.Sprintf("  id = \"%s\"\n", chainIDHex))
	sb.WriteString(fmt.Sprintf("  baseFeeVaultRecipient = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString(fmt.Sprintf("  l1FeeVaultRecipient = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString(fmt.Sprintf("  sequencerFeeVaultRecipient = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString("  eip1559DenominatorCanyon = 0\n")
	sb.WriteString("  eip1559Denominator = 0\n")
	sb.WriteString("  eip1559Elasticity = 0\n")
	sb.WriteString("  gasLimit = 30000000\n")
	sb.WriteString("  operatorFeeScalar = 0\n")
	sb.WriteString("  operatorFeeConstant = 0\n")
	sb.WriteString("  minBaseFee = 0\n")
	sb.WriteString("  daFootprintGasScalar = 0\n")
	sb.WriteString("  [chains.roles]\n")
	sb.WriteString(fmt.Sprintf("    l1ProxyAdminOwner = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString(fmt.Sprintf("    l2ProxyAdminOwner = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString(fmt.Sprintf("    systemConfigOwner = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString(fmt.Sprintf("    unsafeBlockSigner = \"%s\"\n", cfg.EOAAddress))
	sb.WriteString(fmt.Sprintf("    batcher = \"%s\"\n", cfg.Batcher))
	sb.WriteString(fmt.Sprintf("    proposer = \"%s\"\n", cfg.Proposer))
	sb.WriteString(fmt.Sprintf("    challenger = \"%s\"\n", cfg.Challenger))
	sb.WriteString("\n")

	return sb.String()
}

// WriteOpDeployerFiles writes both intent.toml and state.json to the work directory
func WriteOpDeployerFiles(ctx context.Context, cfg *Config, state *DeploymentState) error {
	// Generate and write intent.toml
	intentContent := GenerateOpDeployerIntent(cfg, state)
	intentPath := filepath.Join(cfg.WorkDir, "intent.toml")
	if err := os.WriteFile(intentPath, []byte(intentContent), 0644); err != nil {
		return fmt.Errorf("failed to write intent.toml: %w", err)
	}

	// Generate and write state.json
	opState, err := GenerateOpDeployerState(ctx, cfg, state, cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("failed to generate op-deployer state: %w", err)
	}

	// Try to load L2 allocs from the generated file and encode as gzip+base64
	allocsPath := filepath.Join(cfg.WorkDir, "l2-allocs.json")
	if allocsData, err := os.ReadFile(allocsPath); err == nil {
		var allocs map[string]interface{}
		if err := json.Unmarshal(allocsData, &allocs); err == nil {
			// Encode allocs as gzip+base64 (op-deployer GzipData format)
			encoded, err := gzipBase64Encode(allocs)
			if err == nil && len(opState.OpChainDeployments) > 0 {
				opState.OpChainDeployments[0].Allocs = encoded
			}
		}
	}

	stateData, err := json.MarshalIndent(opState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	statePath := filepath.Join(cfg.WorkDir, "state.json")
	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		return fmt.Errorf("failed to write state.json: %w", err)
	}

	return nil
}

// getLatestL1Block gets the latest L1 block info for startBlock
func getLatestL1Block(ctx context.Context, rpcURL string) (*StartBlock, error) {
	// Fetch a block a few confirmations behind the tip to avoid reorgs.
	// RSK testnet has ~30s block times; 5 blocks ≈ 2.5 minutes of safety.
	const confirmations = 5

	// Get latest block number
	numCmd := exec.CommandContext(ctx, "cast", "block-number", "--rpc-url", rpcURL)
	numOutput, err := numCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest block number: %w", err)
	}

	var latestNum uint64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(numOutput)), "%d", &latestNum); err != nil {
		return nil, fmt.Errorf("failed to parse block number %q: %w", strings.TrimSpace(string(numOutput)), err)
	}

	safeNum := latestNum
	if latestNum > confirmations {
		safeNum = latestNum - confirmations
	}

	// Fetch the confirmed block by number
	cmd := exec.CommandContext(ctx, "cast", "block", fmt.Sprintf("%d", safeNum), "--json", "--rpc-url", rpcURL)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get block %d: %w", safeNum, err)
	}

	var blockData map[string]interface{}
	if err := json.Unmarshal(output, &blockData); err != nil {
		return nil, fmt.Errorf("failed to parse block data: %w", err)
	}

	return &StartBlock{
		Hash:       getString(blockData, "hash"),
		ParentHash: getString(blockData, "parentHash"),
		Number:     getString(blockData, "number"),
		Timestamp:  getString(blockData, "timestamp"),
	}, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func zeroAddress() string {
	return "0x0000000000000000000000000000000000000000"
}

// orZero returns the zero address if the input is empty, otherwise returns the input
func orZero(addr string) string {
	if addr == "" {
		return zeroAddress()
	}
	return addr
}
