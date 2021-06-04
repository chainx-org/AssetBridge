package chainset

import (
	"fmt"
	log "github.com/ChainSafe/log15"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
	"github.com/rjman-ljm/substrate-go/expand/chainx/xevents"
	"math/big"
)

func (bc *BridgeCore) GetSubChainRecipient(m msg.Message) (interface{}, error) {
	originRecipient := string(m.Payload[1].([]byte))
	fmt.Printf("Receive address: %v\n", m.Payload[1].([]byte))
	/// Convert publicKey to accountId
	if originRecipient[:2] == "0x" {
		accountId, err := types.HexDecodeString(originRecipient)
		if err != nil {
			return nil, err
		}
		m.Payload[1] = accountId
	}
	fmt.Printf("Resolve address: %v\n", m.Payload[1].([]byte))

	var multiAddressRecipient types.MultiAddress
	var addressRecipient types.Address

	// TODO: modify deal method
	//if m.Source == IdBSC || m.Source == IdKovan || m.Source == IdHeco {
	multiAddressRecipient = types.NewMultiAddressFromAccountID(m.Payload[1].([]byte))
	addressRecipient = types.NewAddressFromAccountID(m.Payload[1].([]byte))
	//} else {
	//	multiAddressRecipient, _ = types.NewMultiAddressFromHexAccountID(string(m.Payload[1].([]byte)))
	//	addressRecipient, _ = types.NewAddressFromHexAccountID(string(m.Payload[1].([]byte)))
	//}

	chainType := bc.ChainInfo.Type
	if chainType == ChainXAssetV1Like || chainType == ChainXV1Like {
		fmt.Printf("Send to `Address` recipient %v\n", addressRecipient.AsAccountID)
		return addressRecipient, nil
	} else {
		fmt.Printf("Send to `MultiAddress` recipient %v\n", multiAddressRecipient.AsID)
		return multiAddressRecipient, nil
	}
}

func (bc *BridgeCore) GetAmountFromSubToSub(origin []byte, assetId xevents.AssetId) (*big.Int, error) {
	currency, err := bc.GetCurrencyByAssetId(assetId)
	if err != nil {
		return big.NewInt(0), err
	}
	//fmt.Printf("Currency is %v\n", currency)
	return bc.CalculateAmountToSub(origin, 1, currency.FixedFee, currency.ExtraFeeRate, currency.Name)
}

func (bc *BridgeCore) GetAmountToSub(origin []byte, assetId xevents.AssetId) (*big.Int, error) {
	currency, err := bc.GetCurrencyByAssetId(assetId)
	if err != nil {
		return big.NewInt(0), err
	}
	//fmt.Printf("Currency is %v\n", currency)
	return bc.CalculateAmountToSub(origin, currency.Difference, currency.FixedFee, currency.ExtraFeeRate, currency.Name)
}

func (bc *BridgeCore) GetAmountToEth(origin []byte, assetId xevents.AssetId) (*big.Int, error) {
	currency, err := bc.GetCurrencyByAssetId(assetId)
	if err != nil {
		return big.NewInt(0), err
	}
	//fmt.Printf("Currency is %v\n", currency)
	return bc.CalculateAmountToEth(origin, currency.Difference, currency.FixedFee, currency.ExtraFeeRate, currency.Name)
}

func (bc *BridgeCore) CalculateAmountToSub(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64, token string) (*big.Int, error) {
	originAmount := big.NewInt(0).SetBytes(origin)
	receiveAmount := big.NewInt(0).Div(originAmount, big.NewInt(singleToken))

	/// Calculate fixedFee and extraFee
	fixedFee := big.NewInt(fixedTokenFee)
	extraFee := big.NewInt(0)
	if extraFeeRate != 0 {
		extraFee.Div(receiveAmount, big.NewInt(extraFeeRate))
	}
	fee := big.NewInt(0).Add(fixedFee, extraFee)

	sendAmount := big.NewInt(0).Sub(receiveAmount, fee)
	if sendAmount.Cmp(big.NewInt(0)) == -1 {
		return big.NewInt(0), fmt.Errorf("amount is too low to pay the handling fee")
	}
	log.Info("Send " + token, "OriginAmount", originAmount, "SendAmount", sendAmount)
	return sendAmount, nil
}

func (bc *BridgeCore) CalculateAmountToEth(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64, token string) (*big.Int, error) {
	originAmount := big.NewInt(0).SetBytes(origin)
	/// Calculate fixedFee and extraFee
	fixedFee := big.NewInt(fixedTokenFee)
	extraFee := big.NewInt(0)
	if extraFeeRate != 0 {
		extraFee.Div(originAmount, big.NewInt(extraFeeRate))
	}
	fee := big.NewInt(0).Add(fixedFee, extraFee)
	actualAmount := big.NewInt(0).Sub(originAmount, fee)
	if actualAmount.Cmp(big.NewInt(0)) == -1 {
		return big.NewInt(0), fmt.Errorf("amount is too low to pay the handling fee")
	}
	sendAmount := big.NewInt(0).Mul(actualAmount, big.NewInt(singleToken))

	log.Info("Send " + token, "OriginAmount", originAmount, "SendAmount", sendAmount)
	return sendAmount, nil
}

func logCrossChainTx (token string, actualAmount *big.Int) {
	log.Info("Transfer " + token, "Actual_Amount", actualAmount)
}