/*
Provides the command-line interface for the ethlike application.
*/
package main

import (
	"errors"
	"fmt"
	log "github.com/ChainSafe/log15"
	"github.com/Rjman-self/BBridge/chains/ethlike"
	"github.com/Rjman-self/BBridge/chains/substrate"
	"github.com/Rjman-self/BBridge/config"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	"github.com/centrifuge/go-substrate-rpc-client/v3/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rjman-self/sherpax-utils/core"
	"github.com/rjman-self/sherpax-utils/metrics/health"
	metrics "github.com/rjman-self/sherpax-utils/metrics/types"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/urfave/cli/v2"
	"net/http"
	"os"
	"strconv"
)

var app = cli.NewApp()

var cliFlags = []cli.Flag{
	config.ConfigFileFlag,
	config.VerbosityFlag,
	config.KeystorePathFlag,
	config.BlockstorePathFlag,
	config.FreshStartFlag,
	config.LatestBlockFlag,
	config.MetricsFlag,
	config.MetricsPort,
}

var generateFlags = []cli.Flag{
	config.PasswordFlag,
	config.Sr25519Flag,
	config.Secp256k1Flag,
	config.SubkeyNetworkFlag,
}

var devFlags = []cli.Flag{
	config.TestKeyFlag,
}

var importFlags = []cli.Flag{
	config.EthereumImportFlag,
	config.PrivateKeyFlag,
	config.Sr25519Flag,
	config.Secp256k1Flag,
	config.PasswordFlag,
	config.SubkeyNetworkFlag,
}

var accountCommand = cli.Command{
	Name:  "accounts",
	Usage: "manage bridge keystore",
	Description: "The accounts command is used to manage the bridge keystore.\n" +
		"\tTo generate a new account (key type generated is determined on the flag passed in): ethlike accounts generate\n" +
		"\tTo import a keystore file: ethlike accounts import path/to/file\n" +
		"\tTo import a geth keystore file: ethlike accounts import --ethereum path/to/file\n" +
		"\tTo import a private key file: ethlike accounts import --privateKey private_key\n" +
		"\tTo list keys: ethlike accounts list",
	Subcommands: []*cli.Command{
		{
			Action: wrapHandler(handleGenerateCmd),
			Name:   "generate",
			Usage:  "generate bridge keystore, key type determined by flag",
			Flags:  generateFlags,
			Description: "The generate subcommand is used to generate the bridge keystore.\n" +
				"\tIf no options are specified, a secp256k1 key will be made.",
		},
		{
			Action: wrapHandler(handleImportCmd),
			Name:   "import",
			Usage:  "import bridge keystore",
			Flags:  importFlags,
			Description: "The import subcommand is used to import a keystore for the bridge.\n" +
				"\tA path to the keystore must be provided\n" +
				"\tUse --ethereum to import an ethereum keystore from external sources such as geth\n" +
				"\tUse --privateKey to create a keystore from a provided private key.",
		},
		{
			Action:      wrapHandler(handleListCmd),
			Name:        "list",
			Usage:       "list bridge keystore",
			Description: "The list subcommand is used to list all of the bridge keystore.\n",
		},
	},
}

var (
	Version = "0.0.1"
)

// init initializes CLI
func init() {
	app.Action = run
	app.Copyright = "Copyright 2021 BBridge Authors"
	app.Name = "BBridge"
	app.Usage = "BBridge"
	app.Authors = []*cli.Author{{Name: "BBridge 2021"}}
	app.Version = Version
	app.EnableBashCompletion = true
	app.Commands = []*cli.Command{
		&accountCommand,
	}
	app.Flags = append(app.Flags, cliFlags...)
	app.Flags = append(app.Flags, devFlags...)
}

func main() {
	sendSimpleTx()
	if err := app.Run(os.Args); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func startLogger(ctx *cli.Context) error {
	logger := log.Root()
	handler := logger.GetHandler()
	var lvl log.Lvl

	if lvlToInt, err := strconv.Atoi(ctx.String(config.VerbosityFlag.Name)); err == nil {
		lvl = log.Lvl(lvlToInt)
	} else if lvl, err = log.LvlFromString(ctx.String(config.VerbosityFlag.Name)); err != nil {
		return err
	}
	log.Root().SetHandler(log.LvlFilterHandler(lvl, handler))

	return nil
}

func run(ctx *cli.Context) error {
	err := startLogger(ctx)
	if err != nil {
		return err
	}

	log.Info("Starting BBridge...")

	cfg, err := config.GetConfig(ctx)
	if err != nil {
		return err
	}

	// Check for test key flag
	var ks string
	var insecure bool
	if key := ctx.String(config.TestKeyFlag.Name); key != "" {
		ks = key
		insecure = true
	} else {
		ks = cfg.KeystorePath
	}

	// Used to signal core shutdown due to fatal error
	sysErr := make(chan error)
	c := core.NewCore(sysErr)

	for _, chain := range cfg.Chains {
		chainId, err := strconv.Atoi(chain.Id)
		if err != nil {
			return err
		}
		chainConfig := &core.ChainConfig{
			Name:           chain.Name,
			Id:             msg.ChainId(chainId),
			Endpoint:       chain.Endpoint,
			From:           chain.From,
			KeystorePath:   ks,
			Insecure:       insecure,
			BlockstorePath: ctx.String(config.BlockstorePathFlag.Name),
			FreshStart:     ctx.Bool(config.FreshStartFlag.Name),
			LatestBlock:    ctx.Bool(config.LatestBlockFlag.Name),
			Opts:           chain.Opts,
		}
		var newChain core.Chain
		var m *metrics.ChainMetrics

		logger := log.Root().New("chain", chainConfig.Name)

		if ctx.Bool(config.MetricsFlag.Name) {
			m = metrics.NewChainMetrics(chain.Name)
		}

		if chain.Type == "ethereum" {
			newChain, err = ethlike.InitializeChain(chainConfig, logger, sysErr, m)
		} else if chain.Type == "substrate" {
			newChain, err = substrate.InitializeChain(chainConfig, logger, sysErr, m)
		} else {
			return errors.New("unrecognized Chain Type")
		}
		if err != nil {
			return err
		}
		c.AddChain(newChain)
	}

	// Start prometheus and health server
	if ctx.Bool(config.MetricsFlag.Name) {
		port := ctx.Int(config.MetricsPort.Name)
		blockTimeoutStr := os.Getenv(config.HealthBlockTimeout)
		blockTimeout := config.DefaultBlockTimeout
		if blockTimeoutStr != "" {
			blockTimeout, err = strconv.ParseInt(blockTimeoutStr, 10, 0)
			if err != nil {
				return err
			}
		}
		h := health.NewHealthServer(port, c.Registry, int(blockTimeout))

		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.HandleFunc("/health", h.HealthStatus)
			err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
			if errors.Is(err, http.ErrServerClosed) {
				log.Info("Health status server is shutting down", err)
			} else {
				log.Error("Error serving metrics", "err", err)
			}
		}()
	}

	c.Start()

	return nil
}

func sendSimpleTx() bool {
	var RelayerSeedOrSecret = "0x68341ec5d0c60361873c98043c1bd7ff840b14d66c518164ac9a95e5fa067443"
	var RelayerPublicKey =  types.MustHexDecodeString("0x0a19674301c56a1721feb98dbe93cfab911a8c1bed127f598ef93b374bcc6e71")
	var RelayerAddress = "5CHwt8bFyDLC3MyzPQugmmxZTGjShBW2kFMWiC2kSL5TuJxd"

	sender := signature.KeyringPair{
		URI:       RelayerSeedOrSecret,
		Address:   RelayerAddress,
		PublicKey: RelayerPublicKey,
	}

	api, err := gsrpc.NewSubstrateAPI("wss://chainx.supercube.pro/ws")
	if err != nil {
		panic(err)
	}

	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		panic(err)
	}

	//serialize signature data
	types.SetSerDeOptions(types.SerDeOptions{NoPalletIndices: true})

	//BEGIN: Create a call of transfer
	method := "Balances.transfer"

	recipient, _ := types.NewMultiAddressFromHexAccountID("0x50a80eb26a7fb43ff4f84ead705fc61c1d4074112e53f781a6b03c0c7504f663")
	amount := types.NewUCompactFromUInt(2000000000)

	c, err := types.NewCall(
		meta,
		method,
		recipient,
		amount,
	)
	if err != nil {
		panic(err)
	}

	ext := types.NewExtrinsic(c)

	genesisHash, err := api.RPC.Chain.GetBlockHash(0)
	if err != nil {
		panic(err)
	}
	rv, err := api.RPC.State.GetRuntimeVersionLatest()
	if err != nil {
		panic(err)
	}

	//key, err := types.CreateStorageKey(meta, "System", "Account", signature.TestKeyringPairAlice.PublicKey, nil)
	key, err := types.CreateStorageKey(meta, "System", "Account", sender.PublicKey, nil)
	if err != nil {
		panic(err)
	}

	var accountInfo types.AccountInfo
	ok, err := api.RPC.State.GetStorageLatest(key, &accountInfo)
	if err != nil || !ok {
		panic(err)
	}

	nonce := uint32(accountInfo.Nonce)

	o := types.SignatureOptions{
		BlockHash:          genesisHash,
		Era:                types.ExtrinsicEra{IsMortalEra: false},
		GenesisHash:        genesisHash,
		Nonce:              types.NewUCompactFromUInt(uint64(nonce)),
		SpecVersion:        rv.SpecVersion,
		Tip:                types.NewUCompactFromUInt(0),
		TransactionVersion: rv.TransactionVersion,
	}

	//var phrase = "outer spike flash urge bus text aim public drink pumpkin pretty loan"

	err = ext.Sign(sender, o)
	if err != nil {
		panic(err)
	}

	sub, err := api.RPC.Author.SubmitAndWatchExtrinsic(ext)
	fmt.Printf("call is\n%v\n", ext)
	enc, _ := types.EncodeToHexString(ext)
	fmt.Printf("hexcall is\n%v\n", enc)
	if err != nil {
		panic(err)
	}
	for {
		status := <-sub.Chan()
		fmt.Printf("Transaction status: %#v\n", status)

		if status.IsFinalized {
			//w.conn.api.
			fmt.Printf("Completed at block hash: %#x\n", status.AsFinalized)
		}
	}
	return true
}