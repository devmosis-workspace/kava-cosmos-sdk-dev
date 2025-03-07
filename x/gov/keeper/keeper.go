package keeper

import (
	"fmt"
	"time"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/gov/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
)

// Keeper defines the governance module Keeper
type Keeper struct {
	authKeeper  types.AccountKeeper
	bankKeeper  types.BankKeeper
	distrkeeper types.DistributionKeeper

	// The reference to the DelegationSet and ValidatorSet to get information about validators and delegators
	sk types.StakingKeeper

	// GovHooks
	hooks types.GovHooks

	// The (unexposed) keys used to access the stores from the Context.
	storeKey storetypes.StoreKey

	// The codec for binary encoding/decoding.
	cdc codec.BinaryCodec

	// Legacy Proposal router
	legacyRouter v1beta1.Router

	// Msg server router
	router *baseapp.MsgServiceRouter

	config types.Config

	// the address capable of executing a MsgUpdateParams message. Typically, this
	// should be the x/gov module account.
	authority string
}

// GetAuthority returns the x/gov module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// NewKeeper returns a governance keeper. It handles:
// - submitting governance proposals
// - depositing funds into proposals, and activating upon sufficient funds being deposited
// - users voting on proposals, with weight proportional to stake in the system
// - and tallying the result of the vote.
//
// CONTRACT: the parameter Subspace must have the param key table already initialized
func NewKeeper(
	cdc codec.BinaryCodec, key storetypes.StoreKey, authKeeper types.AccountKeeper,
	bankKeeper types.BankKeeper, sk types.StakingKeeper, distrkeeper types.DistributionKeeper,
	router *baseapp.MsgServiceRouter, config types.Config, authority string,
) *Keeper {
	// ensure governance module account is set
	if addr := authKeeper.GetModuleAddress(types.ModuleName); addr == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}

	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address: %s", authority))
	}

	// If MaxMetadataLen not set by app developer, set to default value.
	if config.MaxMetadataLen == 0 {
		config.MaxMetadataLen = types.DefaultConfig().MaxMetadataLen
	}

	return &Keeper{
		storeKey:    key,
		authKeeper:  authKeeper,
		bankKeeper:  bankKeeper,
		distrkeeper: distrkeeper,
		sk:          sk,
		cdc:         cdc,
		router:      router,
		config:      config,
		authority:   authority,
	}
}

// Hooks gets the hooks for governance *Keeper {
func (k *Keeper) Hooks() types.GovHooks {
	if k.hooks == nil {
		// return a no-op implementation if no hooks are set
		return types.MultiGovHooks{}
	}

	return k.hooks
}

// SetHooks sets the hooks for governance
func (k *Keeper) SetHooks(gh types.GovHooks) *Keeper {
	if k.hooks != nil {
		panic("cannot set governance hooks twice")
	}

	k.hooks = gh

	return k
}

// SetLegacyRouter sets the legacy router for governance
func (k *Keeper) SetLegacyRouter(router v1beta1.Router) {
	// It is vital to seal the governance proposal router here as to not allow
	// further handlers to be registered after the keeper is created since this
	// could create invalid or non-deterministic behavior.
	router.Seal()
	k.legacyRouter = router
}

// Logger returns a module-specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", "x/"+types.ModuleName)
}

// Router returns the gov keeper's router
func (k Keeper) Router() *baseapp.MsgServiceRouter {
	return k.router
}

// LegacyRouter returns the gov keeper's legacy router
func (k Keeper) LegacyRouter() v1beta1.Router {
	return k.legacyRouter
}

// GetGovernanceAccount returns the governance ModuleAccount
func (k Keeper) GetGovernanceAccount(ctx sdk.Context) sdk.ModuleAccountI {
	return k.authKeeper.GetModuleAccount(ctx, types.ModuleName)
}

// ProposalQueues

// InsertActiveProposalQueue inserts a proposalID into the active proposal queue at endTime
func (k Keeper) InsertActiveProposalQueue(ctx sdk.Context, proposalID uint64, endTime time.Time) {
	store := ctx.KVStore(k.storeKey)
	bz := types.GetProposalIDBytes(proposalID)
	store.Set(types.ActiveProposalQueueKey(proposalID, endTime), bz)
}

// RemoveFromActiveProposalQueue removes a proposalID from the Active Proposal Queue
func (k Keeper) RemoveFromActiveProposalQueue(ctx sdk.Context, proposalID uint64, endTime time.Time) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.ActiveProposalQueueKey(proposalID, endTime))
}

// InsertInactiveProposalQueue inserts a proposalID into the inactive proposal queue at endTime
func (k Keeper) InsertInactiveProposalQueue(ctx sdk.Context, proposalID uint64, endTime time.Time) {
	store := ctx.KVStore(k.storeKey)
	bz := types.GetProposalIDBytes(proposalID)
	store.Set(types.InactiveProposalQueueKey(proposalID, endTime), bz)
}

// RemoveFromInactiveProposalQueue removes a proposalID from the Inactive Proposal Queue
func (k Keeper) RemoveFromInactiveProposalQueue(ctx sdk.Context, proposalID uint64, endTime time.Time) {
	store := ctx.KVStore(k.storeKey)
	store.Delete(types.InactiveProposalQueueKey(proposalID, endTime))
}

// Iterators

// IterateActiveProposalsQueue iterates over the proposals in the active proposal queue
// and performs a callback function
func (k Keeper) IterateActiveProposalsQueue(ctx sdk.Context, endTime time.Time, cb func(proposal v1.Proposal) (stop bool)) {
	iterator := k.ActiveProposalQueueIterator(ctx, endTime)

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		proposalID, _ := types.SplitActiveProposalQueueKey(iterator.Key())
		proposal, found := k.GetProposal(ctx, proposalID)
		if !found {
			panic(fmt.Sprintf("proposal %d does not exist", proposalID))
		}

		if cb(proposal) {
			break
		}
	}
}

// IterateInactiveProposalsQueue iterates over the proposals in the inactive proposal queue
// and performs a callback function
func (k Keeper) IterateInactiveProposalsQueue(ctx sdk.Context, endTime time.Time, cb func(proposal v1.Proposal) (stop bool)) {
	iterator := k.InactiveProposalQueueIterator(ctx, endTime)

	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		proposalID, _ := types.SplitInactiveProposalQueueKey(iterator.Key())
		proposal, found := k.GetProposal(ctx, proposalID)
		if !found {
			panic(fmt.Sprintf("proposal %d does not exist", proposalID))
		}

		if cb(proposal) {
			break
		}
	}
}

// ActiveProposalQueueIterator returns an storetypes.Iterator for all the proposals in the Active Queue that expire by endTime
func (k Keeper) ActiveProposalQueueIterator(ctx sdk.Context, endTime time.Time) storetypes.Iterator {
	store := ctx.KVStore(k.storeKey)
	return store.Iterator(types.ActiveProposalQueuePrefix, storetypes.PrefixEndBytes(types.ActiveProposalByTimeKey(endTime)))
}

// InactiveProposalQueueIterator returns an storetypes.Iterator for all the proposals in the Inactive Queue that expire by endTime
func (k Keeper) InactiveProposalQueueIterator(ctx sdk.Context, endTime time.Time) storetypes.Iterator {
	store := ctx.KVStore(k.storeKey)
	return store.Iterator(types.InactiveProposalQueuePrefix, storetypes.PrefixEndBytes(types.InactiveProposalByTimeKey(endTime)))
}

// ModuleAccountAddress returns gov module account address
func (k Keeper) ModuleAccountAddress() sdk.AccAddress {
	return k.authKeeper.GetModuleAddress(types.ModuleName)
}

// assertMetadataLength returns an error if given metadata length
// is greater than a pre-defined MaxMetadataLen.
func (k Keeper) assertMetadataLength(metadata string) error {
	if metadata != "" && uint64(len(metadata)) > k.config.MaxMetadataLen {
		return types.ErrMetadataTooLong.Wrapf("got metadata with length %d", len(metadata))
	}
	return nil
}
