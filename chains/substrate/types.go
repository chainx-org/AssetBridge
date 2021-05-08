// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"bytes"
	"github.com/centrifuge/go-substrate-rpc-client/v3/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/rjman-self/substrate-go/expand/chainx/xevents"
	"math/big"
	"sync"
	"time"
)

/// AssetId Type
const (
	XBTC			xevents.AssetId = 1
)
const (
	/// MultiSigTx Message
	FindNewMultiSigTx 						string = "Find a MultiSign New extrinsic"
	FindApproveMultiSigTx 					string = "Find a MultiSign Approve extrinsic"
	FindExecutedMultiSigTx 					string = "Find a MultiSign Executed extrinsic"
	FindBatchMultiSigTx 					string = "Find a MultiSign Batch Extrinsic"
	FindFailedBatchMultiSigTx 				string = "But Batch Extrinsic Failed"
	/// Other
	StartATx 								string = "Start a redeemTx..."
	MeetARepeatTx 							string = "Meet a Repeat Transaction"
	FindLostMultiSigTx 						string = "Find a Lost BatchTx"
	TryToMakeNewMultiSigTx 					string = "Try to make a New MultiSign Tx!"
	TryToApproveMultiSigTx 					string = "Try to Approve a MultiSignTx!"
	FinishARedeemTx 						string = "Finish a redeemTx"
	MultiSigExtrinsicExecuted 				string = "MultiSig extrinsic executed!"
	BlockNotYetFinalized 					string = "Block not yet finalized"
	SubListenerWorkFinished 				string = "Sub listener work is Finished"
	FailedToProcessCurrentBlock 			string = "Failed to process current block"
	FailedToWriteToBlockStore 				string = "Failed to write to blockStore"
	RelayerFinishTheTx 						string = "Relayer Finish the Tx"
	LineLog           			 			string = "------------------------------------"
	/// Error
	MaybeAProblem                         	string = "There may be a problem with the deal"
	RedeemTxTryTooManyTimes               	string = "Redeem Tx failed, try too many times"
	MultiSigExtrinsicError                	string = "MultiSig extrinsic err! UnknownError(amount、chainId...)"
	RedeemNegAmountError                  	string = "Redeem a neg amount"
	NewBalancesTransferCallError          	string = "New Balances.transfer err"
	NewBalancesTransferKeepAliveCallError 	string = "New Balances.transferKeepAlive err"
	NewXAssetsTransferCallError           	string = "New XAssets.Transfer err"
	NewMultiCallError                     	string = "New MultiCall err"
	NewApiError                           	string = "New api error"
	SignMultiSignTxFailed                 	string = "Sign MultiSignTx failed"
	SubmitExtrinsicFailed                 	string = "Submit Extrinsic Failed"
	GetMetadataError                      	string = "Get Metadata Latest err"
	GetBlockHashError                     	string = "Get BlockHash Latest err"
	GetBlockByNumberError                 	string = "Get BlockByNumber err"
	GetRuntimeVersionLatestError          	string = "Get RuntimeVersionLatest Latest err"
	GetStorageLatestError                	string = "Get StorageLatest Latest err"
	CreateStorageKeyError                 	string = "Create StorageKey err"
	ProcessBlockError                     	string = "ProcessBlock err, check it"
)

var UnKnownError = MultiSignTx{
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

/// Substrate-pallet types
type voteState struct {
	VotesFor     []types.AccountID
	VotesAgainst []types.AccountID
	Status       voteStatus
}

type voteStatus struct {
	IsActive   bool
	IsApproved bool
	IsRejected bool
}

func (m *voteStatus) Decode(decoder scale.Decoder) error {
	b, err := decoder.ReadOneByte()

	if err != nil {
		return err
	}

	if b == 0 {
		m.IsActive = true
	} else if b == 1 {
		m.IsApproved = true
	} else if b == 2 {
		m.IsRejected = true
	}

	return nil
}

// proposal represents an on-chain proposal
type proposal struct {
	depositNonce types.U64
	call         types.Call
	sourceId     types.U8
	resourceId   types.Bytes32
	method       string
}

// encode takes only nonce and call and encodes them for storage queries
func (p *proposal) encode() ([]byte, error) {
	return types.EncodeToBytes(struct {
		types.U64
		types.Call
	}{p.depositNonce, p.call})
}

func (w *writer) createNativeTx(m msg.Message) {
	w.checkRepeat(m)

	if m.Destination != w.listener.chainId {
		w.log.Info("Not Mine", "msg.DestId", m.Destination, "w.l.chainId", w.listener.chainId)
		return
	}

	w.log.Info(LineLog,"DepositNonce", m.DepositNonce, "From", m.Source, "To", m.Destination)
	w.log.Info(StartATx, "DepositNonce", m.DepositNonce, "From", m.Source, "To", m.Destination)
	w.log.Info(LineLog,"DepositNonce", m.DepositNonce, "From", m.Source, "To", m.Destination)

	/// Mark isProcessing
	destMessage := Dest{
		DepositNonce: m.DepositNonce,
		DestAddress:  string(m.Payload[1].([]byte)),
		DestAmount:   string(m.Payload[0].([]byte)),
	}
	w.messages[destMessage] = true

	go func()  {
		// calculate spend time
		start := time.Now()
		defer func() {
			cost := time.Since(start)
			w.log.Info(LineLog, "DepositNonce", m.DepositNonce)
			w.log.Info(RelayerFinishTheTx,"Relayer", w.relayer.currentRelayer, "DepositNonce", m.DepositNonce, "CostTime", cost)
			w.log.Info(LineLog, "DepositNonce", m.DepositNonce)
		}()
		retryTimes := RedeemRetryLimit
		for {
			retryTimes--
			// No more retries, stop RedeemTx
			if retryTimes < RedeemRetryLimit / 2 {
				w.log.Warn(MaybeAProblem, "RetryTimes", retryTimes)
			}
			if retryTimes == 0 {
				w.logErr(RedeemTxTryTooManyTimes, nil)
				break
			}
			isFinished, currentTx := w.redeemTx(m)
			if isFinished {
				var mutex sync.Mutex
				mutex.Lock()

				/// If currentTx is UnKnownError
				if currentTx == UnKnownError {
					w.log.Error(MultiSigExtrinsicError, "DepositNonce", m.DepositNonce)

					var mutex sync.Mutex
					mutex.Lock()

					/// Delete Listener msTx
					delete(w.listener.msTxAsMulti, currentTx)

					/// Delete Message
					dm := Dest{
						DepositNonce: m.DepositNonce,
						DestAddress:  string(m.Payload[1].([]byte)),
						DestAmount:   string(m.Payload[0].([]byte)),
					}
					delete(w.messages, dm)

					mutex.Unlock()
					break
				}

				/// If currentTx is voted
				if currentTx == YesVoted {
					time.Sleep(RoundInterval * time.Duration(w.relayer.totalRelayers) / 2)
				}
				/// Executed or UnKnownError
				if currentTx != YesVoted && currentTx != NotExecuted && currentTx != UnKnownError {
					w.log.Info(MultiSigExtrinsicExecuted, "DepositNonce", m.DepositNonce, "OriginBlock", currentTx.BlockNumber)

					var mutex sync.Mutex
					mutex.Lock()

					/// Delete Listener msTx
					delete(w.listener.msTxAsMulti, currentTx)

					/// Delete Message
					dm := Dest{
						DepositNonce: m.DepositNonce,
						DestAddress:  string(m.Payload[1].([]byte)),
						DestAmount:   string(m.Payload[0].([]byte)),
					}
					delete(w.messages, dm)

					mutex.Unlock()
					break
				}
			}
		}
		w.log.Info(FinishARedeemTx, "DepositNonce", m.DepositNonce)
	}()
}

func (w *writer) createFungibleProposal(m msg.Message) (*proposal, error) {
	bigAmt := big.NewInt(0).SetBytes(m.Payload[0].([]byte))
	amount := types.NewU128(*bigAmt)
	recipient := types.NewAccountID(m.Payload[1].([]byte))
	depositNonce := types.U64(m.DepositNonce)

	w.UpdateMetadata()
	method, err := w.resolveResourceId(m.ResourceId)
	if err != nil {
		return nil, err
	}

	call, err := types.NewCall(
		w.meta,
		method,
		recipient,
		amount,
	)
	if err != nil {
		return nil, err
	}
	if w.extendCall {
		eRID, err := types.EncodeToBytes(m.ResourceId)
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, eRID...)
	}

	return &proposal{
		depositNonce: depositNonce,
		call:         call,
		sourceId:     types.U8(m.Source),
		resourceId:   types.NewBytes32(m.ResourceId),
		method:       method,
	}, nil
}

func (w *writer) createNonFungibleProposal(m msg.Message) (*proposal, error) {
	tokenId := types.NewU256(*big.NewInt(0).SetBytes(m.Payload[0].([]byte)))
	recipient := types.NewAccountID(m.Payload[1].([]byte))
	metadata := types.Bytes(m.Payload[2].([]byte))
	depositNonce := types.U64(m.DepositNonce)

	w.UpdateMetadata()
	method, err := w.resolveResourceId(m.ResourceId)
	if err != nil {
		return nil, err
	}

	call, err := types.NewCall(
		w.meta,
		method,
		recipient,
		tokenId,
		metadata,
	)
	if err != nil {
		return nil, err
	}
	if w.extendCall {
		eRID, err := types.EncodeToBytes(m.ResourceId)
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, eRID...)
	}

	return &proposal{
		depositNonce: depositNonce,
		call:         call,
		sourceId:     types.U8(m.Source),
		resourceId:   types.NewBytes32(m.ResourceId),
		method:       method,
	}, nil
}

func (w *writer) createGenericProposal(m msg.Message) (*proposal, error) {
	w.UpdateMetadata()
	method, err := w.resolveResourceId(m.ResourceId)
	if err != nil {
		return nil, err
	}

	call, err := types.NewCall(
		w.meta,
		method,
		types.NewHash(m.Payload[0].([]byte)),
	)
	if err != nil {
		return nil, err
	}

	if w.extendCall {
		eRID, err := types.EncodeToBytes(m.ResourceId)
		if err != nil {
			return nil, err
		}

		call.Args = append(call.Args, eRID...)
	}
	return &proposal{
		depositNonce: types.U64(m.DepositNonce),
		call:         call,
		sourceId:     types.U8(m.Source),
		resourceId:   types.NewBytes32(m.ResourceId),
		method:       method,
	}, nil
}

