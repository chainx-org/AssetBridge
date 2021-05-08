// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"fmt"
	"github.com/JFJun/go-substrate-crypto/ss58"
	"github.com/Rjman-self/BBridge/config"
	utils "github.com/Rjman-self/BBridge/shared/substrate"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rjman-self/substrate-go/expand"
	"github.com/rjman-self/substrate-go/expand/base"
	"github.com/rjman-self/substrate-go/models"
	"strconv"

	"github.com/rjman-self/substrate-go/client"

	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"math/big"
	"time"

	"github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/chains"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	types3 "github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/sherpax-utils/blockstore"
	metrics "github.com/rjman-self/sherpax-utils/metrics/types"
	"github.com/rjman-self/sherpax-utils/msg"
)

type listener struct {
	name          string
	chainId       msg.ChainId
	startBlock    uint64
	endBlock      uint64
	lostAddress   string
	blockStore    blockstore.Blockstorer
	conn          *Connection
	subscriptions map[eventName]eventHandler // Handlers for specific events
	router        chains.Router
	log           log15.Logger
	stop          <-chan int
	sysErr        chan<- error
	latestBlock   metrics.LatestBlock
	metrics       *metrics.ChainMetrics
	client        client.Client
	multiSignAddr types.AccountID
	currentTx     MultiSignTx
	msTxAsMulti   map[MultiSignTx]MultiSigAsMulti
	resourceId    msg.ResourceId
	destId        msg.ChainId
	relayer       Relayer
}

var ErrBlockNotReady = errors.New("required result to be 32 bytes, but got 0")

// Frequency of polling for a new block
const InitCapacity = 100

var BlockRetryInterval = time.Second * 5
var BlockRetryLimit = 30
var RedeemRetryLimit = 15
var KSM int64 = 1e12
var DOT int64 = 1e10
var PCX int64 = 1e8
var FixedKSMFee = KSM * 3 / 100	/// 0.03KSM
var FixedDOTFee = DOT * 5 / 10	/// 0.5DOT
var FixedPCXFee = PCX * 1 / 10	/// 0.1PCX
var FeeRate int64 = 1000


func NewListener(conn *Connection, name string, id msg.ChainId, startBlock uint64, endBlock uint64, lostAddress string, log log15.Logger, bs blockstore.Blockstorer,
	stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics, multiSignAddress types.AccountID, cli *client.Client,
	resource msg.ResourceId, dest msg.ChainId, relayer Relayer) *listener {
	return &listener{
		name:          name,
		chainId:       id,
		startBlock:    startBlock,
		endBlock:      endBlock,
		lostAddress:   lostAddress,
		blockStore:    bs,
		conn:          conn,
		subscriptions: make(map[eventName]eventHandler),
		log:           log,
		stop:          stop,
		sysErr:        sysErr,
		latestBlock:   metrics.LatestBlock{LastUpdated: time.Now()},
		metrics:       m,
		client:        *cli,
		multiSignAddr: multiSignAddress,
		msTxAsMulti:   make(map[MultiSignTx]MultiSigAsMulti, InitCapacity),
		resourceId:    resource,
		destId:        dest,
		relayer:       relayer,
	}
}

func (l *listener) setRouter(r chains.Router) {
	l.router = r
}

// start creates the initial subscription for all events
func (l *listener) start() error {
	// Check whether latest is less than starting block
	header, err := l.client.Api.RPC.Chain.GetHeaderLatest()
	if err != nil {
		return err
	}
	if uint64(header.Number) < l.startBlock {
		return fmt.Errorf("starting block (%d) is greater than latest known block (%d)", l.startBlock, header.Number)
	}

	for _, sub := range Subscriptions {
		err := l.registerEventHandler(sub.name, sub.handler)
		if err != nil {
			return err
		}
	}

	go func() {
		l.connect()
	}()

	return nil
}

// registerEventHandler enables a handler for a given event. This cannot be used after Start is called.
func (l *listener) registerEventHandler(name eventName, handler eventHandler) error {
	if l.subscriptions[name] != nil {
		return fmt.Errorf("event %s already registered", name)
	}
	l.subscriptions[name] = handler
	return nil
}


func (l *listener) connect() {
	err := l.pollBlocks()
	if err != nil {
		l.log.Error("Polling blocks failed, retrying...", "err", err)
		/// Poll block err, reconnecting...
		l.reconnect()
	}
}

func (l *listener) reconnect() {
	ClientRetryLimit := BlockRetryLimit*BlockRetryLimit
	for {
		if ClientRetryLimit == 0 {
			l.log.Error("Retry...", "chain", l.name)
			cli, err := client.New(l.conn.url)
			if err == nil {
				//InitializePrefixById(l.chainId, cli)
				InitializePrefixByName(l.name, cli)
				l.client = *cli
			}
			ClientRetryLimit = BlockRetryLimit
		}
		// Check whether latest is less than starting block
		header, err := l.client.Api.RPC.Chain.GetHeaderLatest()
		if err != nil {
			time.Sleep(BlockRetryInterval)
			ClientRetryLimit--
			continue
		}
		l.startBlock = uint64(header.Number)

		_, err = l.client.Api.RPC.Chain.GetFinalizedHead()
		if err != nil {
			time.Sleep(BlockRetryInterval)
			continue
		}

		go func() {
			l.connect()
		}()

		break
	}
}

func (l *listener) logBlock(currentBlock uint64) {
	if currentBlock % 5 == 0 {
		message := l.name + " listening..."
		l.log.Debug(message, "Block", currentBlock)
		if currentBlock % 1000 == 0 {
			l.log.Info(message, "Block", currentBlock)
		}
	}
}

// pollBlocks will poll for the latest block and proceed to parse the associated events as it sees new blocks.
// Polling begins at the block defined in `l.startBlock`. Failed attempts to fetch the latest block or parse
// a block will be retried up to BlockRetryLimit times before returning with an error.
func (l *listener) pollBlocks() error {
	l.log.Info("Polling Blocks...", "ChainId", l.chainId, "Chain", l.name)
	var currentBlock = l.startBlock
	var endBlock = l.endBlock
	var retry = BlockRetryLimit
	for {
		select {
		case <-l.stop:
			return errors.New("terminated")
		default:
			// No more retries, goto next block
			if retry == 0 {
				l.sysErr <- fmt.Errorf("event polling retries exceeded (chain=%d, name=%s)", l.chainId, l.name)
				return nil
			}

			/// Get finalized block hash
			finalizedHash, err := l.client.Api.RPC.Chain.GetFinalizedHead()
			if err != nil {
				l.log.Error("Failed to fetch finalized hash", "err", err)
				retry--
				time.Sleep(BlockRetryInterval)
				continue
			}

			// Get finalized block header
			finalizedHeader, err := l.client.Api.RPC.Chain.GetHeader(finalizedHash)
			if err != nil {
				l.log.Error("Failed to fetch finalized header", "err", err)
				retry--
				time.Sleep(BlockRetryInterval)
				continue
			}

			if l.metrics != nil {
				l.metrics.LatestKnownBlock.Set(float64(finalizedHeader.Number))
			}

			// Sleep if the block we want comes after the most recently finalized block
			if currentBlock > uint64(finalizedHeader.Number) {
				l.log.Trace(BlockNotYetFinalized, "target", currentBlock, "latest", finalizedHeader.Number)
				time.Sleep(BlockRetryInterval)
				continue
			}

			l.logBlock(currentBlock)

			if endBlock != 0 && currentBlock > endBlock {
				l.logInfo(SubListenerWorkFinished, int64(currentBlock))
				return nil
			}

			/// Listen Native Transfer
			err = l.processBlock(int64(currentBlock))
			if err != nil {
				l.log.Error(FailedToProcessCurrentBlock, "block", currentBlock, "err", err)
				retry--
				continue
			}

			/// Listen Erc20/Erc721/Generic Transfer
			// Get hash for latest block, sleep and retry if not ready
			hash, err := l.conn.api.RPC.Chain.GetBlockHash(currentBlock)
			if err != nil && err.Error() == ErrBlockNotReady.Error() {
				time.Sleep(BlockRetryInterval)
				continue
			} else if err != nil {
				l.log.Error("Failed to query current block", "block", currentBlock, "err", err)
				retry--
				time.Sleep(BlockRetryInterval)
				continue
			}

			err = l.processEvents(hash)
			if err != nil {
				l.log.Error("Failed to process events in block", "block", currentBlock, "err", err)
				retry--
				continue
			}

			// Write to blockStore
			err = l.blockStore.StoreBlock(big.NewInt(0).SetUint64(currentBlock))
			if err != nil {
				l.log.Error(FailedToWriteToBlockStore, "err", err)
			}

			if l.metrics != nil {
				l.metrics.BlocksProcessed.Inc()
				l.metrics.LatestProcessedBlock.Set(float64(currentBlock))
			}

			currentBlock++
			l.latestBlock.Height = big.NewInt(0).SetUint64(currentBlock)
			l.latestBlock.LastUpdated = time.Now()

			/// Succeed, reset retryLimit
			retry = BlockRetryLimit
		}
	}
}

func (l *listener) processBlock(currentBlock int64) error {
	retryTimes := BlockRetryLimit
	for {
		if retryTimes == 0 {
			l.logInfo(ProcessBlockError, currentBlock)
			return nil
		}

		resp, err := l.client.GetBlockByNumber(currentBlock)
		if err != nil {
			l.logErr(GetBlockByNumberError, err)
			time.Sleep(time.Second)
			retryTimes--
			continue
		}

		/// Deal each extrinsic
		l.dealBlockTx(resp, currentBlock)

		break
	}
	return nil
}


// processEvents fetches a block and parses out the events, calling Listener.handleEvents()
func (l *listener) processEvents(hash types3.Hash) error {
	l.log.Trace("Fetching block for events", "hash", hash.Hex())

	api, err := gsrpc.NewSubstrateAPI(l.conn.url)
	if err != nil {
		l.logErr("New api Err", err)
	}

	meta := l.conn.getMetadata()

	key, err := types3.CreateStorageKey(&meta, "System", "Events", nil, nil)
	if err != nil {
		return err
	}

	var records types3.EventRecordsRaw
	_, err = api.RPC.State.GetStorage(key, &records, types3.Hash(hash))
	if err != nil {
		return err
	}

	e := utils.Events{}
	err = records.DecodeEventRecords(&meta, &e)
	if err != nil {
		return err
	}

	l.handleEvents(e)
	l.log.Trace("Finished processing events", "block", hash.Hex())

	return nil
}

// handleEvents calls the associated handler for all registered event types
func (l *listener) handleEvents(evts utils.Events) {
	if l.subscriptions[FungibleTransfer] != nil {
		for _, evt := range evts.ChainBridge_FungibleTransfer {
			l.log.Trace("Handling FungibleTransfer event")
			l.submitMessage(l.subscriptions[FungibleTransfer](evt, l.log))
		}
	}
	if l.subscriptions[NonFungibleTransfer] != nil {
		for _, evt := range evts.ChainBridge_NonFungibleTransfer {
			l.log.Trace("Handling NonFungibleTransfer event")
			l.submitMessage(l.subscriptions[NonFungibleTransfer](evt, l.log))
		}
	}
	if l.subscriptions[GenericTransfer] != nil {
		for _, evt := range evts.ChainBridge_GenericTransfer {
			l.log.Trace("Handling GenericTransfer event")
			l.submitMessage(l.subscriptions[GenericTransfer](evt, l.log))
		}
	}

	if len(evts.System_CodeUpdated) > 0 {
		l.log.Trace("Received CodeUpdated event")
		err := l.conn.updateMetatdata()
		if err != nil {
			l.log.Error("Unable to update Metadata", "error", err)
		}
	}
}

// submitMessage inserts the chainId into the msg and sends it to the router
func (l *listener) submitMessage(m msg.Message, err error) {
	if err != nil {
		log15.Error("Critical error processing event", "err", err)
		return
	}
	m.Source = l.chainId
	err = l.router.Send(m)
	if err != nil {
		log15.Error("failed to process event", "err", err)
	}
}


func (l *listener) dealBlockTx(resp *models.BlockResponse, currentBlock int64) {
	for _, e := range resp.Extrinsic {
		if e.Status == "fail" {
			continue
		}
		// Current Extrinsic { Block, Index }
		l.currentTx.BlockNumber = BlockNumber(currentBlock)
		l.currentTx.MultiSignTxId = MultiSignTxId(e.ExtrinsicIndex)
		msTx := MultiSigAsMulti{
			DestAddress: e.MultiSigAsMulti.DestAddress,
			DestAmount:  e.MultiSigAsMulti.DestAmount,
		}
		fromCheck := l.checkFromAddress(e)
		toCheck := l.checkToAddress(e)
		if e.Type == base.AsMultiNew && fromCheck {
			//l.logInfo(FindNewMultiSigTx, currentBlock)
			/// Mark New a MultiSign Transfer
			l.markNew(e)
		}

		if e.Type == base.AsMultiApprove && fromCheck {
			//l.logInfo(FindApproveMultiSigTx, currentBlock)
			/// Mark Vote(Approve)
			l.markVote(msTx, e)
		}

		if e.Type == base.AsMultiExecuted && fromCheck {
			//l.logInfo(FindExecutedMultiSigTx, currentBlock)
			// Find An existing multi-signed transaction in the record, and marks for executed status
			l.markVote(msTx, e)
			l.markExecution(msTx)
		}

		if e.Type == base.UtilityBatch && toCheck {
			//l.logInfo(FindBatchMultiSigTx, currentBlock)
			if l.findLostTxByAddress(currentBlock, e) {
				continue
			}

			sendAmount, ok := l.getSendAmount(e)
			/// if `chainId wrong`, `amount is negative` or `not cross-chain tx`
			if !ok || e.Recipient == "" {
				continue
			}

			var recipient []byte
			if e.Recipient[:3] == "hex" {
				recipientAccount := types.NewAccountID(common.FromHex(e.Recipient[3:]))
				recipient = recipientAccount[:]
			} else {
				recipient = []byte(e.Recipient)
			}

			depositNonce, _ := strconv.ParseInt(strconv.FormatInt(currentBlock, 10)+strconv.FormatInt(int64(e.ExtrinsicIndex), 10), 10, 64)

			m := msg.NewNativeTransfer(
				l.chainId,
				l.destId,
				msg.Nonce(depositNonce),
				sendAmount,
				l.resourceId,
				recipient[:],
			)
			if m.Destination <= 100 {
				fmt.Printf("Sub listener make a `NativeTransfer`, dest is %v, delete this line\n", m.Destination)
			}
			l.logReadyToSend(sendAmount, recipient, e)
			l.submitMessage(m, nil)
		}
	}
}


func (l *listener) markNew(e *models.ExtrinsicResponse) {
	msTx := MultiSigAsMulti{
		Executed:       false,
		Threshold:      e.MultiSigAsMulti.Threshold,
		MaybeTimePoint: e.MultiSigAsMulti.MaybeTimePoint,
		DestAddress:    e.MultiSigAsMulti.DestAddress,
		DestAmount:     e.MultiSigAsMulti.DestAmount,
		Others:         nil,
		StoreCall:      e.MultiSigAsMulti.StoreCall,
		MaxWeight:      e.MultiSigAsMulti.MaxWeight,
		OriginMsTx:     l.currentTx,
	}
	/// Mark voted
	msTx.Others = append(msTx.Others, e.MultiSigAsMulti.OtherSignatories)
	l.msTxAsMulti[l.currentTx] = msTx
}

func (l *listener) markVote(msTx MultiSigAsMulti, e *models.ExtrinsicResponse) {
	for k, ms := range l.msTxAsMulti {
		if !ms.Executed && ms.DestAddress == msTx.DestAddress && ms.DestAmount == msTx.DestAmount {
			//l.log.Info("relayer succeed vote", "Address", e.FromAddress)
			voteMsTx := l.msTxAsMulti[k]
			voteMsTx.Others = append(voteMsTx.Others, e.MultiSigAsMulti.OtherSignatories)
			l.msTxAsMulti[k] = voteMsTx
		}
	}
}

func (l *listener) markExecution(msTx MultiSigAsMulti) {
	for k, ms := range l.msTxAsMulti {
		if !ms.Executed && ms.DestAddress == msTx.DestAddress && ms.DestAmount == msTx.DestAmount {
			exeMsTx := l.msTxAsMulti[k]
			exeMsTx.Executed = true
			l.msTxAsMulti[k] = exeMsTx
		}
	}
}

func (l *listener) findLostTxByAddress(currentBlock int64, e *models.ExtrinsicResponse) bool {
	sendPubAddress, _ := ss58.DecodeToPub(e.FromAddress)
	lostPubAddress, _ := ss58.DecodeToPub(l.lostAddress)

	if l.lostAddress != "" {
		/// Find the lost transaction
		if string(sendPubAddress) == string(lostPubAddress[:]) {
			l.logInfo(FindLostMultiSigTx, currentBlock)
		}
		return true
	} else {
		return false
	}
}

func (l *listener) getSendAmount(e *models.ExtrinsicResponse) (*big.Int, bool) {
	// Construct parameters of message
	amount, ok := big.NewInt(0).SetString(e.Amount, 10)
	if !ok {
		fmt.Printf("parse transfer amount %v, amount.string %v\n", amount, amount.String())
	}
	sendAmount := big.NewInt(0)
	actualAmount := big.NewInt(0)
	if l.chainId == config.IdPolkadot {
		/// DOT is 10 digits.
		fixedFee := big.NewInt(FixedDOTFee)
		additionalFee := big.NewInt(0).Div(amount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)

		actualAmount.Sub(amount, fee)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			l.log.Error("Charge a neg amount", "Amount", actualAmount, "chain", l.name)
			return nil, false
		}
		sendAmount.Mul(actualAmount, big.NewInt(oneDOT))
		l.logCrossChainTx("DOT", "BDOT", amount, fee, actualAmount)
	} else if l.chainId == config.IdKusama {
		/// KSM is 12 digits.
		fixedFee := big.NewInt(FixedKSMFee)
		additionalFee := big.NewInt(0).Div(amount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)

		actualAmount.Sub(amount, fee)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			l.log.Error("Charge a neg amount", "Amount", actualAmount, "chain", l.name)
			return nil, false
		}
		sendAmount.Mul(actualAmount, big.NewInt(oneKSM))
		l.logCrossChainTx("KSM", "BKSM", amount, fee, actualAmount)
	} else if (l.chainId == config.IdChainXBTCV1 || l.chainId == config.IdChainXBTCV2) && e.AssetId == XBTC {
		/// XBTC is 8 digits.
		sendAmount.Mul(amount, big.NewInt(oneXBTC))
		l.logCrossChainTx("XBTC", "BBTC", amount, big.NewInt(0), amount)
	} else if (l.chainId == config.IdChainXPCXV1 || l.chainId == config.IdChainXPCXV2) && e.AssetId == expand.PcxAssetId {
		/// PCX is 8 digits.
		fixedFee := big.NewInt(FixedPCXFee)
		additionalFee := big.NewInt(0).Div(amount, big.NewInt(FeeRate))
		fee := big.NewInt(0).Add(fixedFee, additionalFee)

		actualAmount.Sub(amount, fee)
		if actualAmount.Cmp(big.NewInt(0)) == -1 {
			l.log.Error("Charge a neg amount", "Amount", actualAmount, "chain", l.name)
			return nil, false
		}
		sendAmount.Mul(actualAmount, big.NewInt(onePCX))
		l.logCrossChainTx("PCX", "BPCX", amount, fee, actualAmount)
	} else {
		/// Other Chain
		l.log.Error("chainId set wrong", "chainId", l.chainId)
		return nil, false
	}
	return sendAmount, true
}

func (l *listener) checkToAddress(e *models.ExtrinsicResponse) bool {
	/// Validate whether a cross-chain transaction
	toPubAddress, _ := ss58.DecodeToPub(e.ToAddress)
	toAddress := types.NewAddressFromAccountID(toPubAddress)
	return toAddress.AsAccountID == l.multiSignAddr
}

func (l *listener) checkFromAddress(e *models.ExtrinsicResponse) bool {
	fromPubAddress, _ := ss58.DecodeToPub(e.FromAddress)
	fromAddress := types.NewAddressFromAccountID(fromPubAddress)
	currentRelayer := types.NewAddressFromAccountID(l.relayer.kr.PublicKey)
	if currentRelayer.AsAccountID == fromAddress.AsAccountID {
		return true
	}
	for _, r := range l.relayer.otherSignatories {
		if types.AccountID(r) == fromAddress.AsAccountID {
			return true
		}
	}
	return false
}

func (l *listener) logInfo (msg string, block int64) {
	l.log.Info(msg, "Block", block, "chain", l.name)
}

func (l *listener) logErr (msg string, err error) {
	l.log.Error(msg, "Error", err, "chain", l.name)
}

func (l *listener) logCrossChainTx (tokenX string, tokenY string, amount *big.Int, fee *big.Int, actualAmount *big.Int) {
	message := tokenX + " to " + tokenY
	actualTitle := "Actual_" + tokenY + "Amount"
	l.log.Info(message,"Amount", amount, "Fee", fee, actualTitle, actualAmount)
}

func (l *listener) logReadyToSend(amount *big.Int, recipient []byte, e *models.ExtrinsicResponse) {
	var token string
	switch l.chainId {
	case config.IdKusama:
		token = "AKSM"
	case config.IdPolkadot:
		token = "PDOT"
	case config.IdChainXBTCV1:
		if e.AssetId == XBTC {
			token = "ABTC"
		}
	case config.IdChainXBTCV2:
		if e.AssetId == XBTC {
			token = "ABTC"
		}
	case config.IdChainXPCXV1:
		if e.AssetId == expand.PcxAssetId {
			token = "APCX"
		}
	case config.IdChainXPCXV2:
		if e.AssetId == expand.PcxAssetId {
			token = "APCX"
		}
	default:
		l.log.Error("chainId set wrong, meet error", "ChainId", l.chainId)
		return
	}
	message := "Ready to send " + token + "..."
	l.log.Info(LineLog, "Amount", amount, "FromId", l.chainId, "To", l.destId)
	l.log.Info(message, "Amount", amount, "FromId", l.chainId, "To", l.destId)
	l.log.Info(LineLog, "Amount", amount, "FromId", l.chainId, "To", l.destId)
}