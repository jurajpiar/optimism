// SPDX-License-Identifier: MIT
pragma solidity 0.8.15;

import { ISuperchainConfig } from "interfaces/L1/ISuperchainConfig.sol";
import { IOPContractsManagerStandardValidator } from "interfaces/L1/IOPContractsManagerStandardValidator.sol";

/// @title OPCMStandardValidatorStub
/// @notice Minimal stub for RSK compatibility where the full validator contract
///         exceeds the block gas limit. This stub passes validation checks but
///         skips actual validation logic. For use only when fault proofs are disabled.
contract OPCMStandardValidatorStub is IOPContractsManagerStandardValidator {
    /// @notice Semantic version.
    string public constant version = "1.0.0-stub";

    // Stored implementation addresses (all zeros for stub)
    Implementations private _implementations;
    ISuperchainConfig private _superchainConfig;
    address private _l1PAOMultisig;
    address private _challenger;
    uint256 private _withdrawalDelaySeconds;
    bytes32 private _devFeatureBitmap;

    constructor(
        Implementations memory implementations_,
        ISuperchainConfig superchainConfig_,
        address l1PAOMultisig_,
        address challenger_,
        uint256 withdrawalDelaySeconds_,
        bytes32 devFeatureBitmap_
    ) {
        _implementations = implementations_;
        _superchainConfig = superchainConfig_;
        _l1PAOMultisig = l1PAOMultisig_;
        _challenger = challenger_;
        _withdrawalDelaySeconds = withdrawalDelaySeconds_;
        _devFeatureBitmap = devFeatureBitmap_;
    }

    function __constructor__(
        Implementations memory,
        ISuperchainConfig,
        address,
        address,
        uint256,
        bytes32
    ) external pure {
        // No-op for interface compliance
    }

    // View functions returning stored values
    function anchorStateRegistryImpl() external view returns (address) { return _implementations.anchorStateRegistryImpl; }
    function challenger() external view returns (address) { return _challenger; }
    function delayedWETHImpl() external view returns (address) { return _implementations.delayedWETHImpl; }
    function devFeatureBitmap() external view returns (bytes32) { return _devFeatureBitmap; }
    function disputeGameFactoryImpl() external view returns (address) { return _implementations.disputeGameFactoryImpl; }
    function l1CrossDomainMessengerImpl() external view returns (address) { return _implementations.l1CrossDomainMessengerImpl; }
    function l1ERC721BridgeImpl() external view returns (address) { return _implementations.l1ERC721BridgeImpl; }
    function l1PAOMultisig() external view returns (address) { return _l1PAOMultisig; }
    function l1StandardBridgeImpl() external view returns (address) { return _implementations.l1StandardBridgeImpl; }
    function mipsImpl() external view returns (address) { return _implementations.mipsImpl; }
    function faultDisputeGameImpl() external view returns (address) { return _implementations.faultDisputeGameImpl; }
    function permissionedDisputeGameImpl() external view returns (address) { return _implementations.permissionedDisputeGameImpl; }
    function optimismMintableERC20FactoryImpl() external view returns (address) { return _implementations.optimismMintableERC20FactoryImpl; }
    function optimismPortalImpl() external view returns (address) { return _implementations.optimismPortalImpl; }
    function optimismPortalInteropImpl() external view returns (address) { return _implementations.optimismPortalInteropImpl; }
    function ethLockboxImpl() external view returns (address) { return _implementations.ethLockboxImpl; }
    function preimageOracleVersion() external pure returns (string memory) { return "1.1.2"; }
    function superchainConfig() external view returns (ISuperchainConfig) { return _superchainConfig; }
    function systemConfigImpl() external view returns (address) { return _implementations.systemConfigImpl; }
    function withdrawalDelaySeconds() external view returns (uint256) { return _withdrawalDelaySeconds; }

    // Validation functions - return empty string (success) since fault proofs are disabled
    function validate(ValidationInput memory, bool) external pure returns (string memory) {
        return "";
    }

    function validate(ValidationInputDev memory, bool) external pure returns (string memory) {
        return "";
    }

    function validateWithOverrides(
        ValidationInput memory,
        bool,
        ValidationOverrides memory
    ) external pure returns (string memory) {
        return "";
    }

    function validateWithOverrides(
        ValidationInputDev memory,
        bool,
        ValidationOverrides memory
    ) external pure returns (string memory) {
        return "";
    }
}
