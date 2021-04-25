// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"fmt"
	"github.com/JFJun/go-substrate-crypto/ss58"
	"github.com/Rjman-self/BBridge/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rjman-self/substrate-go/expand/base"
	"github.com/rjman-self/substrate-go/models"
	"strconv"

	"github.com/rjman-self/substrate-go/client"

	"github.com/rjmand/go-substrate-rpc-client/v2/types"
	"math/big"
	"time"

	"github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/chains"
	"github.com/rjman-self/platdot-utils/blockstore"
	metrics "github.com/rjman-self/platdot-utils/metrics/types"
	"github.com/rjman-self/platdot-utils/msg"
)

type listener struct {
	name          string
	chainId       msg.ChainId
	startBlock    uint64
	endBlock      uint64
	lostAddress   string
	blockStore    blockstore.Blockstorer
	conn          *Connection
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

// Frequency of polling for a new block
const InitCapacity = 100

var BlockRetryInterval = time.Second * 5
var BlockRetryLimit = 20
var KSM int64 = 1e12
var DOT int64 = 1e10
var FixedKSMFee = KSM * 3 / 100
var FixedDOTFee = DOT * 3 / 100
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

	go func() {
		err := l.pollBlocks()
		if err != nil {
			l.log.Error("Polling blocks failed", "err", err)
		}
	}()

	return nil
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
				l.log.Trace("Block not yet finalized", "target", currentBlock, "latest", finalizedHeader.Number)
				time.Sleep(BlockRetryInterval)
				continue
			}

			if currentBlock % 5 == 0 {
				switch l.chainId {
				case config.Kusama:
					fmt.Printf("Kusama Block is #%v\n", currentBlock)
				case config.ChainX:
					fmt.Printf("ChainX Block is #%v\n", currentBlock)
				case config.Polkadot:
					fmt.Printf("Polkadot Block is #%v\n", currentBlock)
				default:
					fmt.Printf("Sub Block is #%v\n", currentBlock)
				}
			}

			if endBlock != 0 && currentBlock > endBlock {
				fmt.Printf("Sub listener work is Finished\n")
				return nil
			}

			err = l.processBlock(int64(currentBlock))
			if err != nil {
				l.log.Error("Failed to process current block", "block", currentBlock, "err", err)
				retry--
				continue
			}

			// Write to blockStore
			err = l.blockStore.StoreBlock(big.NewInt(0).SetUint64(currentBlock))
			if err != nil {
				l.log.Error("Failed to write to blockStore", "err", err)
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
			l.log.Info("processBlock err, check it", "CurrentBlock", currentBlock)
			return nil
		}
		resp, err := l.client.GetBlockByNumber(currentBlock)
		if err != nil {
			fmt.Printf("GetBlockByNumber err\n")
			time.Sleep(time.Second)
			retryTimes--
			continue
		}

		for _, e := range resp.Extrinsic {
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
				l.log.Info("Find a MultiSign New extrinsic", "Block", currentBlock)
				/// Mark New a MultiSign Transfer
				l.markNew(e)
			}

			if e.Type == base.AsMultiApprove && fromCheck {
				l.log.Info("Find a MultiSign Approve extrinsic", "Block", currentBlock)
				/// Mark Vote(Approve)
				l.markVote(msTx, e)
			}

			if e.Type == base.AsMultiExecuted && fromCheck {
				l.log.Info("Find a MultiSign Executed extrinsic", "Block", currentBlock)
				// Find An existing multi-signed transaction in the record, and marks for executed status
				l.markVote(msTx, e)
				l.markExecution(msTx)
			}

			if e.Type == base.UtilityBatch && toCheck {
				l.log.Info("Find a MultiSign Batch Extrinsic", "Block", currentBlock)
				if e.Status == "fail" {
					l.log.Info("But Batch Extrinsic Failed", "Block", currentBlock)
					continue
				}

				sendPubAddress, _ := ss58.DecodeToPub(e.FromAddress)
				LostPubAddress, _ := ss58.DecodeToPub(l.lostAddress)

				if l.lostAddress != "" {
					/// RecoverLostAddress
					if string(sendPubAddress) != string(LostPubAddress[:]) {
						continue
					} else {
						l.log.Info("Recover a Lost BatchTx", "Block", currentBlock)
					}
				}

				// Construct parameters of message
				amount, ok := big.NewInt(0).SetString(e.Amount, 10)
				if !ok {
					fmt.Printf("parse transfer amount %v, amount.string %v\n", amount, amount.String())
				}
				sendAmount := big.NewInt(0)
				actualAmount := big.NewInt(0)
				if l.chainId == config.Polkadot {
					/// KSM / DOT is 12 digits.
					fixedFee := big.NewInt(FixedDOTFee)
					additionalFee := big.NewInt(0).Div(amount, big.NewInt(FeeRate))
					fee := big.NewInt(0).Add(fixedFee, additionalFee)

					actualAmount.Sub(amount, fee)
					if actualAmount.Cmp(big.NewInt(0)) == -1 {
						l.log.Error("Charge a neg amount", "Amount", actualAmount)
						continue
					}

					sendAmount.Mul(actualAmount, big.NewInt(oneDToken))

					fmt.Printf("DOT to BDOT, Amount is %v, Fee is %v, Actual_BDOT_Amount = %v\n", amount, fee, actualAmount)
				} else if l.chainId == config.Kusama {
					/// KSM / DOT is 12 digits.
					fixedFee := big.NewInt(FixedKSMFee)
					additionalFee := big.NewInt(0).Div(amount, big.NewInt(FeeRate))
					fee := big.NewInt(0).Add(fixedFee, additionalFee)

					actualAmount.Sub(amount, fee)
					if actualAmount.Cmp(big.NewInt(0)) == -1 {
						l.log.Error("Charge a neg amount", "Amount", actualAmount)
						continue
					}

					sendAmount.Mul(actualAmount, big.NewInt(oneToken))

					fmt.Printf("KSM to AKSM, Amount is %v, Fee is %v, Actual_AKSM_Amount = %v\n", amount, fee, actualAmount)
				} else if l.chainId == config.ChainX && e.AssetId == XBTC {
					/// XBTC is 8 digits.
					sendAmount.Mul(amount, big.NewInt(oneXToken))
					fmt.Printf("XBTC to BBTC, Amount is %v, Fee is %v, Actual_ABTC_Amount = %v\n", amount, 0, amount)
				} else {
					/// Other Chain
					continue
				}

				if e.Recipient == "" {
					fmt.Printf("Not System.Remark\n")
					continue
				}
				var recipient types.AccountID

				if e.Recipient[:3] == "hex" {
					recipient = types.NewAccountID(common.FromHex(e.Recipient[3:]))
				} else {
					recipient = types.NewAccountID(common.FromHex(e.Recipient))
				}

				depositNonce, _ := strconv.ParseInt(strconv.FormatInt(currentBlock, 10)+strconv.FormatInt(int64(e.ExtrinsicIndex), 10), 10, 64)

				m := msg.NewFungibleTransfer(
					l.chainId,
					l.destId,
					msg.Nonce(depositNonce),
					sendAmount,
					l.resourceId,
					recipient[:],
				)

				switch l.chainId {
				case config.Kusama:
					l.log.Info("Ready to send AKSM...", "Amount", amount, "Recipient", recipient, "FromChain", l.name, "FromId", l.chainId, "To", l.destId)
				case config.Polkadot:
					l.log.Info("Ready to send BDOT...", "Amount", amount, "Recipient", recipient, "FromChain", l.name, "FromId", l.chainId, "To", l.destId)
				case config.ChainX:
					if e.AssetId == XBTC {
						l.log.Info("Ready to send BBTC...", "Amount", amount, "Recipient", recipient, "FromChain", l.name, "FromId", l.chainId, "To", l.destId)
					}
				default:
					l.log.Info("Ready to send BDOT...", "Amount", amount, "Recipient", recipient, "FromChain", l.name, "FromId", l.chainId, "To", l.destId)
				}

				l.submitMessage(m, err)
				if err != nil {
					l.log.Error("Submit message to Writer", "Error", err)
					return err
				}
			}
		}
		break
	}
	return nil
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
