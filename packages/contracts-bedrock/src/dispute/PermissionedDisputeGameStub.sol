// SPDX-License-Identifier: MIT
pragma solidity 0.8.15;

// Libraries
import { Clone } from "@solady/utils/Clone.sol";
import {
    GameStatus,
    GameType,
    BondDistributionMode,
    Claim,
    Timestamp,
    Hash,
    Position,
    Clock
} from "src/dispute/lib/Types.sol";
import {
    AlreadyInitialized,
    GameNotInProgress,
    GameNotResolved,
    GameNotFinalized,
    GamePaused,
    NoCreditToClaim,
    BondTransferFailed,
    InvalidBondDistributionMode,
    BadAuth,
    UnknownChainId
} from "src/dispute/lib/Errors.sol";

// Interfaces
import { ISemver } from "interfaces/universal/ISemver.sol";
import { IDelayedWETH } from "interfaces/dispute/IDelayedWETH.sol";
import { IAnchorStateRegistry } from "interfaces/dispute/IAnchorStateRegistry.sol";
import { IDisputeGame } from "interfaces/dispute/IDisputeGame.sol";

/// @title PermissionedDisputeGameStub
/// @notice A minimal stub implementation of the PermissionedDisputeGame interface that resolves
///         immediately as DEFENDER_WINS. This is designed for RSK L1 deployment where the full
///         FaultDisputeGame contract exceeds the block gas limit (6.8M).
///
///         This stub enables the complete L2→L1 withdrawal flow without actual dispute resolution.
///         The proposer creates games via the DisputeGameFactory, and anyone can immediately resolve
///         them in favor of the defender. Withdrawals still respect the protocol-level timing delays
///         (finality delay + proof maturity delay) configured in AnchorStateRegistry and OptimismPortal.
///
///         Trust model: The proposer's output roots are trusted (same as the legacy L2OutputOracle).
///         This contract is NOT intended for production use with untrusted proposers.
contract PermissionedDisputeGameStub is Clone, ISemver {
    ////////////////////////////////////////////////////////////////
    //                         Events                             //
    ////////////////////////////////////////////////////////////////

    /// @notice Emitted when the game is resolved.
    /// @param status The status of the game after resolution.
    event Resolved(GameStatus indexed status);

    /// @notice Emitted when the game is closed.
    event GameClosed(BondDistributionMode bondDistributionMode);

    ////////////////////////////////////////////////////////////////
    //                         State Vars                         //
    ////////////////////////////////////////////////////////////////

    /// @notice Semantic version.
    /// @custom:semver 1.0.0
    function version() public pure returns (string memory) {
        return "1.0.0";
    }

    /// @notice The starting timestamp of the game.
    Timestamp public createdAt;

    /// @notice The timestamp of the game's global resolution.
    Timestamp public resolvedAt;

    /// @notice Returns the current status of the game.
    GameStatus public status;

    /// @notice Flag for the `initialize` function to prevent re-initialization.
    bool internal initialized;

    /// @notice A boolean for whether or not the game type was respected when the game was created.
    bool public wasRespectedGameTypeWhenCreated;

    /// @notice The bond distribution mode of the game.
    BondDistributionMode public bondDistributionMode;

    /// @notice Credited balances for the game creator (the only participant in the stub).
    mapping(address => uint256) public credit;

    ////////////////////////////////////////////////////////////////
    //                       INITIALIZATION                       //
    ////////////////////////////////////////////////////////////////

    /// @notice Initializes the contract. Called by the DisputeGameFactory after cloning.
    function initialize() public payable {
        // INVARIANT: The game must not have already been initialized.
        if (initialized) revert AlreadyInitialized();

        // The creator of the dispute game must be the proposer EOA.
        if (tx.origin != proposer()) revert BadAuth();

        // Set the game as initialized.
        initialized = true;

        // Set the game's starting timestamp.
        createdAt = Timestamp.wrap(uint64(block.timestamp));

        // Set whether the game type was respected when the game was created.
        wasRespectedGameTypeWhenCreated =
            GameType.unwrap(anchorStateRegistry().respectedGameType()) == GameType.unwrap(gameType());

        // Record credit for the game creator (bond refund on claim).
        credit[gameCreator()] += msg.value;

        // Deposit the bond into WETH.
        weth().deposit{ value: msg.value }();
    }

    ////////////////////////////////////////////////////////////////
    //                        RESOLUTION                          //
    ////////////////////////////////////////////////////////////////

    /// @notice No-op for the stub. In the real FaultDisputeGame this resolves individual
    ///         claims in the dispute tree. The stub has no claims to resolve.
    /// @param _claimIndex The index of the claim to resolve (ignored).
    /// @param _numToResolve The number of sub-claims to resolve (ignored).
    function resolveClaim(uint256 _claimIndex, uint256 _numToResolve) external {
        // No-op: the stub game resolves immediately via resolve().
    }

    /// @notice Resolves the game immediately as DEFENDER_WINS.
    ///         No dispute resolution is performed.
    /// @return status_ The status of the game after resolution.
    function resolve() external returns (GameStatus status_) {
        // INVARIANT: Resolution cannot occur unless the game is currently in progress.
        if (status != GameStatus.IN_PROGRESS) revert GameNotInProgress();

        // Immediately resolve in favor of the defender (proposer).
        status_ = GameStatus.DEFENDER_WINS;
        resolvedAt = Timestamp.wrap(uint64(block.timestamp));

        // Update the status and emit the resolved event.
        emit Resolved(status = status_);
    }

    ////////////////////////////////////////////////////////////////
    //                      BOND MANAGEMENT                       //
    ////////////////////////////////////////////////////////////////

    /// @notice Closes out the game, determines the bond distribution mode, and attempts to
    ///         register the game as the anchor game.
    function closeGame() public {
        // If the bond distribution mode has already been determined, return early.
        if (bondDistributionMode == BondDistributionMode.REFUND || bondDistributionMode == BondDistributionMode.NORMAL)
        {
            return;
        } else if (bondDistributionMode != BondDistributionMode.UNDECIDED) {
            revert InvalidBondDistributionMode();
        }

        // Don't close the game while paused.
        if (anchorStateRegistry().paused()) {
            revert GamePaused();
        }

        // Make sure the game is resolved.
        if (resolvedAt.raw() == 0) {
            revert GameNotResolved();
        }

        // Game must be finalized according to the AnchorStateRegistry.
        bool finalized = anchorStateRegistry().isGameFinalized(IDisputeGame(address(this)));
        if (!finalized) {
            revert GameNotFinalized();
        }

        // Try to update the anchor game. Won't always succeed.
        try anchorStateRegistry().setAnchorState(IDisputeGame(address(this))) { } catch { }

        // Check if the game is a proper game.
        bool properGame = anchorStateRegistry().isGameProper(IDisputeGame(address(this)));

        // Set bond distribution mode.
        if (properGame) {
            bondDistributionMode = BondDistributionMode.NORMAL;
        } else {
            bondDistributionMode = BondDistributionMode.REFUND;
        }

        emit GameClosed(bondDistributionMode);
    }

    /// @notice Claim the credit belonging to the recipient address.
    /// @param _recipient The owner and recipient of the credit.
    function claimCredit(address _recipient) external {
        // Close out the game if not already closed.
        closeGame();

        // Fetch the recipient's credit.
        uint256 recipientCredit = credit[_recipient];

        // Revert if the recipient has no credit to claim.
        if (recipientCredit == 0) revert NoCreditToClaim();

        // Set the recipient's credit to 0.
        credit[_recipient] = 0;

        // Unlock and withdraw the WETH.
        weth().unlock(_recipient, recipientCredit);
        weth().withdraw(_recipient, recipientCredit);

        // Transfer the credit to the recipient.
        (bool success,) = _recipient.call{ value: recipientCredit }(hex"");
        if (!success) revert BondTransferFailed();
    }

    ////////////////////////////////////////////////////////////////
    //                      CLAIM DATA                            //
    ////////////////////////////////////////////////////////////////

    /// @notice The `ClaimData` struct represents the data associated with a Claim.
    ///         Matches the layout in FaultDisputeGame for ABI compatibility.
    struct ClaimData {
        uint32 parentIndex;
        address counteredBy;
        address claimant;
        uint128 bond;
        Claim claim;
        Position position;
        Clock clock;
    }

    /// @notice Returns the claim data for the given index.
    ///         Only index 0 (root claim) is valid for this stub.
    function claimData(uint256 _index)
        external
        view
        returns (
            uint32 parentIndex_,
            address counteredBy_,
            address claimant_,
            uint128 bond_,
            Claim claim_,
            Position position_,
            Clock clock_
        )
    {
        require(_index == 0, "claimData: invalid index");
        parentIndex_ = type(uint32).max; // Root claim sentinel (no parent)
        counteredBy_ = address(0);       // Not countered
        claimant_ = gameCreator();       // The proposer who created the game
        bond_ = uint128(credit[gameCreator()]);
        claim_ = rootClaim();
        position_ = Position.wrap(1);    // Root position
        clock_ = Clock.wrap(0);          // No clock for stub
    }

    /// @notice Returns the length of the `claimData` array.
    ///         Always 1 for the stub (only root claim).
    function claimDataLen() external pure returns (uint256 len_) {
        len_ = 1;
    }

    ////////////////////////////////////////////////////////////////
    //                     IMMUTABLE GETTERS                      //
    ////////////////////////////////////////////////////////////////

    // The CWIA layout matches PermissionedDisputeGame exactly:
    // [0, 20):     game creator address
    // [20, 52):    root claim
    // [52, 84):    l1 head (parent block hash)
    // [84, 88):    game type
    // [88, 120):   extra data (l2 block number)
    // [120, 152):  absolute prestate
    // [152, 172):  vm address
    // [172, 192):  anchor state registry address
    // [192, 212):  weth address
    // [212, 244):  l2 chain id
    // [244, 264):  proposer address
    // [264, 284):  challenger address

    /// @notice Getter for the creator of the dispute game.
    function gameCreator() public pure returns (address creator_) {
        creator_ = _getArgAddress(0);
    }

    /// @notice Getter for the root claim.
    function rootClaim() public pure returns (Claim rootClaim_) {
        rootClaim_ = Claim.wrap(_getArgBytes32(20));
    }

    /// @notice Getter for the root claim for a given L2 chain ID.
    function rootClaimByChainId(uint256 _chainId) public pure returns (Claim rootClaim_) {
        if (_chainId != l2ChainId()) revert UnknownChainId();
        rootClaim_ = rootClaim();
    }

    /// @notice Getter for the parent hash of the L1 block when the dispute game was created.
    function l1Head() public pure returns (Hash l1Head_) {
        l1Head_ = Hash.wrap(_getArgBytes32(52));
    }

    /// @notice Getter for the game type.
    function gameType() public pure returns (GameType gameType_) {
        gameType_ = GameType.wrap(_getArgUint32(84));
    }

    /// @notice Getter for the extra data.
    function extraData() public pure returns (bytes memory extraData_) {
        extraData_ = _getArgBytes(88, 32);
    }

    /// @notice The l2BlockNumber of the disputed output root.
    function l2BlockNumber() public pure returns (uint256 l2BlockNumber_) {
        l2BlockNumber_ = _getArgUint256(88);
    }

    /// @notice The l2SequenceNumber (same as l2BlockNumber for this game type).
    function l2SequenceNumber() public pure returns (uint256 l2SequenceNumber_) {
        l2SequenceNumber_ = l2BlockNumber();
    }

    /// @notice Getter for the anchor state registry.
    function anchorStateRegistry() public pure returns (IAnchorStateRegistry registry_) {
        registry_ = IAnchorStateRegistry(_getArgAddress(172));
    }

    /// @notice Getter for the WETH contract.
    function weth() public pure returns (IDelayedWETH weth_) {
        weth_ = IDelayedWETH(payable(_getArgAddress(192)));
    }

    /// @notice Getter for the L2 chain ID.
    function l2ChainId() public pure returns (uint256 l2ChainId_) {
        l2ChainId_ = _getArgUint256(212);
    }

    /// @notice Returns the proposer address.
    function proposer() public pure returns (address proposer_) {
        proposer_ = _getArgAddress(244);
    }

    /// @notice Returns the challenger address.
    function challenger() public pure returns (address challenger_) {
        challenger_ = _getArgAddress(264);
    }

    /// @notice Returns the game type, root claim, and extra data.
    function gameData() external pure returns (GameType gameType_, Claim rootClaim_, bytes memory extraData_) {
        gameType_ = gameType();
        rootClaim_ = rootClaim();
        extraData_ = extraData();
    }
}
