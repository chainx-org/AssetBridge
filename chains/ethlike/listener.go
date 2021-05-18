// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package ethlike

import (
	"context"
	"errors"
	"fmt"
	"github.com/chainx-org/AssetBridge/chains/chainset"
	"math/big"
	"time"

	"github.com/ChainSafe/log15"
	"github.com/chainx-org/AssetBridge/bindings/Bridge"
	"github.com/chainx-org/AssetBridge/bindings/ERC20Handler"
	"github.com/chainx-org/AssetBridge/bindings/ERC721Handler"
	"github.com/chainx-org/AssetBridge/bindings/GenericHandler"
	"github.com/chainx-org/AssetBridge/chains"
	utils "github.com/chainx-org/AssetBridge/shared/ethlike"
	eth "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rjman-ljm/sherpax-utils/blockstore"
	metrics "github.com/rjman-ljm/sherpax-utils/metrics/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
)

var BlockRetryInterval = time.Second * 5
var BlockRetryLimit = 30

//var ErrFatalPolling = errors.New("listener block polling failed")

type listener struct {
	cfg                    Config
	conn                   Connection
	router                 chains.Router
	bridgeContract         *Bridge.Bridge // instance of bound bridge contract
	erc20HandlerContract   *ERC20Handler.ERC20Handler
	erc721HandlerContract  *ERC721Handler.ERC721Handler
	genericHandlerContract *GenericHandler.GenericHandler
	log                    log15.Logger
	blockstore             blockstore.Blockstorer
	stop                   <-chan int
	sysErr                 chan<- error // Reports fatal error to core
	latestBlock            metrics.LatestBlock
	metrics                *metrics.ChainMetrics
	blockConfirmations     *big.Int
}

// NewListener creates and returns a listener
func NewListener(conn Connection, cfg *Config, log log15.Logger, bs blockstore.Blockstorer, stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics) *listener {
	return &listener{
		cfg:                *cfg,
		conn:               conn,
		log:                log,
		blockstore:         bs,
		stop:               stop,
		sysErr:             sysErr,
		latestBlock:        metrics.LatestBlock{LastUpdated: time.Now()},
		metrics:            m,
		blockConfirmations: cfg.blockConfirmations,
	}
}

func (l *listener) setContracts(bridge *Bridge.Bridge, erc20Handler *ERC20Handler.ERC20Handler) {
	l.bridgeContract = bridge
	l.erc20HandlerContract = erc20Handler
}

// sets the router
func (l *listener) setRouter(r chains.Router) {
	l.router = r
}

// start registers all subscriptions provided by the config
func (l *listener) start() error {
	l.log.Debug("Starting listener...")

	go func() {
		l.connect()
	}()

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
			l.log.Error("Retry...", "chain", l.cfg.name)
			ClientRetryLimit = BlockRetryLimit
		}

		_, err := l.conn.LatestBlock()
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

// pollBlocks will poll for the latest block and proceed to parse the associated events as it sees new blocks.
// Polling begins at the block defined in `l.cfg.startBlock`. Failed attempts to fetch the latest block or parse
// a block will be retried up to BlockRetryLimit times before continuing to the next block.
func (l *listener) pollBlocks() error {
	l.log.Info("Polling Blocks...", "ChainId", l.cfg.id, "Chain", l.cfg.name)
	var currentBlock = l.cfg.startBlock
	var endBlock = l.cfg.endBlock
	//fmt.Printf("endBlock is %v\n", endBlock.Uint64())

	var retry = BlockRetryLimit

	for {
		select {
		case <-l.stop:
			return errors.New("polling terminated")
		default:
			// No more retries, goto next block
			if retry == 0 {
				l.log.Error("Polling failed, retries exceeded")
				return nil
			}

			latestBlock, err := l.conn.LatestBlock()
			if err != nil {
				l.log.Error("Unable to get latest block", "block", currentBlock, "err", err)
				retry--
				time.Sleep(BlockRetryInterval)
				continue
			}

			if l.metrics != nil {
				l.metrics.LatestKnownBlock.Set(float64(latestBlock.Int64()))
			}

			// Sleep if the difference is less than BlockDelay; (latest - current) < BlockDelay
			if big.NewInt(0).Sub(latestBlock, currentBlock).Cmp(l.blockConfirmations) == -1 {
				l.log.Debug("Block not ready, will retry", "target", currentBlock, "latest", latestBlock)
				time.Sleep(BlockRetryInterval)
				continue
			}

			l.logBlock(currentBlock.Uint64())

			if endBlock.Uint64() != 0 && currentBlock.Uint64() > endBlock.Uint64(){
				l.logInfo("Listener work is Finished", currentBlock.Int64())
				return nil
			}

			// Parse out events
			err = l.getDepositEventsForBlock(currentBlock)
			if err != nil {
				l.log.Error("Failed to get events for block", "block", currentBlock, "err", err)
				retry--
				continue
			}

			// Write to block store. Not a critical operation, no need to retry
			err = l.blockstore.StoreBlock(currentBlock)
			if err != nil {
				l.log.Error("Failed to write latest block to blockstore", "block", currentBlock, "err", err)
			}

			if l.metrics != nil {
				l.metrics.BlocksProcessed.Inc()
				l.metrics.LatestProcessedBlock.Set(float64(latestBlock.Int64()))
			}

			l.latestBlock.Height = big.NewInt(0).Set(latestBlock)
			l.latestBlock.LastUpdated = time.Now()

			// Goto next block and reset retry counter
			currentBlock.Add(currentBlock, big.NewInt(1))
			retry = BlockRetryLimit
		}
	}
}

// getDepositEventsForBlock looks for the deposit event in the latest block
func (l *listener) getDepositEventsForBlock(latestBlock *big.Int) error {
	l.log.Debug("Querying block for deposit events", "block", latestBlock)

	query := buildQuery(l.cfg.bridgeContract, utils.Deposit, latestBlock, latestBlock)

	// Query for logs
	logs, err := l.conn.Client().FilterLogs(context.Background(), query)
	if err != nil {
		return fmt.Errorf("unable to Filter Logs: %w", err)
	}

	// Read through the log events and handle their deposit event if handler is recognized
	for _, log := range logs {
		var m msg.Message
		destId := msg.ChainId(big.NewInt(0).SetBytes(log.Data[:32]).Uint64())
		rId := msg.ResourceIdFromSlice(log.Data[32:64])
		nonce := msg.Nonce(big.NewInt(0).SetBytes(log.Data[64:96]).Uint64())

		l.log.Info("Parse event successfully.", "DestId", destId, "ResourceId", rId, "Nonce", nonce)

		addr, err := l.bridgeContract.ResourceIDToHandlerAddress(&bind.CallOpts{From: l.conn.Keypair().CommonAddress()}, rId)
		if err != nil {
			return fmt.Errorf("failed to get handler from resource ID %x", rId)
		}

		if addr == l.cfg.erc20HandlerContract && chainset.IsNativeTransfer(destId) {
			m, err = l.handleNativeDepositedEvent(destId, nonce)
		} else if addr == l.cfg.erc20HandlerContract && !chainset.IsNativeTransfer(destId) {
			m, err = l.handleErc20DepositedEvent(destId, nonce)
		} else if addr == l.cfg.erc721HandlerContract {
			m, err = l.handleErc721DepositedEvent(destId, nonce)
		} else if addr == l.cfg.genericHandlerContract {
			m, err = l.handleGenericDepositedEvent(destId, nonce)
		} else {
			l.log.Error("event has unrecognized handler", "handler", addr.Hex())
			return nil
		}
		if err != nil {
			return err
		}

		err = l.router.Send(m)
		if err != nil {
			l.log.Error("subscription error: failed to route message", "err", err)
		}
	}

	return nil
}

// buildQuery constructs a query for the bridgeContract by hashing sig to get the event topic
func buildQuery(contract ethcommon.Address, sig utils.EventSig, startBlock *big.Int, endBlock *big.Int) eth.FilterQuery {
	query := eth.FilterQuery{
		FromBlock: startBlock,
		ToBlock:   endBlock,
		Addresses: []ethcommon.Address{contract},
		Topics: [][]ethcommon.Hash{
			{sig.GetTopic()},
		},
	}
	return query
}

func (l *listener) logBlock(currentBlock uint64) {
	if currentBlock % 5 == 0 {
		message := l.cfg.name + " listening..."
		l.log.Debug(message, "Block", currentBlock)
		if currentBlock % 3000 == 0 {
			l.log.Info(message, "Block", currentBlock)
		}
	}
}
func (l *listener) logInfo (msg string, block int64) {
	l.log.Info(msg, "Block", block, "chain", l.cfg.name)
}