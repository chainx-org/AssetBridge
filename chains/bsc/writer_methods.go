// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package bsc

import (
	"context"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"time"

	log "github.com/ChainSafe/log15"
	utils "github.com/Rjman-self/BBridge/shared/platdot"
	"github.com/rjman-self/platdot-utils/msg"
)

// Number of blocks to wait for an finalization event
const ExecuteBlockWatchLimit = 100

// Time between retrying a failed tx
const TxRetryInterval = time.Second * 2

// Maximum number of tx retries before exiting
const TxRetryLimit = 10

var ErrNonceTooLow = errors.New("nonce too low")
var ErrTxUnderpriced = errors.New("replacement transaction underpriced")
var ErrFatalTx = errors.New("submission of transaction failed")
var ErrFatalQuery = errors.New("query of chain state failed")

// proposalIsComplete returns true if the proposal state is either Passed, Transferred or Cancelled
func (w *writer) proposalIsComplete(srcId msg.ChainId, nonce msg.Nonce, dataHash [32]byte) bool {
	prop, err := w.bridgeContract.GetProposal(w.conn.CallOpts(), uint8(srcId), uint64(nonce), dataHash)

	if err != nil {
		w.log.Error("Failed to check proposal existence", "err", err)
		return false
	}

	return prop.Status == PassedStatus || prop.Status == TransferredStatus || prop.Status == CancelledStatus
}

// proposalIsComplete returns true if the proposal state is Transferred or Cancelled
func (w *writer) proposalIsFinalized(srcId msg.ChainId, nonce msg.Nonce, dataHash [32]byte) bool {
	prop, err := w.bridgeContract.GetProposal(w.conn.CallOpts(), uint8(srcId), uint64(nonce), dataHash)
	if err != nil {
		w.log.Error("Failed to check proposal existence", "err", err)
		return false
	}
	return prop.Status == TransferredStatus || prop.Status == CancelledStatus // Transferred (3)
}

func (w *writer) proposalIsPassed(srcId msg.ChainId, nonce msg.Nonce, dataHash [32]byte) bool {
	prop, err := w.bridgeContract.GetProposal(w.conn.CallOpts(), uint8(srcId), uint64(nonce), dataHash)
	if err != nil {
		w.log.Error("Failed to check proposal existence", "err", err)
		return false
	}
	return prop.Status == PassedStatus
}

// hasVoted checks if this relayer has already voted
func (w *writer) hasVoted(srcId msg.ChainId, nonce msg.Nonce, dataHash [32]byte) bool {
	hasVoted, err := w.bridgeContract.HasVotedOnProposal(w.conn.CallOpts(), utils.IDAndNonce(srcId, nonce), dataHash, w.conn.Opts().From)
	if err != nil {
		w.log.Error("Failed to check proposal existence", "err", err)
		return false
	}

	return hasVoted
}

func (w *writer) shouldVote(m msg.Message, dataHash [32]byte) bool {
	// Check if proposal has passed and skip if Passed or Transferred
	if w.proposalIsComplete(m.Source, m.DepositNonce, dataHash) {
		w.log.Info("Proposal complete, not voting", "src", m.Source, "nonce", m.DepositNonce)
		return false
	}

	// Check if relayer has previously voted
	if w.hasVoted(m.Source, m.DepositNonce, dataHash) {
		w.log.Info("Relayer has already voted, not voting", "src", m.Source, "nonce", m.DepositNonce)
		return false
	}

	return true
}

// createErc20Proposal creates an Erc20 proposal.
// Returns true if the proposal is successfully created or is complete
func (w *writer) createErc20Proposal(m msg.Message) bool {
	w.log.Info("Creating erc20 proposal", "src", m.Source, "nonce", m.DepositNonce)

	data := ConstructErc20ProposalData(m.Payload[0].([]byte), m.Payload[1].([]byte))
	dataHash := utils.Hash(append(w.cfg.erc20HandlerContract.Bytes(), data...))

	if !w.shouldVote(m, dataHash) {
		if w.proposalIsPassed(m.Source, m.DepositNonce, dataHash) {
			// Execute if proposal passed
			w.executeProposal(m, data, dataHash)
			return true
		} else {
			return false
		}
	}

	// Capture latest block so when know where to watch from
	latestBlock, err := w.conn.LatestBlock()
	if err != nil {
		w.log.Error("Unable to fetch latest block", "err", err)
		return false
	}

	// Watch for execution event
	go w.watchThenExecute(m, data, dataHash, latestBlock)

	w.voteProposal(m, dataHash)

	//var Www = "0xa45a0ddd81da79f65cbcfeefc8e62382b1f56ccbbdd9533f77cdc49172cca33d"
	//var Alice = "0xd43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d"
	//var Bob = "0x8eaf04151687736326c9fea17e25fc5287613693c912909cb226aa4794f26a48"
	//var Charlie = "0x90b5ab205c6974c9ea841be688864633dc9ca8a357843eeacf2314649965fe22"
	//var Dave = "0x306721211d5404bd9da88e0204360a1a9ab8b87c66c1bc2fcdd37f3c2222cc20"
	//var Eve = "0xe659a7a1628cdd93febc04a4e0646ea20e9f5f0ce097d9a05290d4a9e054df4e"
	//var Fred = "0x1cbd2d43530a44705ad088af313e18f80b53ef16b36177cd4b77b846f2a5f07c"
	//
	//var recipients = [7]string{Www, Alice, Bob, Charlie, Dave, Eve, Fred}
	//for i := 0; i < 1; i++ {
	//	dataReturn := utils.ConstructErc20DepositData([]byte(recipients[i]), big.NewInt(400000000000000000))
	//	//dataaaaaa := "0x05e2ca170000000000000000000000000000000000000000000000000000000000000003000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000600000000000000000000000000000000000000000000000000000000000000082000000000000000000000000000000000000000000000000000002ba7def30000000000000000000000000000000000000000000000000000000000000000042307831636264326434333533306134343730356164303838616633313365313866383062353365663136623336313737636434623737623834366632613566303763000000000000000000000000000000000000000000000000000000000000"
	//	w.conn.Opts().Nonce = w.conn.Opts().Nonce.Add(w.conn.Opts().Nonce, big.NewInt(1))
	//	w.conn.Opts().Value = big.NewInt(0)
	//	DepositTx, err := w.bridgeContract.Deposit(
	//		w.conn.Opts(),
	//		uint8(m.Source),
	//		m.ResourceId,
	//		dataReturn,
	//	)
	//	fmt.Printf("datareturn is %v\n", common.BytesToHash(dataReturn))
	//
	//	log.Info("Deposit", "dataReturn", dataReturn, "DepositTx", DepositTx)
	//	if err != nil {
	//		log.Info("err is %v\n", err)
	//	}
	//}

	return true
}

// createErc721Proposal creates an Erc721 proposal.
// Returns true if the proposal is succesfully created or is complete
func (w *writer) createErc721Proposal(m msg.Message) bool {
	w.log.Info("Creating erc721 proposal", "src", m.Source, "nonce", m.DepositNonce)

	data := ConstructErc721ProposalData(m.Payload[0].([]byte), m.Payload[1].([]byte), m.Payload[2].([]byte))
	dataHash := utils.Hash(append(w.cfg.erc721HandlerContract.Bytes(), data...))

	if !w.shouldVote(m, dataHash) {
		if w.proposalIsPassed(m.Source, m.DepositNonce, dataHash) {
			// We should not vote for this proposal but it is ready to be executed
			w.executeProposal(m, data, dataHash)
			return true
		} else {
			return false
		}
	}

	// Capture latest block so we know where to watch from
	latestBlock, err := w.conn.LatestBlock()
	if err != nil {
		w.log.Error("Unable to fetch latest block", "err", err)
		return false
	}

	// watch for execution event
	go w.watchThenExecute(m, data, dataHash, latestBlock)

	w.voteProposal(m, dataHash)

	return true
}

// createGenericDepositProposal creates a generic proposal
// returns true if the proposal is complete or is succesfully created
func (w *writer) createGenericDepositProposal(m msg.Message) bool {
	w.log.Info("Creating generic proposal", "src", m.Source, "nonce", m.DepositNonce)

	metadata := m.Payload[0].([]byte)
	data := ConstructGenericProposalData(metadata)
	toHash := append(w.cfg.genericHandlerContract.Bytes(), data...)
	dataHash := utils.Hash(toHash)

	if !w.shouldVote(m, dataHash) {
		if w.proposalIsPassed(m.Source, m.DepositNonce, dataHash) {
			// We should not vote for this proposal but it is ready to be executed
			w.executeProposal(m, data, dataHash)
			return true
		} else {
			return false
		}
	}

	// Capture latest block so when know where to watch from
	latestBlock, err := w.conn.LatestBlock()
	if err != nil {
		w.log.Error("Unable to fetch latest block", "err", err)
		return false
	}

	// Watch for execution event
	go w.watchThenExecute(m, data, dataHash, latestBlock)

	w.voteProposal(m, dataHash)

	return true
}

// watchThenExecute watches for the latest block and executes once the matching finalized event is found
func (w *writer) watchThenExecute(m msg.Message, data []byte, dataHash [32]byte, latestBlock *big.Int) {
	w.log.Info("Watching for finalization event", "src", m.Source, "nonce", m.DepositNonce)

	// Watching for the latest block, querying and matching the finalized event will be retried up to ExecuteBlockWatchLimit times
	for i := 0; i < ExecuteBlockWatchLimit; i++ {
		select {
		case <-w.stop:
			return
		default:
			// Watch for the lastest block, retry up to BlockRetryLimit times
			for waitRetrys := 0; waitRetrys < BlockRetryLimit; waitRetrys++ {
				err := w.conn.WaitForBlock(latestBlock, big.NewInt(1))
				if err != nil {
					w.log.Error("Waiting for block failed", "err", err)
					// Exit if retries exceeded
					if waitRetrys+1 == BlockRetryLimit {
						w.log.Error("Waiting for block retries exceeded, shutting down")
						w.sysErr <- ErrFatalQuery
						return
					}
				} else {
					break
				}
			}

			// query for logs
			query := buildQuery(w.cfg.bridgeContract, utils.ProposalEvent, latestBlock, latestBlock)
			evts, err := w.conn.Client().FilterLogs(context.Background(), query)
			if err != nil {
				w.log.Error("Failed to fetch logs", "err", err)
				return
			}

			// Execute the proposal once we find the matching finalized event
			for _, evt := range evts {
				sourceId := common.BytesToHash(evt.Data[:32]).Big().Uint64()
				depositNonce := common.BytesToHash(evt.Data[33:64]).Big().Uint64()
				status := common.BytesToHash(evt.Data[65:96]).Big().Uint64()
				log.Info("Proposal log", "sourceID", sourceId, "depositNonce", depositNonce, "status", status)

				if m.Source == msg.ChainId(sourceId) &&
					m.DepositNonce.Big().Uint64() == depositNonce &&
					utils.IsFinalized(uint8(status)) {
					w.executeProposal(m, data, dataHash)
					return
				} else {
					w.log.Trace("Ignoring event", "src", sourceId, "nonce", depositNonce)
				}
			}
			w.log.Trace("No finalization event found in current block", "block", latestBlock, "src", m.Source, "nonce", m.DepositNonce)
			latestBlock = latestBlock.Add(latestBlock, big.NewInt(1))
		}
	}
	log.Warn("Block watch limit exceeded, skipping execution", "source", m.Source, "dest", m.Destination, "nonce", m.DepositNonce)
}

// voteProposal submits a vote proposal
// a vote proposal will try to be submitted up to the TxRetryLimit times
func (w *writer) voteProposal(m msg.Message, dataHash [32]byte) {
	for i := 0; i < TxRetryLimit; i++ {
		select {
		case <-w.stop:
			return
		default:
			err := w.conn.LockAndUpdateOpts()
			if err != nil {
				w.log.Error("Failed to update tx opts", "err", err)
				continue
			}

			tx, err := w.bridgeContract.VoteProposal(
				w.conn.Opts(),
				uint8(m.Source),
				uint64(m.DepositNonce),
				m.ResourceId,
				dataHash,
			)
			w.conn.UnlockOpts()

			if err == nil {
				w.log.Info("Submitted proposal vote", "tx", tx.Hash(), "src", m.Source, "depositNonce", m.DepositNonce)
				if w.metrics != nil {
					w.metrics.VotesSubmitted.Inc()
				}
				return
			} else if err.Error() == ErrNonceTooLow.Error() || err.Error() == ErrTxUnderpriced.Error() {
				w.log.Debug("Nonce too low, will retry")
				time.Sleep(TxRetryInterval)
			} else {
				w.log.Warn("Voting failed", "source", m.Source, "dest", m.Destination, "depositNonce", m.DepositNonce, "err", err)
				time.Sleep(TxRetryInterval)
			}

			// Verify proposal is still open for voting, otherwise no need to retry
			if w.proposalIsComplete(m.Source, m.DepositNonce, dataHash) {
				w.log.Info("Proposal voting complete on chain", "src", m.Source, "dst", m.Destination, "nonce", m.DepositNonce)
				return
			}
		}
	}
	w.log.Error("Submission of Vote transaction failed", "source", m.Source, "dest", m.Destination, "depositNonce", m.DepositNonce)
	w.sysErr <- ErrFatalTx
}

// executeProposal executes the proposal
func (w *writer) executeProposal(m msg.Message, data []byte, dataHash [32]byte) {
	for i := 0; i < TxRetryLimit; i++ {
		select {
		case <-w.stop:
			return
		default:
			err := w.conn.LockAndUpdateOpts()
			if err != nil {
				w.log.Error("Failed to update nonce", "err", err)
				return
			}

			tx, err := w.bridgeContract.ExecuteProposal(
				w.conn.Opts(),
				uint8(m.Source),
				uint64(m.DepositNonce),
				data,
				m.ResourceId,
			)
			w.conn.UnlockOpts()

			if err == nil {
				w.log.Info("Submitted proposal execution", "tx", tx.Hash(), "src", m.Source, "dst", m.Destination, "nonce", m.DepositNonce)
				//TODO: store DepositNonce
				return
			} else if err.Error() == ErrNonceTooLow.Error() || err.Error() == ErrTxUnderpriced.Error() {
				w.log.Error("Nonce too low, will retry")
				time.Sleep(TxRetryInterval)
			} else {
				w.log.Warn("Execution failed, proposal may already be complete", "err", err)
				time.Sleep(TxRetryInterval)
			}

			// Verify proposal is still open for execution, tx will fail if we aren't the first to execute,
			// but there is no need to retry
			if w.proposalIsFinalized(m.Source, m.DepositNonce, dataHash) {
				w.log.Info("Proposal finalized on chain", "src", m.Source, "dst", m.Destination, "nonce", m.DepositNonce)
				return
			}
		}
	}
	w.log.Error("Submission of Execute transaction failed", "source", m.Source, "dest", m.Destination, "depositNonce", m.DepositNonce)
	w.sysErr <- ErrFatalTx
}
