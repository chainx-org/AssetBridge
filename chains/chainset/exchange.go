package chainset

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/sherpax-utils/msg"
	"math/big"
)

/// Fixed handling fee for cross-chain transactions
const (
	FixedKSMFee = SingleKSM * 3 / 100	/// 0.03KSM
	FixedDOTFee = SingleDOT * 5 / 10	/// 0.5DOT
	FixedPCXFee = SinglePCX * 1 / 10	/// 0.1PCX
)

/// Additional formalities rate excluding fixed handling fee
var ExtraFeeRate int64 = 1000

/// Calculate handling fee of cross-chain transaction
/// Eth-Like to Sub-Like
func CalculateHandleEthLikeFee(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64) *big.Int {
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

/// Sub-Like to EthLike
func CalculateHandleSubLikeFee(origin []byte, singleToken int64, fixedTokenFee int64, extraFeeRate int64) *big.Int {
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

func GetSubLikeRecipient(m msg.Message) (types.MultiAddress, types.Address) {
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
