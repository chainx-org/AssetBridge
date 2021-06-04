package substrate
// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

/*
The substrate package contains the logic for interacting with substrate chains.
The current supported transfer types are Fungible, Nonfungible, and generic.

There are 3 major components: the connection, the listener, and the writer.

Connection

The Connection handles connecting to the substrate client, and submitting transactions to the client.
It also handles state queries. The connection is shared by the writer and listener.

Listener

The substrate listener polls blocks and parses the associated events for the three transfer types. It then forwards these into the router.

Writer

As the writer receives messages from the router, it constructs proposals. If a proposal is still active, the writer will attempt to vote on it. Resource IDs are resolved to method name on-chain, which are then used in the proposals when constructing the resulting Call struct.

*/

import (
	"fmt"
	"github.com/ChainSafe/log15"
	"github.com/centrifuge/go-substrate-rpc-client/v3/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/chainx-org/AssetBridge/chains/chainset"
	"github.com/chainx-org/AssetBridge/config"
	"github.com/rjman-ljm/go-substrate-crypto/ss58"
	"github.com/rjman-ljm/sherpax-utils/blockstore"
	"github.com/rjman-ljm/sherpax-utils/core"
	"github.com/rjman-ljm/sherpax-utils/crypto/sr25519"
	"github.com/rjman-ljm/sherpax-utils/keystore"
	metrics "github.com/rjman-ljm/sherpax-utils/metrics/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
)

var _ core.Chain = &Chain{}

type Chain struct {
	cfg      *core.ChainConfig // The config of the chain
	conn     *Connection       // THe chains connection
	listener *listener         // The listener of this chain
	writer   *writer           // The writer of the chain
	stop     chan<- int
}

// checkBlockstore queries the blockStore for the latest known block. If the latest block is
// greater than startBlock, then the latest block is returned, otherwise startBlock is.
func checkBlockstore(bs *blockstore.Blockstore, startBlock uint64) (uint64, error) {
	latestBlock, err := bs.TryLoadLatestBlock()
	if err != nil {
		return 0, err
	}

	if latestBlock.Uint64() > startBlock {
		return latestBlock.Uint64(), nil
	} else {
		return startBlock, nil
	}
}

func InitializeChain(cfg *core.ChainConfig, logger log15.Logger, sysErr chan<- error, m *metrics.ChainMetrics) (*Chain, error) {
	stop := make(chan int)
	/// Load keypair
	fromPubKey, _ := ss58.DecodeToPub(cfg.From)
	kp, err := keystore.KeypairFromAddress(types.HexEncodeToString(fromPubKey), keystore.SubChain, cfg.KeystorePath, cfg.Insecure)
	if err != nil {
		fmt.Printf("keystore not found, addr is %v\n", cfg)
		return nil, err
	}

	krp := kp.(*sr25519.Keypair).AsKeyringPair()

	/// Attempt to load latest block
	bs, err := blockstore.NewBlockstore(cfg.BlockstorePath, cfg.Id, kp.Address())
	if err != nil {
		return nil, err
	}

	startBlock := parseStartBlock(cfg)
	endBlock := parseEndBlock(cfg)
	lostAddress := parseLostAddress(cfg)

	/// Setup connection
	conn := NewConnection(cfg.Endpoint[config.InitialEndPointId], cfg.Endpoint, cfg.Name, (*signature.KeyringPair)(krp), logger, stop, sysErr)
	err = conn.Connect()
	if err != nil {
		return nil, err
	}

	//if cfg.LatestBlock {
	if startBlock == 0 {
		curr, err := conn.api.RPC.Chain.GetHeaderLatest()
		if err != nil {
			return nil, err
		}
		startBlock = uint64(curr.Number)
		log15.Info("Substrate Start block is newest", "StartBlock", startBlock)
	} else {
		log15.Info("Substrate Start block is specified", "StartBlock", startBlock)
	}

	/// Load configuration required by listener and writer
	useExtended := parseUseExtended(cfg)
	otherRelayers := parseOtherRelayers(cfg)
	multiSigAddress := parseMultiSigAddress(cfg)
	total, currentRelayer, threshold := parseMultiSigConfig(cfg)
	weight := parseMaxWeight(cfg)
	dest := parseDestId(cfg)
	resource := parseResourceId(cfg)

	//log15.Info("Initialize ChainInfo", "Prefix", cli.Prefix, "Name", cli.Name, "Id", cfg.Id)
	//fmt.Printf("chain: %v\n", bc.ChainInfo)

	/// Set relayer parameters
	relayer := NewRelayer(*krp, otherRelayers, total, threshold, currentRelayer)

	bc := chainset.NewBridgeCore(cfg.Name)
	bc.InitializeClientPrefix(conn.cli)

	/// Setup listener & writer
	l := NewListener(conn, cfg.Name, cfg.Id, startBlock, endBlock, lostAddress,
		logger, bs, stop, sysErr, m, multiSigAddress, resource, dest, relayer, bc)
	w := NewWriter(conn, l, logger, sysErr, m, useExtended, weight, relayer, bc)

	return &Chain{
		cfg:      cfg,
		conn:     conn,
		listener: l,
		writer:   w,
		stop:     stop,
	}, nil
}

func (c *Chain) Start() error {
	err := c.listener.start()
	if err != nil {
		return err
	}
	c.conn.log.Debug("Successfully started chain", "chainId", c.cfg.Id)
	log15.Info("Successfully started chain", "chainId", c.cfg.Id)
	return nil
}

func (c *Chain) SetRouter(r *core.Router) {
	r.Listen(c.cfg.Id, c.writer)
	c.listener.setRouter(r)
}

func (c *Chain) LatestBlock() metrics.LatestBlock {
	return c.listener.latestBlock
}

func (c *Chain) Id() msg.ChainId {
	return c.cfg.Id
}

func (c *Chain) Name() string {
	return c.cfg.Name
}

func (c *Chain) Stop() {
	close(c.stop)
}
