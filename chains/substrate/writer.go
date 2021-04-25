// Copyright 2021 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"fmt"
	"github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/config"
	utils "github.com/Rjman-self/BBridge/shared/substrate"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v2"
	"github.com/centrifuge/go-substrate-rpc-client/v2/rpc/author"
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

const RoundInterval = time.Second * 6
const RedeemRetryTimes = 100
const oneToken = 1000000	/// 12 digits
const oneDToken = 100000000 /// 10 digits
const oneXToken = 10000000000 /// 8 digits

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
	w.log.Info("Start a redeemTx...", "DepositNonce", m.DepositNonce, "From", m.Source, "To", m.Destination)

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
			fmt.Printf("Relayer #%v finish depositNonce %v cost %v\n", w.relayer.currentRelayer, m.DepositNonce, cost)
		}()
		retryTimes := BlockRetryLimit
		for {
			retryTimes--
			// No more retries, stop RedeemTx
			if retryTimes < BlockRetryLimit / 2 {
				w.log.Warn("There may be a problem with the deal", "RetryTimes", retryTimes)
			}
			if retryTimes == 0 {
				w.log.Error("Redeem Tx failed, try too many times\n")
				break
			}
			isFinished, currentTx := w.redeemTx(m)
			if isFinished {
				var mutex sync.Mutex
				mutex.Lock()

				/// If currentTx is AmountError
				if currentTx == AmountError {
					w.log.Error("MultiSig extrinsic Error! check Amount.", "DepositNonce", m.DepositNonce)

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
				/// if currentTx is Executed or AmountError
				if currentTx != YesVoted && currentTx != NotExecuted && currentTx != AmountError {
					w.log.Info("MultiSig extrinsic executed!", "DepositNonce", m.DepositNonce, "OriginBlock", currentTx.BlockNumber)

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
		w.log.Info("finish a redeemTx", "DepositNonce", m.DepositNonce)
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
			w.log.Info("Meet a Repeat Transaction", "DepositNonce", m.DepositNonce, "Waiting", repeatTime)
			time.Sleep(repeatTime)
		} else {
			break
		}
	}
	return true
}

func (w *writer) redeemTx(m msg.Message) (bool, MultiSignTx) {
	w.UpdateMetadate()
	types.SetSerDeOptions(types.SerDeOptions{NoPalletIndices: true})

	defer func() {
		/// Single thread send one time each round
		time.Sleep(RoundInterval)
	}()

	/// Parameters
	method := string(utils.BalancesTransferKeepAliveMethod)
	xMethod := string(utils.XAssetsTransferMethod)
	mulMethod := string(utils.MultisigAsMulti)

	var c types.Call
	/// BEGIN: Create a call of MultiSignTransfer

	amount := big.NewInt(0).SetBytes(m.Payload[0].([]byte))
	dest := types.NewAddressFromAccountID(m.Payload[1].([]byte)).AsAccountID
	destAddress := utils2.BytesToHex(dest[:])
	multiAddressRecipient := types.NewMultiAddressFromAccountID(m.Payload[1].([]byte))
	addressRecipient := types.NewAddressFromAccountID(m.Payload[1].([]byte))

	actualAmount := big.NewInt(0)

	// Get parameters of Balances.Transfer Call
	if m.Destination == config.Polkadot {
		// Convert BDOT amount to DOT amount
		receiveAmount := big.NewInt(0).Div(amount, big.NewInt(oneDToken))

		/// calculate fee and actualAmount
		fixedFee := big.NewInt(FixedDOTFee)
		additionalFee := big.NewInt(0).Div(receiveAmount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)
		actualAmount.Sub(receiveAmount, fee)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			fmt.Printf("AKSM to KSM, Amount is %v, Fee is %v, Actual_KSM_Amount = %v\n", receiveAmount, fee, actualAmount)
			w.log.Error("Redeem a neg amount", "Amount", actualAmount)
			return true, AmountError
		}
		fmt.Printf("BDOT to DOT, Amount is %v, Fee is %v, Actual_DOT_Amount = %v\n", receiveAmount, fee, actualAmount)
		sendAmount := types.NewUCompact(actualAmount)

		/// Create a transfer_keep_alive call
		var err error
		c, err = types.NewCall(
			w.meta,
			method,
			multiAddressRecipient,
			sendAmount,
		)
		if err != nil {
			w.log.Error("New Balances.Transfer_Keep_Alive Call err", "err", err)
		}
	} else if m.Destination == config.Kusama {
		// Convert AKSM amount to KSM amount
		receiveAmount := big.NewInt(0).Div(amount, big.NewInt(oneToken))

		/// calculate fee and actualAmount
		fixedFee := big.NewInt(FixedKSMFee)
		additionalFee := big.NewInt(0).Div(receiveAmount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)
		actualAmount.Sub(receiveAmount, fee)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			fmt.Printf("AKSM to KSM, Amount is %v, Fee is %v, Actual_KSM_Amount = %v\n", receiveAmount, fee, actualAmount)
			w.log.Error("Redeem a neg amount", "Amount", actualAmount)
			return true, AmountError
		}
		fmt.Printf("AKSM to KSM, Amount is %v, Fee is %v, Actual_KSM_Amount = %v\n", receiveAmount, fee, actualAmount)
		sendAmount := types.NewUCompact(actualAmount)

		/// Create a transfer_keep_alive call
		var err error
		c, err = types.NewCall(
			w.meta,
			method,
			multiAddressRecipient,
			sendAmount,
		)
		if err != nil {
			w.log.Error("New Balances.Transfer_Keep_Alive Call err", "err", err)
		}
	} else if m.Destination == config.ChainX {
		/// Convert BBTC amount to XBTC amount
		actualAmount.Div(amount, big.NewInt(oneXToken))

		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			w.log.Error("Redeem a neg amount", "Amount", actualAmount)
			return true, AmountError
		}

		fmt.Printf("BBTC to XBTC, Amount is %v, Fee is %v, Actual_XBTC_Amount = %v\n", actualAmount, 0, actualAmount)

		sendAmount := types.NewUCompact(actualAmount)

		// Create a XAssets.Transfer call
		assetId := types.NewUCompactFromUInt(1)
		var err error

		c, err = types.NewCall(
			w.meta,
			xMethod,
			/// ChainX XBTC2.0 Address
			//multiAddressRecipient,
			/// ChainX XBTC1.0 Address
			uint8(255),
			addressRecipient,
			assetId,
			sendAmount,
		)
		if err != nil {
			w.log.Error("New XAssets.Transfer err", "err", err)
		}
	} else {
		/// Other Chain
	}

	for {
		processRound := (w.relayer.currentRelayer + uint64(m.DepositNonce)) % w.relayer.totalRelayers
		round := w.getRound()
		if round.blockRound.Uint64() == processRound {
			//fmt.Printf("process the message in block #%v, round #%v, depositnonce is %v\n", round.blockHeight, processRound, m.DepositNonce)
			// Try to find a exist MultiSignTx
			var maybeTimePoint interface{}
			maxWeight := types.Weight(0)

			// Traverse all of matched Tx, included New、Approve、Executed

			for _, ms := range w.listener.msTxAsMulti {
				// Validate parameter
				if ms.DestAddress == destAddress && ms.DestAmount == actualAmount.String() {
					/// Once MultiSign Extrinsic is executed, stop sending Extrinsic to Polkadot
					finished, executed := w.isFinish(ms)
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
				w.log.Info("Try to make a New MultiSign Tx!", "depositNonce", m.DepositNonce)
			} else {
				_, height := maybeTimePoint.(TimePointSafe32).Height.Unwrap()
				w.log.Info("Try to Approve a MultiSignTx!", "Block", height, "Index", maybeTimePoint.(TimePointSafe32).Index, "depositNonce", m.DepositNonce)
			}

			mc, err := types.NewCall(w.meta, mulMethod, w.relayer.multiSignThreshold, w.relayer.otherSignatories, maybeTimePoint, EncodeCall(c), false, maxWeight)

			if err != nil {
				w.log.Error("New MultiCall err", "err", err)
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
	var api *gsrpc.SubstrateAPI
	var err error

	switch w.listener.chainId {
	case config.ChainX:
		api, err = gsrpc.NewSubstrateAPI(w.conn.url)
		if err != nil {
			fmt.Printf("New api error: %v\n", err)
		}
	case config.Polkadot:
		api = w.msApi
	case config.Kusama:
		api = w.msApi
	default:
		api = w.msApi
	}

	retryTimes := BlockRetryLimit
	for {
		// No more retries, stop submitting Tx
		if retryTimes == 0 {
			fmt.Printf("submit Tx failed, check it\n")
		}

		meta, err := api.RPC.State.GetMetadataLatest()
		if err != nil {
			fmt.Printf("Get Metadata Latest err\n")
			retryTimes--
			continue
		}
		genesisHash, err := api.RPC.Chain.GetBlockHash(0)
		if err != nil {
			fmt.Printf("GetBlockHash err\n")
			retryTimes--
			continue
		}
		rv, err := api.RPC.State.GetRuntimeVersionLatest()
		if err != nil {
			fmt.Printf("GetRuntimeVersionLatest err\n")
			retryTimes--
			continue
		}

		key, err := types.CreateStorageKey(meta, "System", "Account", w.relayer.kr.PublicKey, nil)
		if err != nil {
			fmt.Printf("CreateStorageKey err\n")
			retryTimes--
			continue
		}
		// END: Get the essential information

		// Validate account and get account information
		var accountInfo types.AccountInfo
		ok, err := api.RPC.State.GetStorageLatest(key, &accountInfo)
		if err != nil || !ok {
			fmt.Printf("GetStorageLatest err\n")
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

		switch w.listener.chainId {
		case config.Kusama:
			err = ext.MultiSign(w.relayer.kr, o)
		case config.Polkadot:
			err = ext.MultiSign(w.relayer.kr, o)
		case config.ChainX:
			/// ChainX XBTC2.0 MultiAddress
			//err = ext.MultiSign(w.relayer.kr, o)
			/// ChainX XBTC1.0 Address
			err = ext.Sign(w.relayer.kr, o)
		default:
			err = ext.MultiSign(w.relayer.kr, o)
		}
		if err != nil {
			w.log.Error("MultiTx Sign err\n")
		}

		// Do the transfer and track the actual status
		_, err = api.RPC.Author.SubmitAndWatchExtrinsic(ext)
		if err != nil {
			w.log.Error("Submit extrinsic failed", "Failed", err, "Nonce", nonce)
		}

		//fmt.Printf("submit Tx, Relayer nonce is %v\n", nonce)
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

func (w *writer) isFinish(ms MultiSigAsMulti) (bool, MultiSignTx) {
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
			w.log.Info("relayer has vote, wait others!", "Relayer", w.relayer.currentRelayer, "Block", ms.OriginMsTx.BlockNumber, "Index", ms.OriginMsTx.MultiSignTxId)
			return true, YesVoted
		}
	}

	return false, NotExecuted
}

func (w *writer) watchSubmission(sub *author.ExtrinsicStatusSubscription) error {
	for {
		select {
		case status := <-sub.Chan():
			switch {
			case status.IsInBlock:
				w.log.Info("Extrinsic included in block", status.AsInBlock.Hex())
				return nil
			case status.IsRetracted:
				fmt.Printf("extrinsic retracted: %s", status.AsRetracted.Hex())
			case status.IsDropped:
				fmt.Printf("extrinsic dropped from network\n")
			case status.IsInvalid:
				fmt.Printf("extrinsic invalid\n")
			}
		case err := <-sub.Err():
			w.log.Trace("Extrinsic subscription error\n", "err", err)
			return err
		}
	}
}

func (w *writer) UpdateMetadate() {
	meta, _ := w.msApi.RPC.State.GetMetadataLatest()
	if meta != nil {
		w.meta = meta
	}
}
