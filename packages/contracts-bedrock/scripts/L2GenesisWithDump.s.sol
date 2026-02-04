// SPDX-License-Identifier: MIT
pragma solidity 0.8.15;

import { Script } from "forge-std/Script.sol";
import { L2Genesis } from "scripts/L2Genesis.s.sol";

/// @title L2GenesisWithDump
/// @notice Wrapper around L2Genesis that dumps state to a JSON file after generation.
///         This allows capturing the L2 genesis allocations for use in rollup configuration.
contract L2GenesisWithDump is Script {
    L2Genesis internal genesis;

    constructor() {
        genesis = new L2Genesis();
    }

    /// @notice Runs L2Genesis and dumps the resulting state to a file.
    /// @param _input The L2Genesis input parameters
    /// @param _outputPath The path to write the state dump JSON file
    function run(L2Genesis.Input memory _input, string memory _outputPath) public {
        // Run the L2Genesis script to set up all predeploys
        genesis.run(_input);

        // Dump the entire EVM state to a JSON file
        vm.dumpState(_outputPath);
    }

    /// @notice Convenience function that reads input from environment variables
    /// @param _outputPath The path to write the state dump JSON file
    function runWithEnv(string memory _outputPath) public {
        L2Genesis.Input memory input = _buildInputFromEnv();
        run(input, _outputPath);
    }

    /// @notice Build the L2Genesis.Input struct from environment variables
    function _buildInputFromEnv() internal view returns (L2Genesis.Input memory) {
        return L2Genesis.Input({
            l1ChainID: vm.envUint("L1_CHAIN_ID"),
            l2ChainID: vm.envUint("L2_CHAIN_ID"),
            l1CrossDomainMessengerProxy: payable(vm.envAddress("L1_CROSS_DOMAIN_MESSENGER_PROXY")),
            l1StandardBridgeProxy: payable(vm.envAddress("L1_STANDARD_BRIDGE_PROXY")),
            l1ERC721BridgeProxy: payable(vm.envAddress("L1_ERC721_BRIDGE_PROXY")),
            opChainProxyAdminOwner: vm.envAddress("OP_CHAIN_PROXY_ADMIN_OWNER"),
            sequencerFeeVaultRecipient: vm.envAddress("SEQUENCER_FEE_VAULT_RECIPIENT"),
            sequencerFeeVaultMinimumWithdrawalAmount: vm.envOr("SEQUENCER_FEE_VAULT_MIN_WITHDRAWAL", uint256(10 ether)),
            sequencerFeeVaultWithdrawalNetwork: vm.envOr("SEQUENCER_FEE_VAULT_WITHDRAWAL_NETWORK", uint256(0)), // 0 = L1
            baseFeeVaultRecipient: vm.envAddress("BASE_FEE_VAULT_RECIPIENT"),
            baseFeeVaultMinimumWithdrawalAmount: vm.envOr("BASE_FEE_VAULT_MIN_WITHDRAWAL", uint256(10 ether)),
            baseFeeVaultWithdrawalNetwork: vm.envOr("BASE_FEE_VAULT_WITHDRAWAL_NETWORK", uint256(0)), // 0 = L1
            l1FeeVaultRecipient: vm.envAddress("L1_FEE_VAULT_RECIPIENT"),
            l1FeeVaultMinimumWithdrawalAmount: vm.envOr("L1_FEE_VAULT_MIN_WITHDRAWAL", uint256(10 ether)),
            l1FeeVaultWithdrawalNetwork: vm.envOr("L1_FEE_VAULT_WITHDRAWAL_NETWORK", uint256(0)), // 0 = L1
            governanceTokenOwner: vm.envOr("GOVERNANCE_TOKEN_OWNER", address(0)),
            fork: vm.envOr("L2_GENESIS_FORK", uint256(8)), // 8 = Holocene (latest)
            deployCrossL2Inbox: vm.envOr("DEPLOY_CROSS_L2_INBOX", false),
            enableGovernance: vm.envOr("ENABLE_GOVERNANCE", false),
            fundDevAccounts: vm.envOr("FUND_DEV_ACCOUNTS", false)
        });
    }
}
