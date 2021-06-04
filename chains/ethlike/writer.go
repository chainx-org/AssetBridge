// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package ethlike

import (
	"github.com/ChainSafe/log15"
	"github.com/chainx-org/AssetBridge/bindings/Bridge"
	"github.com/chainx-org/AssetBridge/bindings/WETH10"
	"github.com/chainx-org/AssetBridge/chains/chainset"
	"github.com/rjman-ljm/sherpax-utils/core"
	"github.com/rjman-ljm/sherpax-utils/crypto/secp256k1"
	metrics "github.com/rjman-ljm/sherpax-utils/metrics/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
)

var _ core.Writer = &writer{}

var PassedStatus uint8 = 2
var TransferredStatus uint8 = 3
var CancelledStatus uint8 = 4

type writer struct {
	cfg            Config
	conn           Connection
	bridgeContract *Bridge.Bridge // instance of bound receiver bridgeContract
	assetContract  *WETH10.WETH10
	kp             secp256k1.Keypair
	log            log15.Logger
	stop           <-chan int
	sysErr         chan<- error // Reports fatal error to core
	metrics        *metrics.ChainMetrics
	bridgeCore     *chainset.BridgeCore
}

// NewWriter creates and returns writer
func NewWriter(conn Connection, cfg *Config, log log15.Logger, kp secp256k1.Keypair, stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics, bc *chainset.BridgeCore) *writer {
	return &writer{
		cfg:     *cfg,
		conn:    conn,
		kp:		 kp,
		log:     log,
		stop:    stop,
		sysErr:  sysErr,
		metrics: m,
		bridgeCore: bc,
	}
}

func (w *writer) start() error {
	w.log.Debug("Starting writer...", "chain", w.cfg.name)
	return nil
}

// setContract adds the bound receiver bridgeContract to the writer
func (w *writer) setContract(bridge *Bridge.Bridge, asset *WETH10.WETH10) {
	w.bridgeContract = bridge
	w.assetContract = asset
}

// ResolveMessage handles any given message based on type
// A bool is returned to indicate failure/success, this should be ignored except for within tests.
func (w *writer) ResolveMessage(m msg.Message) bool {
	w.log.Info("Attempting to resolve message", "type", m.Type, "src", m.Source, "dst", m.Destination, "nonce", m.DepositNonce, "recipient", m.Payload[1])
	switch m.Type {
	case msg.MultiSigTransfer:
		/// Amount Aleady Converted
		return w.createMultiSigProposal(m)
	case msg.NativeTransfer:
		return w.createNativeProposal(m)
	case msg.FungibleTransfer:
		/// Need to convert amount
		return w.createErc20Proposal(m)
	case msg.NonFungibleTransfer:
		return w.createErc721Proposal(m)
	case msg.GenericTransfer:
		return w.createGenericDepositProposal(m)
	default:
		w.log.Error("Unknown message type received", "type", m.Type)
		return false
	}
}
