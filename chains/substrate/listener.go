// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"fmt"
	"github.com/chainx-org/AssetBridge/chains/chainset"
	"github.com/rjman-ljm/substrate-go/expand/chainx"
	"github.com/rjman-ljm/substrate-go/models"

	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"math/big"
	"time"

	"github.com/ChainSafe/log15"
	"github.com/chainx-org/AssetBridge/chains"
	"github.com/rjman-ljm/sherpax-utils/blockstore"
	metrics "github.com/rjman-ljm/sherpax-utils/metrics/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
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
	multiSigAddr  types.AccountID
	curTx         multiSigTx
	asMulti       map[multiSigTx]MultiSigAsMulti
	resourceId    msg.ResourceId
	destId        msg.ChainId
	relayer       Relayer
	bridgeCore    *chainset.BridgeCore
}

var ErrBlockNotReady = errors.New("required result to be 32 bytes, but got 0")

// Frequency of polling for a new block
const InitCapacity = 100

var BlockRetryInterval = time.Second * 5
var BlockRetryLimit = 15
var RedeemRetryLimit = 15

type SourceType int
const(
	ParseExtrinsic = iota
	ParseEvent
)

func NewListener(conn *Connection, name string, id msg.ChainId, startBlock uint64, endBlock uint64, lostAddress string,
	log log15.Logger, bs blockstore.Blockstorer, stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics,
	multiSigAddress types.AccountID, resource msg.ResourceId, dest msg.ChainId, relayer Relayer, bc *chainset.BridgeCore) *listener {
	return &listener{
		name:          	name,
		chainId:       	id,
		startBlock:    	startBlock,
		endBlock:      	endBlock,
		lostAddress:   lostAddress,
		blockStore:    bs,
		conn:          conn,
		subscriptions: make(map[eventName]eventHandler),
		log:           log,
		stop:          stop,
		sysErr:        sysErr,
		latestBlock:   metrics.LatestBlock{LastUpdated: time.Now()},
		metrics:       m,
		multiSigAddr:  multiSigAddress,
		asMulti:       make(map[multiSigTx]MultiSigAsMulti, InitCapacity),
		resourceId:    resource,
		destId:        dest,
		relayer:       relayer,
		bridgeCore:    bc,
	}
}

func (l *listener) setRouter(r chains.Router) {
	l.router = r
}

// start creates the initial subscription for all events
func (l *listener) start() error {
	// Check whether latest is less than starting block
	header, err := l.conn.cli.Api.RPC.Chain.GetHeaderLatest()
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
	ClientRetryLimit := BlockRetryLimit
	for {
		if ClientRetryLimit == 0 {
			err := l.conn.Reconnect()
			l.log.Info("Connecting to another endpoint", "EndPoint", l.conn.url)
			if err != nil {
				l.log.Error("Reconnect Error", "EndPoint", l.conn.url)
			}
			ClientRetryLimit = BlockRetryLimit
		}

		// Check whether latest is less than starting block
		header, err := l.conn.cli.Api.RPC.Chain.GetHeaderLatest()
		if err != nil {
			time.Sleep(BlockRetryInterval)
			ClientRetryLimit--
			continue
		}
		l.startBlock = uint64(header.Number)

		_, err = l.conn.cli.Api.RPC.Chain.GetFinalizedHead()
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
				return fmt.Errorf("polling retries exceeded (chain=%d, name=%s)", l.chainId, l.name)
			}

			/// Get finalized block hash
			finalizedHash, err := l.conn.cli.Api.RPC.Chain.GetFinalizedHead()
			if err != nil {
				l.log.Error("Failed to fetch finalized hash", "err", err)
				retry--
				time.Sleep(BlockRetryInterval)
				continue
			}

			// Get finalized block header
			finalizedHeader, err := l.conn.cli.Api.RPC.Chain.GetHeader(finalizedHash)
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
			l.processBlockExtrinsic(int64(currentBlock))

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

			/// Deal cross-chain tx
			err = l.processEvents(hash)
			if err != nil {
				l.log.Error("Failed to process events in block", "block", currentBlock, "err", err)
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

func (l *listener) processBlockExtrinsic(currentBlock int64) {
	retryTimes := BlockRetryLimit
	for {
		if retryTimes == 0 {
			/// Maybe due to unresolved events
			break
		}

		resp, err := l.conn.cli.GetBlockByNumber(currentBlock)
		if err != nil {
			if retryTimes == BlockRetryLimit / 2 {
				l.logErr(GetBlockByNumberError, err)
			}
			time.Sleep(time.Second)
			retryTimes--
			continue
		}

		/// Deal each extrinsic
		l.dealBlockTx(resp, currentBlock)

		break
	}
}


// processEvents fetches a block and parses out the events, calling Listener.handleEvents()
func (l *listener) processEvents(hash types.Hash) error {
	l.log.Trace("Fetching block for events", "hash", hash.Hex())

	meta := l.conn.getMetadata()

	key, err := types.CreateStorageKey(&meta, "System", "Events", nil, nil)
	if err != nil {
		return err
	}

	var records types.EventRecordsRaw
	_, err = l.conn.api.RPC.State.GetStorage(key, &records, hash)
	if err != nil {
		return err
	}

	e := chainx.ChainXEventRecords{}
	err = records.DecodeEventRecords(&meta, &e)
	if err != nil {
		return err
	}

	l.handleEvents(e)
	l.log.Trace("Finished processing events", "block", hash.Hex())

	return nil
}

// handleEvents calls the associated handler for all registered event types
func (l *listener) handleEvents(evts chainx.ChainXEventRecords) {
	if l.subscriptions[FungibleTransfer] != nil {
		for _, evt := range evts.ChainBridge_FungibleTransfer {
			l.log.Trace("Handling FungibleTransfer event")
			m, err := l.subscriptions[FungibleTransfer](evt, l.log)
			l.submitMessage(ParseEvent, m, err)
		}
	}
	if l.subscriptions[NonFungibleTransfer] != nil {
		for _, evt := range evts.ChainBridge_NonFungibleTransfer {
			l.log.Trace("Handling NonFungibleTransfer event")
			m, err := l.subscriptions[NonFungibleTransfer](evt, l.log)
			l.submitMessage(ParseEvent, m, err)
		}
	}
	if l.subscriptions[GenericTransfer] != nil {
		for _, evt := range evts.ChainBridge_GenericTransfer {
			l.log.Trace("Handling GenericTransfer event")
			m, err := l.subscriptions[GenericTransfer](evt, l.log)
			l.submitMessage(ParseEvent, m, err)
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
func (l *listener) submitMessage(source SourceType, m msg.Message, err error) {
	if err != nil {
		log15.Error("Critical error processing event", "err", err)
		return
	}

	switch source {
	case ParseExtrinsic:
		m.Type = l.checkExtrinsicMessageType(m)
	case ParseEvent:
		m.Type = l.checkEventMessageType(m)
	default:
		/// Do nothing
		l.log.Error("Message from unknown sources")
	}

	m.Source = l.chainId
	err = l.router.Send(m)
	if err != nil {
		log15.Error("failed to process event", "err", err)
	}
}

func (l *listener) checkExtrinsicMessageType(m msg.Message) msg.TransferType {
	rIdXDOT := l.bridgeCore.ConvertStringToResourceId(chainset.ResourceIdXDOT)
	rIdXKSM := l.bridgeCore.ConvertStringToResourceId(chainset.ResourceIdXKSM)
	if m.ResourceId == rIdXDOT || m.ResourceId == rIdXKSM {
		return msg.FungibleTransfer
	} else {
		return msg.MultiSigTransfer
	}
}

func (l *listener) checkEventMessageType(m msg.Message) msg.TransferType {
	rIdXUSD := l.bridgeCore.ConvertStringToResourceId(chainset.ResourceIdXUSD)
	rIdXDOT := l.bridgeCore.ConvertStringToResourceId(chainset.ResourceIdXDOT)
	rIdXKSM := l.bridgeCore.ConvertStringToResourceId(chainset.ResourceIdXKSM)
	if m.ResourceId == rIdXUSD {
		return msg.FungibleTransfer
	} else if m.ResourceId == rIdXDOT || m.ResourceId == rIdXKSM {
		return msg.MultiSigTransfer
	} else {
		return msg.NativeTransfer
	}
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
	currency, err := l.bridgeCore.GetCurrencyByAssetId(e.AssetId)
	if err != nil {
		fmt.Printf("unimplemented currency")
		return
	}
	sendMessage := "Ready to send " + currency.Name + "..."
	l.log.Info(LineLog, "Amount", amount, "FromId", l.chainId, "To", l.destId)
	l.log.Info(sendMessage, "Amount", amount, "FromId", l.chainId, "To", l.destId)
	l.log.Info(LineLog, "Amount", amount, "FromId", l.chainId, "To", l.destId)
}