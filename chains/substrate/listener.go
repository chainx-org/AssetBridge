// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"errors"
	"fmt"
	"github.com/JFJun/go-substrate-crypto/ss58"
	"github.com/Rjman-self/BBridge/config"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rjman-self/substrate-go/expand"
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

			err = l.processBlock(int64(currentBlock))
			if err != nil {
				l.log.Error(FailedToProcessCurrentBlock, "block", currentBlock, "err", err)
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

			m := msg.NewFungibleTransfer(
				l.chainId,
				l.destId,
				msg.Nonce(depositNonce),
				sendAmount,
				l.resourceId,
				recipient[:],
			)
			l.logReadyToSend(sendAmount, recipient, e)
			l.submitMessage(m)
		}
	}
}

// submitMessage inserts the chainId into the msg and sends it to the router
func (l *listener) submitMessage(m msg.Message) {
	m.Source = l.chainId
	err := l.router.Send(m)
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
	if l.chainId == config.Polkadot {
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
	} else if l.chainId == config.Kusama {
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
	} else if (l.chainId == config.ChainXBTCV1 || l.chainId == config.ChainXBTCV2) && e.AssetId == XBTC {
		/// XBTC is 8 digits.
		sendAmount.Mul(amount, big.NewInt(oneXBTC))
		l.logCrossChainTx("XBTC", "BBTC", amount, big.NewInt(0), amount)
	} else if (l.chainId == config.ChainXPCXV1 || l.chainId == config.ChainXPCXV2) && e.AssetId == expand.PcxAssetId {
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
	case config.Kusama:
		token = "AKSM"
	case config.Polkadot:
		token = "PDOT"
	case config.ChainXBTCV1:
		if e.AssetId == XBTC {
			token = "ABTC"
		}
	case config.ChainXBTCV2:
		if e.AssetId == XBTC {
			token = "ABTC"
		}
	case config.ChainXPCXV1:
		if e.AssetId == expand.PcxAssetId {
			token = "APCX"
		}
	case config.ChainXPCXV2:
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