// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package bsc

import (
	"github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/bindings/Bridge"
	"github.com/rjman-self/platdot-utils/core"
	metrics "github.com/rjman-self/platdot-utils/metrics/types"
	"github.com/rjman-self/platdot-utils/msg"
)

var _ core.Writer = &writer{}

var PassedStatus uint8 = 2
var TransferredStatus uint8 = 3
var CancelledStatus uint8 = 4

type writer struct {
	cfg            Config
	conn           Connection
	bridgeContract *Bridge.Bridge // instance of bound receiver bridgeContract
	log            log15.Logger
	stop           <-chan int
	sysErr         chan<- error // Reports fatal error to core
	metrics        *metrics.ChainMetrics
}

// NewWriter creates and returns writer
func NewWriter(conn Connection, cfg *Config, log log15.Logger, stop <-chan int, sysErr chan<- error, m *metrics.ChainMetrics) *writer {
	return &writer{
		cfg:     *cfg,
		conn:    conn,
		log:     log,
		stop:    stop,
		sysErr:  sysErr,
		metrics: m,
	}
}

func (w *writer) start() error {
	w.log.Debug("Starting writer...", "chain", w.cfg.name)
	return nil
}

// setContract adds the bound receiver bridgeContract to the writer
func (w *writer) setContract(bridge *Bridge.Bridge) {
	w.bridgeContract = bridge
}

// ResolveMessage handles any given message based on type
// A bool is returned to indicate failure/success, this should be ignored except for within tests.
func (w *writer) ResolveMessage(m msg.Message) bool {
	w.log.Info("Attempting to resolve message", "type", m.Type, "src", m.Source, "dst", m.Destination, "nonce", m.DepositNonce, "rId", m.ResourceId.Hex(), "recipient", m.Payload[1])
	switch m.Type {
	case msg.FungibleTransfer:
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
