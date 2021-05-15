package chainset

import (
	log "github.com/ChainSafe/log15"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/sherpax-utils/msg"
	"math/big"
)

/// Calculate handling fee of cross-chain transaction
/// Eth-Like to Sub-Like
func (bc *BridgeCore) CalculateHandleEthLikeFee(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64) *big.Int {
	originAmount := big.NewInt(0).SetBytes(origin)
	receiveAmount := big.NewInt(0).Div(originAmount, big.NewInt(int64(singleToken)))

	/// Calculate fixedFee and extraFee
	fixedFee := big.NewInt(fixedTokenFee)
	extraFee := big.NewInt(0).Div(receiveAmount, big.NewInt(extraFeeRate))
	fee := big.NewInt(0).Add(fixedFee, extraFee)

	sendAmount := big.NewInt(0).Sub(receiveAmount, fee)
	if sendAmount.Cmp(big.NewInt(0)) == -1 {
		//w.logErr(RedeemNegAmountError, nil)
		//return types.Call{}, nil, false, true, UnKnownError
		return big.NewInt(0)
	}
	//logCrossChainTx("BDOT", "DOT", receiveAmount, fee, actualAmount)

	return sendAmount
}

func (bc *BridgeCore) GetSendAmount(origin []byte) *big.Int {


	sendAmount := bc.CalculateHandleSubLikeFee(origin,
		DiffDOT, FixedDOTFee, ExtraFeeRate)
	logCrossChainTx("DOT", "BDOT", sendAmount)


	switch bc.ChainType {
	case PolkadotLike:
	case KusamaLike:
		sendAmount := bc.CalculateHandleSubLikeFee(origin,
			DiffKSM, FixedKSMFee, ExtraFeeRate)
		logCrossChainTx("KSM", "BKSM", sendAmount)
	case ChainXLike:
		sendAmount := bc.CalculateHandleSubLikeFee(origin,
			DiffPCX, FixedPCXFee, ExtraFeeRate)
		logCrossChainTx("PCX", "BPCX", sendAmount)
	case ChainXV1Like:
		sendAmount := bc.CalculateHandleSubLikeFee(origin,
			DiffPCX, FixedPCXFee, ExtraFeeRate)
		logCrossChainTx("PCX", "BPCX", sendAmount)
	default:
		/// ChainX with no handling fee
		sendAmount := bc.CalculateHandleSubLikeFee(origin,
			DiffXAsset, 0, 0)
		logCrossChainTx("KSM", "BKSM", sendAmount)
	}
	return
}

/// Sub-Like to EthLike
func (bc *BridgeCore) CalculateHandleSubLikeFee(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64) *big.Int {

	originAmount := big.NewInt(0).SetBytes(origin)
	/// Calculate fixedFee and extraFee
	fixedFee := big.NewInt(fixedTokenFee)
	extraFee := big.NewInt(0).Div(originAmount, big.NewInt(extraFeeRate))
	fee := big.NewInt(0).Add(fixedFee, extraFee)
	actualAmount := big.NewInt(0).Sub(originAmount, fee)
	if actualAmount.Cmp(big.NewInt(0)) == -1 {
		return big.NewInt(0)
	}
	sendAmount := big.NewInt(0).Mul(actualAmount, big.NewInt(singleToken))

	return sendAmount
}

func (bc *BridgeCore) GetSubLikeRecipient(m msg.Message) (types.MultiAddress, types.Address) {
	var multiAddressRecipient types.MultiAddress
	var addressRecipient types.Address

	if m.Source == IdBSC {
		multiAddressRecipient = types.NewMultiAddressFromAccountID(m.Payload[1].([]byte))
		addressRecipient = types.NewAddressFromAccountID(m.Payload[1].([]byte))
	} else {
		multiAddressRecipient, _ = types.NewMultiAddressFromHexAccountID(string(m.Payload[1].([]byte)))
		addressRecipient, _ = types.NewAddressFromHexAccountID(string(m.Payload[1].([]byte)))
	}
	return multiAddressRecipient, addressRecipient
}


func logCrossChainTx (tokenX string, tokenY string, actualAmount *big.Int) {
	message := tokenX + " to " + tokenY
	actualTitle := "Actual_" + tokenY + "Amount"
	log.Info(message, actualTitle, actualAmount)
}