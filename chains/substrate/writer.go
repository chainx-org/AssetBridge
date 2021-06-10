// Copyright 2021 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"fmt"
	"github.com/ChainSafe/log15"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/chainx-org/AssetBridge/chains/chainset"
	utils "github.com/chainx-org/AssetBridge/shared/substrate"
	"github.com/rjman-ljm/sherpax-utils/core"
	metrics "github.com/rjman-ljm/sherpax-utils/metrics/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
	"github.com/rjman-ljm/substrate-go/expand/chainx/xevents"
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
	messages   map[Dest]bool
	chainCore  *chainset.ChainCore
}

func NewWriter(conn *Connection, listener *listener, log log15.Logger, sysErr chan<- error,
	m *metrics.ChainMetrics, extendCall bool, relayer Relayer, bc *chainset.ChainCore) *writer {
	return &writer{
		conn:       conn,
		listener:   listener,
		log:        log,
		sysErr:     sysErr,
		metrics:    m,
		extendCall: extendCall,
		relayer:    relayer,
		messages:   make(map[Dest]bool, InitCapacity),
		chainCore:  bc,
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
		w.log.Info("Start Issue...", "DepositNonce", m.DepositNonce)
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

			assetId , err := w.chainCore.ConvertResourceIdToAssetId(msg.ResourceId(prop.resourceId))
			if err != nil {
				w.logErr("rId to assetId err", err)
			}
			w.log.Info(LineLog, "DepositNonce", m.DepositNonce)
			w.log.Info("End Issue, acknowledging proposal on chain", "nonce", prop.depositNonce, "source", prop.sourceId, "AssetId", assetId)
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

func (w *writer) redeemTx(message *MsgStatus) (RedeemStatusCode, multiSigTx) {
	//w.UpdateMetadata()
	m := message.m
	types.SetSerDeOptions(types.SerDeOptions{NoPalletIndices: true})

	defer func() {
		/// Single thread send one time each round
		time.Sleep(RoundInterval)
	}()

	/// BEGIN: Create a call of multiSigTransfer
	c, err := w.getCall(m)
	if err != nil {
		w.logErr("Get call err", err)
		return UnKnownError, multiSigTx{}
	}

	actualAmount, err := w.chainCore.GetAmountToSub(m.Payload[0].([]byte), w.getRedeemAssetId(m))
	if err != nil {
		w.log.Error(RedeemNegAmountError, "Error", err)
		return UnKnownError, multiSigTx{}
	}

	for {
		processRound := (w.relayer.relayerId + uint64(m.DepositNonce)) % w.relayer.totalRelayers
		round, height := w.getRound()
		blockRound := round.blockRound.Uint64()
		if blockRound == processRound && !message.ok {
			fmt.Printf("Relayer#%v solve %v in block#%v\n", w.relayer.relayerId, round, height)
			// Try to find a exist multiSigTx
			var maybeTimePoint interface{}
			maxWeight := types.Weight(0)

			// Traverse all of matched Tx, included New、Approve、Executed
			for _, ms := range w.listener.asMulti {
				// Validate parameter
				fmt.Printf("verify msg.Address and ms.DestAddress: %v\n", w.verifyRedeemAddress(m.Payload[1].([]byte), ms.DestAddress))
				//fmt.Printf("m.Payload[1].([]byte) is %v\nms.DestAddress is %v\n", m.Payload[1].([]byte), ms.DestAddress)
				if w.verifyRedeemAddress(m.Payload[1].([]byte), ms.DestAddress) && ms.DestAmount == actualAmount.String() {
					/// Once multiSig Extrinsic is executed, stop sending Extrinsic to Polkadot
					status, executed := w.checkRedeemStatus(ms, m)
					if status.finished() {
						return status, executed
					}

					/// Match the correct TimePoint
					maybeTimePoint = TimePointSafe32{
						Height: types.NewOptionU32(types.U32(ms.OriginMsTx.Block)),
						Index:  types.U32(ms.OriginMsTx.TxId),
					}
					maxWeight = types.Weight(w.relayer.maxWeight)
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

			mc, err := types.NewCall(
				&w.conn.meta,
				string(utils.MultisigAsMulti),
				w.relayer.multiSigThreshold,
				w.relayer.otherSignatories,
				maybeTimePoint,
				EncodeCall(c),
				false,
				maxWeight,
			)
			if err != nil {
				w.logErr(NewMultiCallError, err)
				return UnKnownError, multiSigTx{}
			}

			w.submitTx(mc)
			return NotExecuted, multiSigTx{}
		} else {
			if message.ok {
				fmt.Printf("%v hasVote, check tx status\n", w.relayer.relayerId)
			}

			status, executed := w.checkRedeem(m, actualAmount)
			if status.finished() {
				return status, executed
			} else {
				///Round over, wait a RoundInterval
				time.Sleep(RoundInterval)
			}
		}
	}
}

func (w *writer) getRedeemAssetId(m msg.Message) xevents.AssetId {
	var assetId xevents.AssetId
	/// GetResourceId <- AssetId
	if m.Destination == chainset.IdChainXBTCV1 || m.Destination == chainset.IdChainXBTCV2 {
		assetId = chainset.AssetXBTC
	} else {
		assetId = chainset.OriginAsset
	}
	return assetId
}

func (w *writer) getSendToSubAmount(m msg.Message, assetId xevents.AssetId) (*big.Int, error) {
	sendAmount, err := w.chainCore.GetAmountToSub(m.Payload[0].([]byte), assetId)
	if err != nil {
		return nil, fmt.Errorf("eth2sub redeem a neg amount")
	}

	return sendAmount, nil
}
func (w *writer) ConvertToEthMessage(m msg.Message) (msg.Message, error) {
	originAmount := big.NewInt(0).SetBytes(m.Payload[0].([]byte))
	m.Payload[0] = big.NewInt(0).Mul(originAmount, big.NewInt(chainset.DiffXAsset)).Bytes()

	return m, nil
}

func (w *writer) getCall(m msg.Message) (types.Call, error) {
	c, err := w.chainCore.MakeCrossChainTansferCall(m, &w.conn.meta, w.getRedeemAssetId(m))
	if err != nil {
		w.log.Error(NewCrossChainTransferCallError, "Error", err)
		return types.Call{}, err
	}
	return c, nil
}

func (w *writer) checkRedeem(m msg.Message, actualAmount *big.Int) (RedeemStatusCode, multiSigTx) {
	for _, ms := range w.listener.asMulti {
		// Validate parameter
		//var destAddress string
		//if m.Source == chainset.IdBSC || m.Source == chainset.IdKovan || m.Source == chainset.IdHeco {
		//	dest := types.NewAddressFromAccountID(m.Payload[1].([]byte)).AsAccountID
		//	destAddress = utils2.BytesToHex(dest[:])
		//} else {
		//	destAddress = string(m.Payload[1].([]byte))[2:]
		//}

		if w.verifyRedeemAddress(m.Payload[1].([]byte), ms.DestAddress) && ms.DestAmount == actualAmount.String() {
			/// Once multiSig Extrinsic is executed, stop sending Extrinsic to Polkadot
			return w.checkRedeemStatus(ms, m)
		}
	}
	return NotExecuted, multiSigTx{}
}

func (w *writer) verifyRedeemAddress(msgAddress []byte, msAddress string) bool {
	destAddress := types.NewMultiAddressFromAccountID(msgAddress)
	sendAddress, err := types.NewMultiAddressFromHexAccountID("0x"+msAddress)
	if err != nil {
		fmt.Printf("parse ms.DestAddress err")
		return false
	}
	return destAddress.AsID == sendAddress.AsID
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

		// Create and Sign the multiSig
		ext := types.NewExtrinsic(c)

		/// ChainX V1 still use `Address` type
		if w.listener.chainId == chainset.IdChainXPCXV1 || w.listener.chainId == chainset.IdChainXBTCV1 {
			err = ext.Sign(w.relayer.kr, o)
		} else {
			err = ext.Sign(w.relayer.kr, o)
		}
		if err != nil {
			w.log.Error(SignmultiSigTxFailed, "Failed", err)
			break
		}

		// Transfer and track the actual status
		_, err = api.RPC.Author.SubmitAndWatchExtrinsic(ext)
		w.checkErr(SubmitExtrinsicFailed, err)
		break
	}
}

func (w *writer) getRound() (Round, uint64) {
	finalizedHash, err := w.listener.conn.cli.Api.RPC.Chain.GetFinalizedHead()
	if err != nil {
		w.listener.log.Error("Writer Failed to fetch finalized hash", "err", err)
	}

	// Get finalized block header
	finalizedHeader, err := w.listener.conn.cli.Api.RPC.Chain.GetHeader(finalizedHash)
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

func (w *writer) checkRedeemStatus(ms MultiSigAsMulti, m msg.Message) (RedeemStatusCode, multiSigTx) {
	/// Check isExecuted
	if ms.Executed {
		return IsExecuted, ms.OriginMsTx
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
			w.log.Info("relayer has vote, wait others!", "DepositNonce", m.DepositNonce, "Relayer", w.relayer.relayerId, "Block", ms.OriginMsTx.Block, "Index", ms.OriginMsTx.TxId)
			return YesVoted, multiSigTx{}
		}
	}

	return NotExecuted, multiSigTx{}
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