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
package substrate

import (
	"github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/chains/chainset"
	"github.com/centrifuge/go-substrate-rpc-client/v3/signature"
	"github.com/rjman-self/sherpax-utils/blockstore"
	"github.com/rjman-self/sherpax-utils/core"
	"github.com/rjman-self/sherpax-utils/crypto/sr25519"
	"github.com/rjman-self/sherpax-utils/keystore"
	metrics "github.com/rjman-self/sherpax-utils/metrics/types"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/rjman-self/substrate-go/client"
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
	/// Load keypair
	kp, err := keystore.KeypairFromAddress(cfg.From, keystore.SubChain, cfg.KeystorePath, cfg.Insecure)
	if err != nil {
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

	stop := make(chan int)

	/// Setup connection
	conn := NewConnection(cfg.Endpoint, cfg.Name, (*signature.KeyringPair)(krp), logger, stop, sysErr)

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
	otherRelayers := parseOtherRelayer(cfg)
	multiSignAddress := parseMultiSignAddress(cfg)
	total, currentRelayer, threshold := parseMultiSignConfig(cfg)
	weight := parseMaxWeight(cfg)
	url := parseUrl(cfg)
	dest := parseDestId(cfg)
	resource := parseResourceId(cfg)

	cli, err := client.New(url)
	if err != nil {
		panic(err)
	}

	/// Initialize prefix
	//InitializePrefixById(cfg.Id, cli)
	chainset.InitializePrefixByName(cfg.Name, cli)

	log15.Info("Initialize ChainInfo", "Prefix", cli.Prefix, "Name", cli.Name, "Id", cfg.Id)

	/// Set relayer parameters
	relayer := NewRelayer((signature.KeyringPair)(*krp), otherRelayers, total, threshold, currentRelayer)

	/// Setup listener & writer
	l := NewListener(conn, cfg.Name, cfg.Id, startBlock, endBlock, lostAddress,
		logger, bs, stop, sysErr, m, multiSignAddress, cli, resource, dest, relayer)
	w := NewWriter(conn, l, logger, sysErr, m, useExtended, weight, relayer)

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
