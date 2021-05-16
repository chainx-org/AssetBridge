package chainset

import (
	"fmt"
	log "github.com/ChainSafe/log15"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/rjman-self/substrate-go/expand/chainx/xevents"
	"math/big"
)

func (bc *BridgeCore) GetSubChainRecipient(m msg.Message) interface{} {
	var multiAddressRecipient types.MultiAddress
	var addressRecipient types.Address

	// TODO: modify deal method
	if m.Source == IdBSC {
		multiAddressRecipient = types.NewMultiAddressFromAccountID(m.Payload[1].([]byte))
		addressRecipient = types.NewAddressFromAccountID(m.Payload[1].([]byte))
	} else {
		multiAddressRecipient, _ = types.NewMultiAddressFromHexAccountID(string(m.Payload[1].([]byte)))
		addressRecipient, _ = types.NewAddressFromHexAccountID(string(m.Payload[1].([]byte)))
	}

	prefix := bc.ChainInfo.Prefix
	if prefix == NameChainXAssetV1 || prefix == NameChainXV1 {
		return addressRecipient
	}

	return multiAddressRecipient
}

func (bc *BridgeCore) GetAmountToSub(origin []byte, assetId xevents.AssetId) (*big.Int, error) {
	currency, err := bc.GetCurrency(assetId)
	if err != nil {
		return big.NewInt(0), err
	}
	return bc.CalculateAmountToSub(origin, currency.Difference, currency.FixedFee, currency.ExtraFeeRate)
}

func (bc *BridgeCore) GetAmountToEth(origin []byte, assetId xevents.AssetId) (*big.Int, error) {
	currency, err := bc.GetCurrency(assetId)
	if err != nil {
		return big.NewInt(0), err
	}
	return bc.CalculateAmountToEth(origin, currency.Difference, currency.FixedFee, currency.ExtraFeeRate)
}

func (bc *BridgeCore) CalculateAmountToSub(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64) (*big.Int, error) {
	originAmount := big.NewInt(0).SetBytes(origin)
	receiveAmount := big.NewInt(0).Div(originAmount, big.NewInt(singleToken))

	/// Calculate fixedFee and extraFee
	fixedFee := big.NewInt(fixedTokenFee)
	extraFee := big.NewInt(0).Div(receiveAmount, big.NewInt(extraFeeRate))
	fee := big.NewInt(0).Add(fixedFee, extraFee)

	sendAmount := big.NewInt(0).Sub(receiveAmount, fee)
	if sendAmount.Cmp(big.NewInt(0)) == -1 {
		return big.NewInt(0), fmt.Errorf("amount is too low to pay the handling fee")
	}

	return sendAmount, nil
}

func (bc *BridgeCore) CalculateAmountToEth(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64) (*big.Int, error) {
	originAmount := big.NewInt(0).SetBytes(origin)
	/// Calculate fixedFee and extraFee
	fixedFee := big.NewInt(fixedTokenFee)
	extraFee := big.NewInt(0).Div(originAmount, big.NewInt(extraFeeRate))
	fee := big.NewInt(0).Add(fixedFee, extraFee)
	actualAmount := big.NewInt(0).Sub(originAmount, fee)
	if actualAmount.Cmp(big.NewInt(0)) == -1 {
		return big.NewInt(0), fmt.Errorf("amount is too low to pay the handling fee")
	}
	sendAmount := big.NewInt(0).Mul(actualAmount, big.NewInt(singleToken))

	return sendAmount, nil
}

func logCrossChainTx (token string, actualAmount *big.Int) {
	log.Info("Transfer " + token, "Actual_Amount", actualAmount)
}