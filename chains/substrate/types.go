// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"bytes"
	"github.com/centrifuge/go-substrate-rpc-client/v2/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v2/types"
	"github.com/rjman-self/platdot-utils/msg"
	"github.com/rjman-self/substrate-go/expand/chainx/xevents"
	"math/big"
)

/// AssetId Type
const (
	XBTC			xevents.AssetId = 1
)
const (
	/// MultiSigTx Message
	FindNewMultiSigTx string = "Find a MultiSign New extrinsic"
	FindApproveMultiSigTx string = "Find a MultiSign Approve extrinsic"
	FindExecutedMultiSigTx string = "Find a MultiSign Executed extrinsic"
	FindBatchMultiSigTx string = "Find a MultiSign Batch Extrinsic"
	FindFailedBatchMultiSigTx = "But Batch Extrinsic Failed"
	/// Other
	StartATx string = "Start a redeemTx..."
	MeetARepeatTx string = "Meet a Repeat Transaction"
	FindLostMultiSigTx = "Find a Lost BatchTx"
	TryToMakeNewMultiSigTx string = "Try to make a New MultiSign Tx!"
	TryToApproveMultiSigTx string = "Try to Approve a MultiSignTx!"
	FinishARedeemTx string = "Finish a redeemTx"
	MultiSigExtrinsicExecuted string = "MultiSig extrinsic executed!"
	/// Error
	MaybeAProblem                         string = "There may be a problem with the deal"
	RedeemTxTryTooManyTimes               string = "Redeem Tx failed, try too many times"
	MultiSigExtrinsicError                string = "MultiSig extrinsic err! check Amount."
	RedeemNegAmountError                  string = "Redeem a neg amount"
	NewBalancesTransferCallError          string = "New Balances.transfer err"
	NewBalancesTransferKeepAliveCallError string = "New Balances.transferKeepAlive err"
	NewXAssetsTransferCallError           string = "New XAssets.Transfer err"
	NewMultiCallError                     string = "New MultiCall err"
	NewApiError                           string = "New api error"
	SignMultiSignTxError                         = "Sign MultiSignTx err"
	SubmitExtrinsicFailed                        = "Sign MultiSignTx err"
	GetMetadataError                      string = "Get Metadata Latest err"
	GetBlockHashError                     string = "Get BlockHash Latest err"
	GetRuntimeVersionLatestError          string = "Get RuntimeVersionLatest Latest err"
	GetStorageLatestError                 string = "Get StorageLatest Latest err"
	CreateStorageKeyError                 string = "Create StorageKey err"
)

var AmountError = MultiSignTx{
	BlockNumber:   -2,
	MultiSignTxId: 0,
}

var NotExecuted = MultiSignTx{
	BlockNumber:   -1,
	MultiSignTxId: 0,
}

var YesVoted = MultiSignTx{
	BlockNumber:   -1,
	MultiSignTxId: 1,
}

type TimePointSafe32 struct {
	Height types.OptionU32
	Index  types.U32
}

type Round struct {
	blockHeight *big.Int
	blockRound  *big.Int
}

type Dest struct {
	DepositNonce msg.Nonce
	DestAddress  string
	DestAmount   string
}

func EncodeCall(call types.Call) []byte {
	var buffer = bytes.Buffer{}
	encoderGoRPC := scale.NewEncoder(&buffer)
	_ = encoderGoRPC.Encode(call)
	return buffer.Bytes()
}
