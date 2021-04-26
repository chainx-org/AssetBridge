// Copyright 2021 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/config"
	utils "github.com/Rjman-self/BBridge/shared/substrate"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v2"
	"github.com/centrifuge/go-substrate-rpc-client/v2/types"
	"github.com/rjman-self/platdot-utils/core"
	metrics "github.com/rjman-self/platdot-utils/metrics/types"
	"github.com/rjman-self/platdot-utils/msg"
	utils2 "github.com/rjman-self/substrate-go/utils"
	"math/big"
	"sync"
	"time"
)

var _ core.Writer = &writer{}

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
	w.checkRepeat(m)

	if m.Destination != w.listener.chainId {
		w.log.Info("Not Mine", "msg.DestId", m.Destination, "w.l.chainId", w.listener.chainId)
		return false
	}
	w.log.Info(StartATx, "DepositNonce", m.DepositNonce, "From", m.Source, "To", m.Destination)

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
			w.log.Info(RelayerFinishTheTx,"Relayer", w.relayer.currentRelayer, "DepositNonce", m.DepositNonce, "CostTime", cost)
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

				/// If currentTx is Vote
				if currentTx == YesVoted {
					//fmt.Printf("I have Vote, wait executing\n")
					time.Sleep(RoundInterval * time.Duration(w.relayer.totalRelayers) / 2)
				}
				/// if currentTx is Executed or UnKnownError
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

func (w *writer) getCall(m msg.Message) (types.Call, *big.Int, bool, bool, MultiSignTx){
	var c types.Call
	actualAmount := big.NewInt(0)
	amount := big.NewInt(0).SetBytes(m.Payload[0].([]byte))
	multiAddressRecipient := types.NewMultiAddressFromAccountID(m.Payload[1].([]byte))
	addressRecipient := types.NewAddressFromAccountID(m.Payload[1].([]byte))

	// Get parameters of Call
	if m.Destination == config.Polkadot {
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
		w.checkErr(NewBalancesTransferKeepAliveCallError, err)
	} else if m.Destination == config.Kusama {
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
		w.checkErr(NewBalancesTransferKeepAliveCallError, err)
	} else if m.Destination == config.ChainXBTC {
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
		w.checkErr(NewXAssetsTransferCallError, err)
	} else if m.Destination == config.ChainXPCX {
		/// Convert BPCX amount to PCX amount
		actualAmount.Div(amount, big.NewInt(onePCX))

		w.logCrossChainTx("BPCX", "PCX", actualAmount, big.NewInt(0), actualAmount)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.logErr(RedeemNegAmountError, nil)
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
		w.checkErr(NewBalancesTransferCallError, err)
	} else {
		/// Other Chain
		w.log.Error("chainId set wrong", "ChainId", w.listener.chainId)
		return types.Call{}, nil, false, true, UnKnownError
	}
	return c, actualAmount, true, false, NotExecuted
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
		round := w.getRound()
		if round.blockRound.Uint64() == processRound {
			// Try to find a exist MultiSignTx
			var maybeTimePoint interface{}
			maxWeight := types.Weight(0)

			// Traverse all of matched Tx, included New、Approve、Executed
			for _, ms := range w.listener.msTxAsMulti {
				// Validate parameter
				dest := types.NewAddressFromAccountID(m.Payload[1].([]byte)).AsAccountID
				if ms.DestAddress == utils2.BytesToHex(dest[:]) && ms.DestAmount == actualAmount.String() {
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
			///Round over, wait a RoundInterval
			time.Sleep(RoundInterval)
		}
	}
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

		if w.listener.chainId == config.ChainXBTC || w.listener.chainId == config.ChainXPCX {
			err = ext.Sign(w.relayer.kr, o)
		} else {
			err = ext.MultiSign(w.relayer.kr, o)
		}
		if err != nil {
			w.logErr(SignMultiSignTxError, err)
			break
		}

		// Transfer and track the actual status
		_, err = api.RPC.Author.SubmitAndWatchExtrinsic(ext)
		w.checkErr(SubmitExtrinsicFailed, err)
		break
	}
}

func (w *writer) getRound() Round {
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

	return round
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
	if w.listener.chainId == config.ChainXPCX || w.listener.chainId == config.ChainXBTC {
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