package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/osmosis-labs/osmosis/x/txfees/types"
)

// MempoolFeeDecorator will check if the transaction's fee is at least as large
// as the local validator's minimum gasFee (defined in validator config).
// If fee is too low, decorator returns error and tx is rejected from mempool.
// Note this only applies when ctx.CheckTx = true
// If fee is high enough or not CheckTx, then call next AnteHandler
// CONTRACT: Tx must implement FeeTx to use MempoolFeeDecorator
type MempoolFeeDecorator struct {
	TxFeesKeeper Keeper
}

func NewMempoolFeeDecorator(txFeesKeeper Keeper, spotPriceCalculator types.SpotPriceCalculator) MempoolFeeDecorator {
	return MempoolFeeDecorator{}
}

func (mfd MempoolFeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	feeCoins := feeTx.GetFee()
	gas := feeTx.GetGas()

	if len(feeCoins) > 1 {
		return ctx, types.ErrTooManyFeeCoins
	}

	baseDenom, err := mfd.TxFeesKeeper.GetBaseDenom(ctx)
	if err != nil {
		return ctx, err
	}

	// If there is a fee attached to the tx, make sure the fee denom is a valid denom
	if len(feeCoins) == 1 {
		feeDenom := feeCoins.GetDenomByIndex(0)
		if feeDenom != baseDenom {
			_, err := mfd.TxFeesKeeper.GetFeeToken(ctx, feeDenom)
			if err != nil {
				return ctx, err
			}
		}
	}

	// Ensure that the provided fees meet a minimum threshold for the validator,
	// if this is a CheckTx. This is only for local mempool purposes, and thus
	// is only ran on check tx.
	if ctx.IsCheckTx() && !simulate {
		minGasPrices := ctx.MinGasPrices()
		baseDenomAmt := minGasPrices.AmountOf(baseDenom)
		if !(baseDenomAmt.IsZero()) {

			if len(feeCoins) != 1 {
				return ctx, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "no fee attached")
			}

			// Determine the required fees by multiplying the required minimum gas
			// price by the gas limit, where fee = ceil(minGasPrice * gasLimit).
			glDec := sdk.NewDec(int64(gas))
			requiredBaseFee := sdk.NewCoin(baseDenom, baseDenomAmt.Mul(glDec).Ceil().RoundInt())

			if feeCoins[0].Denom == baseDenom {
				if !(feeCoins[0].IsGTE(requiredBaseFee)) {
					return ctx, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "insufficient fees; got: %s required: %s", feeCoins, requiredBaseFee)
				}
			} else {
				convertedFee, err := mfd.TxFeesKeeper.ConvertToBaseToken(ctx, feeCoins[0])
				if err != nil {
					return ctx, err
				}
				if !(convertedFee.IsGTE(requiredBaseFee)) {
					return ctx, sdkerrors.Wrapf(sdkerrors.ErrInsufficientFee, "insufficient fees; got: %s which converts to %s. required: %s", feeCoins, convertedFee, requiredBaseFee)
				}
			}
		}
	}

	return next(ctx, tx, simulate)
}
