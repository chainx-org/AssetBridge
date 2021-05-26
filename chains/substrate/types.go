// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"bytes"
	"fmt"
	"github.com/centrifuge/go-substrate-rpc-client/v3/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	utils "github.com/chainx-org/AssetBridge/shared/substrate"
	"github.com/rjman-ljm/sherpax-utils/msg"
	"math/big"
	"sync"
	"time"
)

const (

	FindNewMultiSigTx 						string = "Find a MultiSign New extrinsic"
	FindApproveMultiSigTx 					string = "Find a MultiSign Approve extrinsic"
	FindExecutedMultiSigTx 					string = "Find a MultiSign Executed extrinsic"
	FindBatchMultiSigTx 					string = "Find a MultiSign Batch Extrinsic"
	FindFailedBatchMultiSigTx 				string = "But Batch Extrinsic Failed"

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

	MaybeAProblem                         	string = "There may be a problem with the deal"
	RedeemTxTryTooManyTimes               	string = "Redeem Tx failed, try too many times"
	MultiSigExtrinsicError                	string = "MultiSig extrinsic err! UnknownError(amount、chainId...)"
	RedeemNegAmountError                  	string = "Redeem a neg amount"
	NewBalancesTransferCallError          	string = "New Balances.transfer err"
	NewBalancesTransferKeepAliveCallError 	string = "New Balances.transferKeepAlive err"
	NewXAssetsTransferCallError           	string = "New XAssets.Transfer err"
	NewCrossChainTransferCallError          string = "New Cross-Chain Transfer err"
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
	Block: -2,
	txId:  0,
}

var NotExecuted = MultiSignTx{
	Block: -1,
	txId:  0,
}

var YesVoted = MultiSignTx{
	Block: -1,
	txId:  1,
}

type TimePointSafe32 struct {
	Height types.OptionU32
	Index  types.U32
}

type Round struct {
	blockHeight *big.Int
	blockRound  *big.Int
}

type Msg struct {
	m 	msg.Message
	ok 	bool
}

func NewMsg(msg msg.Message) *Msg {
	return &Msg{
		m: msg,
		ok: false,
	}
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

func (w *writer) createMultiSigTx(m msg.Message) {
	/// If there is a duplicate transaction, wait for it to complete
	w.checkRepeat(m)

	if m.Destination != w.listener.chainId {
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
		message := NewMsg(m)

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
			isFinished, currentTx := w.redeemTx(message)
			if isFinished {
				var mutex sync.Mutex
				mutex.Lock()

				/// If curTx is UnKnownError
				if currentTx == UnKnownError {
					w.log.Error(MultiSigExtrinsicError, "DepositNonce", m.DepositNonce)

					var mutex sync.Mutex
					mutex.Lock()

					/// Delete Listener msTx
					delete(w.listener.asMulti, currentTx)

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

				/// If curTx is voted
				if currentTx == YesVoted {
					message.ok = true
					time.Sleep(RoundInterval * time.Duration(w.relayer.totalRelayers) / 2)
				}
				/// Executed or UnKnownError
				if currentTx != YesVoted && currentTx != NotExecuted && currentTx != UnKnownError {
					w.log.Info(MultiSigExtrinsicExecuted, "DepositNonce", m.DepositNonce, "OriginBlock", currentTx.Block)

					var mutex sync.Mutex
					mutex.Lock()

					/// Delete Listener msTx
					delete(w.listener.asMulti, currentTx)

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
	assetId, err := w.bridgeCore.ConvertResourceIdToAssetId(m.ResourceId)
	if err != nil {
		return nil, err
	}

	sendAmount, err := w.bridgeCore.GetAmountToSub(m.Payload[0].([]byte), assetId)
	if err != nil {
		return nil, fmt.Errorf("create fungible proposal error, neg amount")
	}

	recipient := w.bridgeCore.GetSubChainRecipient(m)
	depositNonce := types.U64(m.DepositNonce)

	err = w.conn.updateMetatdata()
	if err != nil {
		return nil, err
	}

	method, err := w.resolveResourceId(m.ResourceId)
	if err != nil {
		return nil, err
	}

	call, err := types.NewCall(
		&w.conn.meta,
		method,
		recipient,
		types.NewUCompact(sendAmount),
		m.ResourceId,
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
	recipient := w.bridgeCore.GetSubChainRecipient(m)
	metadata := types.Bytes(m.Payload[2].([]byte))
	depositNonce := types.U64(m.DepositNonce)

	err := w.conn.updateMetatdata()
	if err != nil {
		return nil, err
	}

	method, err := w.resolveResourceId(m.ResourceId)
	if err != nil {
		return nil, err
	}

	call, err := types.NewCall(
		&w.conn.meta,
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
	err := w.conn.updateMetatdata()
	if err != nil {
		return nil, err
	}

	method, err := w.resolveResourceId(m.ResourceId)
	if err != nil {
		return nil, err
	}

	call, err := types.NewCall(
		&w.conn.meta,
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

func (w *writer) resolveResourceId(id [32]byte) (string, error) {
	var res []byte
	exists, err := w.conn.queryStorage(utils.BridgeStoragePrefix, "Resources", id[:], nil, &res)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("resource %x not found on chain", id)
	}
	return string(res), nil
}

// proposalValid asserts the state of a proposal. If the proposal is active and this relayer
// has not voted, it will return true. Otherwise, it will return false with a reason string.
func (w *writer) proposalValid(prop *proposal) (bool, string, error) {
	var voteRes voteState
	srcId, err := types.EncodeToBytes(prop.sourceId)
	if err != nil {
		return false, "", err
	}
	propBz, err := prop.encode()
	if err != nil {
		return false, "", err
	}
	exists, err := w.conn.queryStorage(utils.BridgeStoragePrefix, "Votes", srcId, propBz, &voteRes)
	if err != nil {
		return false, "", err
	}

	if !exists {
		return true, "", nil
	} else if voteRes.Status.IsActive {
		if containsVote(voteRes.VotesFor, types.NewAccountID(w.conn.key.PublicKey)) ||
			containsVote(voteRes.VotesAgainst, types.NewAccountID(w.conn.key.PublicKey)) {
			return false, "already voted", nil
		} else {
			return true, "", nil
		}
	} else {
		return false, "proposal complete", nil
	}
}

func containsVote(votes []types.AccountID, voter types.AccountID) bool {
	for _, v := range votes {
		if bytes.Equal(v[:], voter[:]) {
			return true
		}
	}
	return false
}

