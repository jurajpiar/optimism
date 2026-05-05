package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// DeploymentState tracks the state of the deployment for resumability
type DeploymentState struct {
	// Chain identifier
	L2ChainID string `json:"l2ChainId"`

	// Superchain deployment addresses
	Superchain *SuperchainAddresses `json:"superchain,omitempty"`

	// Implementation deployment addresses
	Implementations *ImplementationAddresses `json:"implementations,omitempty"`

	// OP Chain deployment addresses
	OpChain *OpChainAddresses `json:"opChain,omitempty"`

	// Genesis generation state
	L2AllocsGenerated bool   `json:"l2AllocsGenerated,omitempty"`
	GenesisGenerated  bool   `json:"genesisGenerated,omitempty"`
	GenesisHash       string `json:"genesisHash,omitempty"`
}

// SuperchainAddresses holds the addresses from superchain deployment
type SuperchainAddresses struct {
	SuperchainProxyAdmin  string `json:"superchainProxyAdmin"`
	SuperchainConfigImpl  string `json:"superchainConfigImpl"`
	SuperchainConfigProxy string `json:"superchainConfigProxy"`
	ProtocolVersionsImpl  string `json:"protocolVersionsImpl"`
	ProtocolVersionsProxy string `json:"protocolVersionsProxy"`
}

// ImplementationAddresses holds the addresses from implementation deployment
type ImplementationAddresses struct {
	OPCM                          string `json:"opcm"`
	OpcmContractsContainerImpl    string `json:"opcmContractsContainerImpl,omitempty"`
	OpcmGameTypeAdderImpl         string `json:"opcmGameTypeAdderImpl,omitempty"`
	OpcmDeployerImpl              string `json:"opcmDeployerImpl,omitempty"`
	OpcmUpgraderImpl              string `json:"opcmUpgraderImpl,omitempty"`
	OpcmInteropMigratorImpl       string `json:"opcmInteropMigratorImpl,omitempty"`
	OpcmStandardValidatorImpl     string `json:"opcmStandardValidatorImpl,omitempty"`
	DelayedWETHImpl               string `json:"delayedWETHImpl,omitempty"`
	OptimismPortalImpl            string `json:"optimismPortalImpl,omitempty"`
	OptimismPortalInteropImpl     string `json:"optimismPortalInteropImpl,omitempty"`
	EthLockboxImpl                string `json:"ethLockboxImpl,omitempty"`
	PreimageOracleSingleton       string `json:"preimageOracleSingleton,omitempty"`
	MipsSingleton                 string `json:"mipsSingleton,omitempty"`
	SystemConfigImpl              string `json:"systemConfigImpl,omitempty"`
	L1CrossDomainMessengerImpl    string `json:"l1CrossDomainMessengerImpl,omitempty"`
	L1ERC721BridgeImpl            string `json:"l1ERC721BridgeImpl,omitempty"`
	L1StandardBridgeImpl          string `json:"l1StandardBridgeImpl,omitempty"`
	OptimismMintableERC20Factory  string `json:"optimismMintableERC20FactoryImpl,omitempty"`
	DisputeGameFactoryImpl        string `json:"disputeGameFactoryImpl,omitempty"`
	AnchorStateRegistryImpl       string `json:"anchorStateRegistryImpl,omitempty"`
	FaultDisputeGameV2Impl        string `json:"faultDisputeGameV2Impl,omitempty"`
	PermissionedDisputeGameV2Impl string `json:"permissionedDisputeGameV2Impl,omitempty"`
}

// OpChainAddresses holds the addresses from OP chain deployment
type OpChainAddresses struct {
	ProxyAdmin                   string `json:"proxyAdmin"`
	AddressManager               string `json:"addressManager,omitempty"`
	L1ERC721BridgeProxy          string `json:"l1ERC721BridgeProxy,omitempty"`
	SystemConfigProxy            string `json:"systemConfigProxy"`
	OptimismMintableERC20Factory string `json:"optimismMintableERC20FactoryProxy,omitempty"`
	L1StandardBridgeProxy        string `json:"l1StandardBridgeProxy,omitempty"`
	L1CrossDomainMessengerProxy  string `json:"l1CrossDomainMessengerProxy,omitempty"`
	OptimismPortalProxy          string `json:"optimismPortalProxy,omitempty"`
	EthLockboxProxy              string `json:"ethLockboxProxy,omitempty"`
	DisputeGameFactoryProxy      string `json:"disputeGameFactoryProxy,omitempty"`
	AnchorStateRegistryProxy     string `json:"anchorStateRegistryProxy,omitempty"`
	AnchorStateRegistryImpl      string `json:"anchorStateRegistryImpl,omitempty"`
	FaultDisputeGame             string `json:"faultDisputeGame,omitempty"`
	PermissionedDisputeGame      string `json:"permissionedDisputeGame,omitempty"`
	DelayedWETHPermissionedProxy string `json:"delayedWETHPermissionedProxy,omitempty"`
	DelayedWETHProxy             string `json:"delayedWETHProxy,omitempty"`
}

// LoadDeploymentState loads the deployment state from file
func LoadDeploymentState(path string) (*DeploymentState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &DeploymentState{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state DeploymentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// Save saves the deployment state to file
func (s *DeploymentState) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// ContractChecker verifies if contracts exist on-chain
type ContractChecker struct {
	client *ethclient.Client
	rpcURL string
}

// NewContractChecker creates a new contract checker
func NewContractChecker(rpcURL string) (*ContractChecker, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}
	return &ContractChecker{client: client, rpcURL: rpcURL}, nil
}

// Close closes the client connection
func (c *ContractChecker) Close() {
	if c.client != nil {
		c.client.Close()
	}
}

// HasCode checks if an address has deployed code
func (c *ContractChecker) HasCode(ctx context.Context, address string) (bool, error) {
	if address == "" || address == "0x0000000000000000000000000000000000000000" {
		return false, nil
	}

	addr := common.HexToAddress(address)
	code, err := c.client.CodeAt(ctx, addr, nil)
	if err != nil {
		return false, fmt.Errorf("failed to get code at %s: %w", address, err)
	}

	return len(code) > 0, nil
}

// IsSuperchainDeployed checks if superchain contracts are deployed
func (s *DeploymentState) IsSuperchainDeployed(ctx context.Context, checker *ContractChecker) (bool, error) {
	if s.Superchain == nil {
		return false, nil
	}

	// Check if SuperchainConfigProxy has code
	if s.Superchain.SuperchainConfigProxy == "" {
		return false, nil
	}

	hasCode, err := checker.HasCode(ctx, s.Superchain.SuperchainConfigProxy)
	if err != nil {
		return false, err
	}

	if !hasCode {
		return false, nil
	}

	// Also verify ProtocolVersionsProxy
	if s.Superchain.ProtocolVersionsProxy == "" {
		return false, nil
	}

	hasCode, err = checker.HasCode(ctx, s.Superchain.ProtocolVersionsProxy)
	if err != nil {
		return false, err
	}

	return hasCode, nil
}

// IsImplementationsDeployed checks if implementation contracts are deployed
func (s *DeploymentState) IsImplementationsDeployed(ctx context.Context, checker *ContractChecker) (bool, error) {
	if s.Implementations == nil {
		return false, nil
	}

	// Check if OPCM has code (primary indicator)
	if s.Implementations.OPCM == "" {
		return false, nil
	}

	hasCode, err := checker.HasCode(ctx, s.Implementations.OPCM)
	if err != nil {
		return false, err
	}

	return hasCode, nil
}

// IsOpChainDeployed checks if OP chain contracts are deployed
func (s *DeploymentState) IsOpChainDeployed(ctx context.Context, checker *ContractChecker) (bool, error) {
	if s.OpChain == nil {
		return false, nil
	}

	// Check if SystemConfigProxy has code (primary indicator)
	if s.OpChain.SystemConfigProxy == "" {
		return false, nil
	}

	hasCode, err := checker.HasCode(ctx, s.OpChain.SystemConfigProxy)
	if err != nil {
		return false, err
	}

	return hasCode, nil
}

// SetSuperchainAddresses updates superchain addresses in state
func (s *DeploymentState) SetSuperchainAddresses(addrs *SuperchainAddresses) {
	s.Superchain = addrs
}

// SetImplementationAddresses updates implementation addresses in state
func (s *DeploymentState) SetImplementationAddresses(addrs *ImplementationAddresses) {
	s.Implementations = addrs
}

// SetOpChainAddresses updates OP chain addresses in state
func (s *DeploymentState) SetOpChainAddresses(addrs *OpChainAddresses) {
	s.OpChain = addrs
}
