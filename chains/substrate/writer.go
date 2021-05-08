// Copyright 2021 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/config"
	utils "github.com/Rjman-self/BBridge/shared/substrate"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/sherpax-utils/core"
	metrics "github.com/rjman-self/sherpax-utils/metrics/types"
	"github.com/rjman-self/sherpax-utils/msg"
	utils2 "github.com/rjman-self/substrate-go/utils"
	"math/big"
	"sync"
	"time"
)

var _ core.Writer = &writer{}
var AcknowledgeProposal utils.Method = utils.BridgePalletName + ".acknowledge_proposal"
var TerminatedError = errors.New("terminated")

const genesisBlock = 0
const RoundInterval = time.Second * 6
const xParameter uint8 = 255

const(
	oneKSM = 1000000      /// KSM is 12 digits
	oneDOT = 100000000    /// DOT is 10 digits
	oneXBTC = 10000000000 /// XBTC is 8 digits
	onePCX = 10000000000 /// XBTC is 8 digits
)

type writer struct {
	meta       *types.Metadata
	conn       *Connection
	listener   *listener
	log        log15.Logger
	sysErr     chan<- error
	metrics    *metrics.ChainMetrics
	extendCall bool // Extend extrinsic calls to substrate with ResourceID.Used for backward compatibility with example pallet.
	msApi      *gsrpc.SubstrateAPI
	relayer    Relayer
	maxWeight  uint64
	messages   map[Dest]bool
}

func NewWriter(conn *Connection, listener *listener, log log15.Logger, sysErr chan<- error,
	m *metrics.ChainMetrics, extendCall bool, weight uint64, relayer Relayer) *writer {

	msApi, err := gsrpc.NewSubstrateAPI(conn.url)
	if err != nil {
		log15.Error("New Substrate API err", "err", err)
	}

	meta, err := msApi.RPC.State.GetMetadataLatest()
	if err != nil {
		log15.Error("GetMetadataLatest err", "err", err)
	}

	return &writer{
		meta:       meta,
		conn:       conn,
		listener:   listener,
		log:        log,
		sysErr:     sysErr,
		metrics:    m,
		extendCall: extendCall,
		msApi:      msApi,
		relayer:    relayer,
		maxWeight:  weight,
		messages:   make(map[Dest]bool, InitCapacity),
	}
}
func (w *writer) ResolveMessage(m msg.Message) bool {
	var prop *proposal
	var err error

	// Construct the Transaction
	switch m.Type {
	case msg.NativeTransfer:
		w.createNativeTx(m)
		return true
	case msg.FungibleTransfer:
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
			w.log.Info("Acknowledging proposal on chain", "nonce", prop.depositNonce, "source", prop.sourceId, "resource", fmt.Sprintf("%x", prop.resourceId), "method", prop.method)

			err = w.conn.SubmitTx(AcknowledgeProposal, prop.depositNonce, prop.sourceId, prop.resourceId, prop.call)
			if err != nil && err.Error() == TerminatedError.Error() {
				return false
			} else if err != nil {
				w.log.Error("Failed to execute extrinsic", "err", err)
				time.Sleep(BlockRetryInterval)
				continue
			}
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

func (w *writer) redeemTx(m msg.Message) (bool, MultiSignTx) {
	w.UpdateMetadata()
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
		if blockRound == processRound {
			fmt.Printf("Relayer#%v solve %v in block#%v\n", w.relayer.currentRelayer, round, height)
			// Try to find a exist MultiSignTx
			var maybeTimePoint interface{}
			maxWeight := types.Weight(0)

			// Traverse all of matched Tx, included New、Approve、Executed
			for _, ms := range w.listener.msTxAsMulti {
				// Validate parameter
				var destAddress string
				if m.Source == config.IdBSC {
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
					height := types.U32(ms.OriginMsTx.BlockNumber)
					maybeTimePoint = TimePointSafe32{
						Height: types.NewOptionU32(height),
						Index:  types.U32(ms.OriginMsTx.MultiSignTxId),
					}
					maxWeight = types.Weight(w.maxWeight)
					break
				} else {
					maybeTimePoint = []byte{}
				}
			}

			if len(w.listener.msTxAsMulti) == 0 {
				maybeTimePoint = []byte{}
			}

			if maxWeight == 0 {
				w.log.Info(TryToMakeNewMultiSigTx, "depositNonce", m.DepositNonce)
			} else {
				_, height := maybeTimePoint.(TimePointSafe32).Height.Unwrap()
				w.log.Info(TryToApproveMultiSigTx, "Block", height, "Index", maybeTimePoint.(TimePointSafe32).Index, "depositNonce", m.DepositNonce)
			}

			mc, err := types.NewCall(w.meta, string(utils.MultisigAsMulti), w.relayer.multiSignThreshold, w.relayer.otherSignatories, maybeTimePoint, EncodeCall(c), false, maxWeight)

			if err != nil {
				w.logErr(NewMultiCallError, err)
			}
			///END: Create a call of MultiSignTransfer

			///BEGIN: Submit a MultiSignExtrinsic to Polkadot
			w.submitTx(mc)
			return false, NotExecuted
			///END: Submit a MultiSignExtrinsic to Polkadot
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
	actualAmount := big.NewInt(0)
	amount := big.NewInt(0).SetBytes(m.Payload[0].([]byte))

	var multiAddressRecipient types.MultiAddress
	var addressRecipient types.Address

	if m.Source == config.IdBSC {
		multiAddressRecipient = types.NewMultiAddressFromAccountID(m.Payload[1].([]byte))
		addressRecipient = types.NewAddressFromAccountID(m.Payload[1].([]byte))
	} else {
		multiAddressRecipient, _ = types.NewMultiAddressFromHexAccountID(string(m.Payload[1].([]byte)))
		addressRecipient, _ = types.NewAddressFromHexAccountID(string(m.Payload[1].([]byte)))
	}
	// Get parameters of Call
	if m.Destination == config.IdPolkadot {
		receiveAmount := big.NewInt(0).Div(amount, big.NewInt(oneDOT))
		/// calculate fee and actualAmount
		fixedFee := big.NewInt(FixedDOTFee)
		additionalFee := big.NewInt(0).Div(receiveAmount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)
		actualAmount.Sub(receiveAmount, fee)
		w.logCrossChainTx("BDOT", "DOT", receiveAmount, fee, actualAmount)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.log.Error(RedeemNegAmountError, "Amount", actualAmount)
			return types.Call{}, nil, false, true, UnKnownError
		}
		sendAmount := types.NewUCompact(actualAmount)

		/// Create a transfer_keep_alive call
		var err error
		c, err = types.NewCall(
			w.meta,
			string(utils.BalancesTransferKeepAliveMethod),
			multiAddressRecipient,
			sendAmount,
		)
		if err != nil {
			w.log.Error(NewBalancesTransferKeepAliveCallError, "Error", err)
			return types.Call{}, nil, false, true, UnKnownError
		}
	} else if m.Destination == config.IdKusama {
		// Convert BKSM amount to KSM amount
		receiveAmount := big.NewInt(0).Div(amount, big.NewInt(oneKSM))

		/// calculate fee and actualAmount
		fixedFee := big.NewInt(FixedKSMFee)
		additionalFee := big.NewInt(0).Div(receiveAmount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)
		actualAmount.Sub(receiveAmount, fee)

		w.logCrossChainTx("BKSM", "KSM", receiveAmount, fee, actualAmount)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.logErr(RedeemNegAmountError, nil)
			return types.Call{}, nil, false, true, UnKnownError
		}
		sendAmount := types.NewUCompact(actualAmount)

		/// Create a transfer_keep_alive call
		var err error
		c, err = types.NewCall(
			w.meta,
			string(utils.BalancesTransferKeepAliveMethod),
			multiAddressRecipient,
			sendAmount,
		)
		if err != nil {
			w.log.Error(NewBalancesTransferKeepAliveCallError, "Error", err)
			return types.Call{}, nil, false, true, UnKnownError
		}
	} else if m.Destination == config.IdChainXBTCV1 {
		/// Convert BBTC amount to XBTC amount
		actualAmount.Div(amount, big.NewInt(oneXBTC))
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.logErr(RedeemNegAmountError, nil)
			return types.Call{}, nil, false, true, UnKnownError
		}

		w.logCrossChainTx("BBTC", "XBTC", actualAmount, big.NewInt(0), actualAmount)
		sendAmount := types.NewUCompact(actualAmount)

		// Create a XAssets.Transfer call
		assetId := types.NewUCompactFromUInt(uint64(XBTC))
		var err error

		c, err = types.NewCall(
			w.meta,
			string(utils.XAssetsTransferMethod),
			xParameter,
			/// ChainX XBTC2.0 Address
			//multiAddressRecipient,
			/// ChainX XBTC1.0 Address
			addressRecipient,
			assetId,
			sendAmount,
		)
		if err != nil {
			w.log.Error(NewXAssetsTransferCallError, "Error", err)
			return types.Call{}, nil, false, true, UnKnownError
		}
	} else if m.Destination == config.IdChainXBTCV2 {
		/// Convert BBTC amount to XBTC amount
		actualAmount.Div(amount, big.NewInt(oneXBTC))
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.logErr(RedeemNegAmountError, nil)
			return types.Call{}, nil, false, true, UnKnownError
		}

		w.logCrossChainTx("BBTC", "XBTC", actualAmount, big.NewInt(0), actualAmount)
		sendAmount := types.NewUCompact(actualAmount)

		// Create a XAssets.Transfer call
		assetId := types.NewUCompactFromUInt(uint64(XBTC))
		var err error

		c, err = types.NewCall(
			w.meta,
			string(utils.XAssetsTransferMethod),
			/// ChainX XBTC2.0 Address
			//multiAddressRecipient,
			/// ChainX XBTC1.0 Address
			multiAddressRecipient,
			assetId,
			sendAmount,
		)
		if err != nil {
			w.log.Error(NewXAssetsTransferCallError, "Error", err)
			return types.Call{}, nil, false, true, UnKnownError
		}
	} else if m.Destination == config.IdChainXPCXV1 {
		/// Convert BPCX amount to PCX amount
		receiveAmount := big.NewInt(0).Div(amount, big.NewInt(onePCX))
		/// calculate fee and actualAmount
		fixedFee := big.NewInt(FixedPCXFee)
		additionalFee := big.NewInt(0).Div(receiveAmount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)
		actualAmount.Sub(receiveAmount, fee)
		w.logCrossChainTx("BPCX", "PCX", receiveAmount, fee, actualAmount)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.log.Error(RedeemNegAmountError, "Amount", actualAmount)
			return types.Call{}, nil, false, true, UnKnownError
		}
		sendAmount := types.NewUCompact(actualAmount)

		// Create a XAssets.Transfer call
		var err error
		c, err = types.NewCall(
			w.meta,
			string(utils.BalancesTransferKeepAliveMethod),
			xParameter,
			addressRecipient,
			sendAmount,
		)
		if err != nil {
			w.log.Error(NewBalancesTransferKeepAliveCallError, "Error", err)
			return types.Call{}, nil, false, true, UnKnownError
		}
	} else if m.Destination == config.IdChainXPCXV2 {
		/// Convert BPCX amount to PCX amount
		receiveAmount := big.NewInt(0).Div(amount, big.NewInt(onePCX))
		/// calculate fee and actualAmount
		fixedFee := big.NewInt(FixedPCXFee)
		additionalFee := big.NewInt(0).Div(receiveAmount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)
		actualAmount.Sub(receiveAmount, fee)
		w.logCrossChainTx("BPCX", "PCX", receiveAmount, fee, actualAmount)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.log.Error(RedeemNegAmountError, "Amount", actualAmount)
			return types.Call{}, nil, false, true, UnKnownError
		}
		sendAmount := types.NewUCompact(actualAmount)

		// Create a XAssets.Transfer call
		var err error
		c, err = types.NewCall(
			w.meta,
			string(utils.BalancesTransferKeepAliveMethod),
			multiAddressRecipient,
			sendAmount,
		)
		if err != nil {
			w.log.Error(NewBalancesTransferKeepAliveCallError, "Error", err)
			return types.Call{}, nil, false, true, UnKnownError
		}
	} else {
		/// Other Chain
		w.log.Error("chainId set wrong", "ChainId", w.listener.chainId)
		return types.Call{}, nil, false, true, UnKnownError
	}
	return c, actualAmount, true, false, NotExecuted
}

func (w *writer) checkRedeem(m msg.Message, actualAmount *big.Int) (bool, MultiSignTx) {
	for _, ms := range w.listener.msTxAsMulti {
		// Validate parameter
		var destAddress string
		if m.Source == config.IdBSC {
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
	api, err := w.getApi()
	w.checkErr("SubmitTx get api error", err)

	retryTimes := RedeemRetryLimit
	for {
		// No more retries, stop submitting Tx
		if retryTimes == 0 {
			w.log.Error("submit Tx failed, check it")
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
		if w.listener.chainId == config.IdChainXPCXV1 || w.listener.chainId == config.IdChainXBTCV1 {
			err = ext.Sign(w.relayer.kr, o)
		} else {
			err = ext.MultiSign(w.relayer.kr, o)
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
			w.log.Info("relayer has vote, wait others!", "DepositNonce", m.DepositNonce, "Relayer", w.relayer.currentRelayer, "Block", ms.OriginMsTx.BlockNumber, "Index", ms.OriginMsTx.MultiSignTxId)
			return true, YesVoted
		}
	}

	return false, NotExecuted
}

func (w *writer) UpdateMetadata() {
	meta, _ := w.msApi.RPC.State.GetMetadataLatest()
	if meta != nil {
		w.meta = meta
	}
}

func (w *writer) getApi() (*gsrpc.SubstrateAPI, error) {
	chainId := w.listener.chainId
	if chainId == config.IdChainXPCXV1 || chainId == config.IdChainXPCXV2 || chainId == config.IdChainXBTCV1 || chainId == config.IdChainXBTCV2 {
		api, err := gsrpc.NewSubstrateAPI(w.conn.url)
		if err != nil {
			w.logErr(NewApiError, err)
			return nil, err
		} else {
			return api, nil
		}
	} else {
		return w.msApi, nil
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