package types

import (
	"errors"
	"fmt"
	time "time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stktypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	asig "github.com/babylonlabs-io/babylon/v4/crypto/schnorr-adaptor-signature"
	bbn "github.com/babylonlabs-io/babylon/v4/types"
	btclightclienttypes "github.com/babylonlabs-io/babylon/v4/x/btclightclient/types"
)

func (fp *FinalityProvider) IsSlashed() bool {
	return fp.SlashedBabylonHeight > 0
}

func (fp *FinalityProvider) IsJailed() bool {
	return fp.Jailed
}

// Address returns the bech32 fp address
func (fp *FinalityProvider) Address() sdk.AccAddress {
	return sdk.MustAccAddressFromBech32(fp.Addr)
}

func (fp *FinalityProvider) ValidateBasic() error {
	// ensure fields are non-empty and well-formatted
	if _, err := sdk.AccAddressFromBech32(fp.Addr); err != nil {
		return fmt.Errorf("invalid finality provider address: %s - %w", fp.Addr, err)
	}
	if fp.BtcPk == nil {
		return fmt.Errorf("empty BTC public key")
	}
	if _, err := fp.BtcPk.ToBTCPK(); err != nil {
		return fmt.Errorf("BtcPk is not correctly formatted: %w", err)
	}
	if fp.Pop == nil {
		return fmt.Errorf("empty proof of possession")
	}
	if err := fp.Pop.ValidateBasic(); err != nil {
		return fmt.Errorf("PoP is not valid: %w", err)
	}

	return nil
}

func ExistsDup(btcPKs []bbn.BIP340PubKey) bool {
	seen := make(map[string]struct{})

	for _, btcPK := range btcPKs {
		pkStr := string(btcPK)
		if _, found := seen[pkStr]; found {
			return true
		} else {
			seen[pkStr] = struct{}{}
		}
	}

	return false
}

func NewSignatureInfo(pk *bbn.BIP340PubKey, sig *bbn.BIP340Signature) *SignatureInfo {
	return &SignatureInfo{
		Pk:  pk,
		Sig: sig,
	}
}

// GetOrderedCovenantSignatures returns the ordered covenant adaptor signatures
// encrypted by the finality provider's PK at the given index from the given list of
// covenant signatures
// the order of covenant adaptor signatures will follow the reverse lexicographical order
// of signing public keys, in order to be used as tx witness
func GetOrderedCovenantSignatures(fpIdx int, covSigsList []*CovenantAdaptorSignatures, params *Params) ([]*asig.AdaptorSignature, error) {
	// construct the map where
	// - key is the covenant PK, and
	// - value is this covenant member's adaptor signature encrypted
	//   by the given finality provider's PK
	covSigsMap := map[string]*asig.AdaptorSignature{}
	for _, covSigs := range covSigsList {
		// find the adaptor signature at the corresponding finality provider's index
		if fpIdx >= len(covSigs.AdaptorSigs) {
			return nil, fmt.Errorf("finality provider index is out of the scope")
		}
		covSigBytes := covSigs.AdaptorSigs[fpIdx]
		// decode the adaptor signature bytes
		covSig, err := asig.NewAdaptorSignatureFromBytes(covSigBytes)
		if err != nil {
			return nil, err
		}
		// append to map
		covSigsMap[covSigs.CovPk.MarshalHex()] = covSig
	}

	// sort covenant PKs in reverse lexicographical order
	orderedCovenantPKs := bbn.SortBIP340PKs(params.CovenantPks)

	// get ordered list of covenant signatures w.r.t. the order of sorted covenant PKs
	// Note that only a quorum number of covenant signatures needs to be provided
	orderedCovSigs := []*asig.AdaptorSignature{}
	for _, covPK := range orderedCovenantPKs {
		if covSig, ok := covSigsMap[covPK.MarshalHex()]; ok {
			orderedCovSigs = append(orderedCovSigs, covSig)
		} else {
			orderedCovSigs = append(orderedCovSigs, nil)
		}
	}

	return orderedCovSigs, nil
}

// NewLargestBtcReOrg creates a new Largest BTC reorg based on the rollback vars
func NewLargestBtcReOrg(rollbackFrom, rollbackTo *btclightclienttypes.BTCHeaderInfo) LargestBtcReOrg {
	return LargestBtcReOrg{
		BlockDiff:    rollbackFrom.Height - rollbackTo.Height,
		RollbackFrom: rollbackFrom,
		RollbackTo:   rollbackTo,
	}
}

func (lbr LargestBtcReOrg) Validate() error {
	if lbr.RollbackFrom == nil {
		return errors.New("rollback_from is nil")
	}

	if lbr.RollbackTo == nil {
		return errors.New("rollback_to is nil")
	}

	if err := lbr.RollbackFrom.Validate(); err != nil {
		return fmt.Errorf("error validating rollback_from: %w", err)
	}

	if err := lbr.RollbackTo.Validate(); err != nil {
		return fmt.Errorf("error validating rollback_to: %w", err)
	}

	if lbr.RollbackFrom.Height <= lbr.RollbackTo.Height {
		return fmt.Errorf("rollback_from height %d is lower or equal than rollback_to height %d", lbr.RollbackFrom.Height, lbr.RollbackTo.Height)
	}

	return nil
}

// NewCommissionInfoWithTime returns an initialized finality provider commission info with a specified
// update time which should be the current block BFT time.
func NewCommissionInfoWithTime(maxRate, maxChangeRate math.LegacyDec, updatedAt time.Time) *CommissionInfo {
	return &CommissionInfo{
		MaxRate:       maxRate,
		MaxChangeRate: maxChangeRate,
		UpdateTime:    updatedAt,
	}
}

// Validate performs basic sanity validation checks of initial commission
// info parameters. If validation fails, an SDK error is returned.
func (cr CommissionInfo) Validate() error {
	switch {
	case cr.MaxRate.IsNegative():
		// max rate cannot be negative
		return stktypes.ErrCommissionNegative

	case cr.MaxRate.GT(math.LegacyOneDec()):
		// max rate cannot be greater than 1
		return stktypes.ErrCommissionHuge

	case cr.MaxChangeRate.IsNegative():
		// change rate cannot be negative
		return stktypes.ErrCommissionChangeRateNegative

	case cr.MaxChangeRate.GT(cr.MaxRate):
		// change rate cannot be greater than the max rate
		return stktypes.ErrCommissionChangeRateGTMaxRate
	}

	return nil
}
