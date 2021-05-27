// Copyright 2021 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"fmt"
	"github.com/ChainSafe/log15"
	"github.com/chainx-org/AssetBridge/chains/chainset"
	utils "github.com/chainx-org/AssetBridge/shared/substrate"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-ljm/sherpax-utils/core"
	metrics "github.com/rjman-ljm/sherpax-utils/metrics/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
	"github.com/rjman-ljm/substrate-go/expand/chainx/xevents"
	utils2 "github.com/rjman-ljm/substrate-go/utils"
	"math/big"
	"sync"
	"time"
)

var _ core.Writer = &writer{}
var AcknowledgeProposal utils.Method = utils.BridgePalletName + ".acknowledge_proposal"
var TerminatedError = errors.New("terminated")

const genesisBlock = 0
const RoundInterval = time.Second * 6

type writer struct {
	conn       *Connection
	listener   *listener
	log        log15.Logger
	sysErr     chan<- error
	metrics    *metrics.ChainMetrics
	extendCall bool // Extend extrinsic calls to substrate with ResourceID.Used for backward compatibility with example pallet.
	relayer    Relayer
	maxWeight  uint64
	messages   map[Dest]bool
	bridgeCore *chainset.BridgeCore
}

func NewWriter(conn *Connection, listener *listener, log log15.Logger, sysErr chan<- error,
	m *metrics.ChainMetrics, extendCall bool, weight uint64, relayer Relayer, bc *chainset.BridgeCore) *writer {

	return &writer{
		conn:       conn,
		listener:   listener,
		log:        log,
		sysErr:     sysErr,
		metrics:    m,
		extendCall: extendCall,
		relayer:    relayer,
		maxWeight:  weight,
		messages:   make(map[Dest]bool, InitCapacity),
		bridgeCore: bc,
	}
}

func (w *writer) ResolveMessage(m msg.Message) bool {
	var prop *proposal
	var err error

	// Construct the Transaction
	switch m.Type {
	case msg.MultiSigTransfer:
		w.createMultiSigTx(m)
		return true
	case msg.FungibleTransfer:
		w.log.Info(LineLog, "DepositNonce", m.DepositNonce)
		w.log.Info("Start Deposit...", "DepositNonce", m.DepositNonce)
		w.log.Info(LineLog, "DepositNonce", m.DepositNonce)
		prop, err = w.createFungibleProposal(m)
	case msg.NonFungibleTransfer:
		prop, err = w.createNonFungibleProposal(m)
	case msg.GenericTransfer:
		prop, err = w.createGenericProposal(m)
	default:
		w.log.Info("unrecognized message type received", "Chain", w.conn.name, "Dest", m.Destination)
		return false
	}
	if err != nil {
		w.logErr("failed to construct proposal", err)
		return false
	}

	for i := 0; i < BlockRetryLimit; i++ {
		// Ensure we only submit a vote if the proposal hasn't completed
		valid, reason, err := w.proposalValid(prop)
		if err != nil {
			w.log.Error("Failed to assert proposal state", "err", err)
			time.Sleep(BlockRetryInterval)
			continue
		}

		// If active submit call, otherwise skip it. Retry on failure.
		if valid {
			//fmt.Printf("method is %v\nnonce is %v\nsourceId is %v\nResourceId is %v\ncall\n%v\n", AcknowledgeProposal, prop.depositNonce, prop.sourceId, prop.resourceId, prop.call)
			err = w.conn.SubmitTx(AcknowledgeProposal, prop.depositNonce, prop.sourceId, prop.resourceId, prop.call)
			if err != nil && err.Error() == TerminatedError.Error() {
				return false
			} else if err != nil {
				w.log.Error("Failed to execute extrinsic", "err", err)
				time.Sleep(BlockRetryInterval)
				continue
			}

			assetId , err := w.bridgeCore.ConvertResourceIdToAssetId(msg.ResourceId(prop.resourceId))
			if err != nil {
				w.logErr("rId to assetId err", err)
			}
			w.log.Info(LineLog, "DepositNonce", m.DepositNonce)
			w.log.Info("End Deposit, acknowledging proposal on chain", "nonce", prop.depositNonce, "source", prop.sourceId, "AssetId", assetId)
			w.log.Info(LineLog, "DepositNonce", m.DepositNonce)
			if w.metrics != nil {
				w.metrics.VotesSubmitted.Inc()
			}
			return true
		} else {
			w.log.Info("Ignoring proposal", "reason", reason, "nonce", prop.depositNonce, "source", prop.sourceId, "resource", prop.resourceId)
			return true
		}
	}
	return true
}

func (w *writer) checkRepeat(m msg.Message) bool {
	for {
		isRepeat := false
		/// Lock
		var mutex sync.Mutex
		mutex.Lock()
		for dest := range w.messages {
			if dest.DepositNonce != m.DepositNonce && dest.DestAmount == string(m.Payload[0].([]byte)) && dest.DestAddress == string(m.Payload[1].([]byte)) {
				isRepeat = true
			}
		}
		mutex.Unlock()

		/// Check Repeat
		if isRepeat {
			repeatTime := RoundInterval
			w.log.Info(MeetARepeatTx, "DepositNonce", m.DepositNonce, "Waiting", repeatTime)
			time.Sleep(repeatTime)
		} else {
			break
		}
	}
	return true
}

func (w *writer) redeemTx(message *Msg) (bool, MultiSignTx) {
	//w.UpdateMetadata()
	m := message.m
	types.SetSerDeOptions(types.SerDeOptions{NoPalletIndices: true})

	defer func() {
		/// Single thread send one time each round
		time.Sleep(RoundInterval)
	}()

	/// BEGIN: Create a call of MultiSignTransfer
	c, actualAmount, ok, isFinish, currentTx := w.getCall(m)
	if !ok {
		return isFinish, currentTx
	}

	for {
		processRound := (w.relayer.currentRelayer + uint64(m.DepositNonce)) % w.relayer.totalRelayers
		round, height := w.getRound()
		blockRound := round.blockRound.Uint64()
		if blockRound == processRound && !message.ok {
			fmt.Printf("Relayer#%v solve %v in block#%v\n", w.relayer.currentRelayer, round, height)
			// Try to find a exist MultiSignTx
			var maybeTimePoint interface{}
			maxWeight := types.Weight(0)

			// Traverse all of matched Tx, included New、Approve、Executed
			for _, ms := range w.listener.asMulti {
				// Validate parameter
				var destAddress string
				if m.Source == chainset.IdBSC || m.Source == chainset.IdKovan || m.Source == chainset.IdHeco{
					dest := types.NewAddressFromAccountID(m.Payload[1].([]byte)).AsAccountID
					destAddress = utils2.BytesToHex(dest[:])
				} else {
					destAddress = string(m.Payload[1].([]byte))[2:]
				}

				if ms.DestAddress == destAddress && ms.DestAmount == actualAmount.String() {
					/// Once MultiSign Extrinsic is executed, stop sending Extrinsic to Polkadot
					finished, executed := w.isFinish(ms, m)
					if finished {
						return finished, executed
					}

					/// Match the correct TimePoint
					height := types.U32(ms.OriginMsTx.Block)
					maybeTimePoint = TimePointSafe32{
						Height: types.NewOptionU32(height),
						Index:  types.U32(ms.OriginMsTx.TxId),
					}
					maxWeight = types.Weight(w.maxWeight)
					break
				} else {
					maybeTimePoint = []byte{}
				}
			}

			if len(w.listener.asMulti) == 0 {
				maybeTimePoint = []byte{}
			}

			if maxWeight == 0 {
				w.log.Info(TryToMakeNewMultiSigTx, "depositNonce", m.DepositNonce)
			} else {
				_, height := maybeTimePoint.(TimePointSafe32).Height.Unwrap()
				w.log.Info(TryToApproveMultiSigTx, "Block", height, "Index", maybeTimePoint.(TimePointSafe32).Index, "depositNonce", m.DepositNonce)
			}

			mc, err := types.NewCall(&w.conn.meta, string(utils.MultisigAsMulti), w.relayer.multiSignThreshold, w.relayer.otherSignatories, maybeTimePoint, EncodeCall(c), false, maxWeight)
			if err != nil {
				w.logErr(NewMultiCallError, err)
			}

			w.submitTx(mc)
			return false, NotExecuted
		} else {
			finished, executed := w.checkRedeem(m, actualAmount)
			if finished {
				return finished, executed
			} else {
				///Round over, wait a RoundInterval
				time.Sleep(RoundInterval)
			}
		}
	}
}

func (w *writer) getCall(m msg.Message) (types.Call, *big.Int, bool, bool, MultiSignTx){
	var c types.Call
	var assetId xevents.AssetId

	if m.Destination == chainset.IdChainXBTCV1 || m.Destination == chainset.IdChainXBTCV2 {
		assetId = chainset.AssetXBTC
	} else {
		assetId = chainset.OriginAsset
	}

	/// Get SendAmount
	sendAmount, err := w.bridgeCore.GetAmountToSub(m.Payload[0].([]byte), assetId)
	if err != nil {
		w.log.Error(RedeemNegAmountError, "Error", err)
		return types.Call{}, nil, false, true, UnKnownError
	}

	c, err = w.bridgeCore.MakeCrossChainTansferCall(m, &w.conn.meta, assetId)
	if err != nil {
		w.log.Error(NewCrossChainTransferCallError, "Error", err)
		return types.Call{}, nil, false, true, UnKnownError
	}
	return c, sendAmount, true, false, NotExecuted
}

func (w *writer) checkRedeem(m msg.Message, actualAmount *big.Int) (bool, MultiSignTx) {
	for _, ms := range w.listener.asMulti {
		// Validate parameter
		var destAddress string
		if m.Source == chainset.IdBSC || m.Source == chainset.IdKovan || m.Source == chainset.IdHeco {
			dest := types.NewAddressFromAccountID(m.Payload[1].([]byte)).AsAccountID
			destAddress = utils2.BytesToHex(dest[:])
		} else {
			destAddress = string(m.Payload[1].([]byte))[2:]
		}

		if ms.DestAddress == destAddress && ms.DestAmount == actualAmount.String() {
			/// Once MultiSign Extrinsic is executed, stop sending Extrinsic to Polkadot
			return w.isFinish(ms, m)
		}
	}
	return false, NotExecuted
}

func (w *writer) submitTx(c types.Call) {
	// BEGIN: Get the essential information first
	api := w.conn.api

	retryTimes := RedeemRetryLimit
	for {
		// No more retries, stop submitting Tx
		if retryTimes == 0 {
			w.log.Error("submit Tx failed, check it")
			break
		}

		meta, err := api.RPC.State.GetMetadataLatest()
		if err != nil {
			w.logErr(GetMetadataError, err)
			retryTimes--
			continue
		}

		genesisHash, err := api.RPC.Chain.GetBlockHash(genesisBlock)
		if err != nil {
			w.logErr(GetBlockHashError, err)
			retryTimes--
			continue
		}

		rv, err := api.RPC.State.GetRuntimeVersionLatest()
		if err != nil {
			w.logErr(GetRuntimeVersionLatestError, err)
			retryTimes--
			continue
		}

		key, err := types.CreateStorageKey(meta, "System", "Account", w.relayer.kr.PublicKey, nil)
		if err != nil {
			w.logErr(CreateStorageKeyError, err)
			retryTimes--
			continue
		}

		// Validate account and get account information
		var accountInfo types.AccountInfo
		ok, err := api.RPC.State.GetStorageLatest(key, &accountInfo)
		if err != nil || !ok {
			w.logErr(GetStorageLatestError, err)
			retryTimes--
			continue
		}

		// Extrinsic nonce
		nonce := uint32(accountInfo.Nonce)

		// Construct signature option
		o := types.SignatureOptions{
			BlockHash:          genesisHash,
			Era:                types.ExtrinsicEra{IsMortalEra: false},
			GenesisHash:        genesisHash,
			Nonce:              types.NewUCompactFromUInt(uint64(nonce)),
			SpecVersion:        rv.SpecVersion,
			Tip:                types.NewUCompactFromUInt(0),
			TransactionVersion: rv.TransactionVersion,
		}

		// Create and Sign the MultiSign
		ext := types.NewExtrinsic(c)

		/// ChainX V1 still use `Address` type
		if w.listener.chainId == chainset.IdChainXPCXV1 || w.listener.chainId == chainset.IdChainXBTCV1 {
			err = ext.Sign(w.relayer.kr, o)
		} else {
			err = ext.Sign(w.relayer.kr, o)
		}
		if err != nil {
			w.log.Error(SignMultiSignTxFailed, "Failed", err)
			break
		}

		// Transfer and track the actual status
		_, err = api.RPC.Author.SubmitAndWatchExtrinsic(ext)
		w.checkErr(SubmitExtrinsicFailed, err)
		break
	}
}

func (w *writer) getRound() (Round, uint64) {
	finalizedHash, err := w.listener.client.Api.RPC.Chain.GetFinalizedHead()
	if err != nil {
		w.listener.log.Error("Writer Failed to fetch finalized hash", "err", err)
	}

	// Get finalized block header
	finalizedHeader, err := w.listener.client.Api.RPC.Chain.GetHeader(finalizedHash)
	if err != nil {
		w.listener.log.Error("Failed to fetch finalized header", "err", err)
	}

	blockHeight := big.NewInt(int64(finalizedHeader.Number))
	blockRound := big.NewInt(0)
	blockRound.Mod(blockHeight, big.NewInt(int64(w.relayer.totalRelayers))).Uint64()

	round := Round{
		blockHeight: blockHeight,
		blockRound:  blockRound,
	}

	return round, blockHeight.Uint64()
}

func (w *writer) isFinish(ms MultiSigAsMulti, m msg.Message) (bool, MultiSignTx) {
	/// Check isExecuted
	if ms.Executed {
		return true, ms.OriginMsTx
	}

	/// Check isVoted
	/// if already voted, avoid sending duplicated Tx until being executed
	for _, others := range ms.Others {
		var isVote = true
		for _, signatory := range others {
			voter, _ := types.NewAddressFromHexAccountID(signatory)
			relayer := types.NewAddressFromAccountID(w.relayer.kr.PublicKey)
			if voter == relayer {
				isVote = false
			}
		}

		if isVote {
			w.log.Info("relayer has vote, wait others!", "DepositNonce", m.DepositNonce, "Relayer", w.relayer.currentRelayer, "Block", ms.OriginMsTx.Block, "Index", ms.OriginMsTx.TxId)
			return true, YesVoted
		}
	}

	return false, NotExecuted
}

func (w *writer) getApi() (*gsrpc.SubstrateAPI, error) {
	chainId := w.listener.chainId
	if chainId == chainset.IdChainXPCXV1 || chainId == chainset.IdChainXPCXV2 || chainId == chainset.IdChainXBTCV1 || chainId == chainset.IdChainXBTCV2 {
		api, err := gsrpc.NewSubstrateAPI(w.conn.url)
		if err != nil {
			w.logErr(NewApiError, err)
			return nil, err
		} else {
			return api, nil
		}
	} else {
		return w.conn.api, nil
	}
}

func (w *writer) checkErr(msg string, err error) {
	if err != nil {
		w.logErr(msg, err)
	}
}

func (w *writer) logErr (msg string, err error) {
	w.log.Error(msg, "Error", err, "chain", w.listener.name)
}

func (w *writer) logInfo (msg string, block int64) {
	w.log.Info(msg, "Block", block, "chain", w.listener.name)
}

func (w *writer) logCrossChainTx (tokenX string, tokenY string, amount *big.Int, fee *big.Int, actualAmount *big.Int) {
	message := tokenX + " to " + tokenY
	actualTitle := "Actual_" + tokenY + "Amount"
	w.log.Info(message,"Amount", amount, "Fee", fee, actualTitle, actualAmount)
}