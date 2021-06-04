// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package ethlike

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	utils "github.com/chainx-org/AssetBridge/shared/ethlike"
	"github.com/rjman-ljm/sherpax-utils/core"
	"github.com/rjman-ljm/sherpax-utils/msg"
)

const DefaultGasLimit = 6721975
const DefaultGasPrice = 20000000000
const DefaultBlockConfirmations = 10
const DefaultGasMultiplier = 1

// Chain specific options
var (
	AssetOpt			  = "asset"
	BridgeOpt             = "bridge"
	Erc20HandlerOpt       = "erc20Handler"
	Erc721HandlerOpt      = "erc721Handler"
	GenericHandlerOpt     = "genericHandler"
	InternalAccount		  = "internalAccount"
	MaxGasPriceOpt        = "maxGasPrice"
	GasLimitOpt           = "gasLimit"
	GasMultiplier         = "gasMultiplier"
	HttpOpt               = "http"
	StartBlockOpt         = "startBlock"
	EndBlockOpt           = "endBlock"
	BlockConfirmationsOpt = "blockConfirmations"
	PrefixOpt             = "prefix"
	NetworkIdOpt          = "networkId"

)

// Config encapsulates all necessary parameters in ethereum compatible forms
type Config struct {
	name                   string      // Human-readable chain name
	id                     msg.ChainId // ChainID
	endpoint               []string      // url for rpc endpoint
	from                   string      // address of key to use
	keystorePath           string      // Location of keyfiles
	blockstorePath         string
	prefix                 string
	networkId              string // Network Id
	freshStart             bool   // Disables loading from blockstore at start
	bridgeContract         common.Address
	erc20HandlerContract   common.Address
	erc721HandlerContract  common.Address
	genericHandlerContract common.Address
	assetContract          common.Address
	internalAccount		   common.Address
	gasLimit               *big.Int
	maxGasPrice            *big.Int
	gasMultiplier          *big.Float
	http                   bool // Config for type of connection
	startBlock             *big.Int
	endBlock			   *big.Int
	blockConfirmations     *big.Int
}

// parseChainConfig uses a core.ChainConfig to construct a corresponding Config
func parseChainConfig(chainCfg *core.ChainConfig) (*Config, error) {
	http, _ := strconv.ParseBool(chainCfg.Opts["http"])

	config := &Config{
		name:                   chainCfg.Name,
		id:                     chainCfg.Id,
		endpoint:               chainCfg.Endpoint,
		from:                   chainCfg.From,
		keystorePath:           chainCfg.KeystorePath,
		blockstorePath:         chainCfg.BlockstorePath,
		freshStart:             chainCfg.FreshStart,
		bridgeContract:         utils.ZeroAddress,
		erc20HandlerContract:   utils.ZeroAddress,
		erc721HandlerContract:  utils.ZeroAddress,
		genericHandlerContract: utils.ZeroAddress,
		internalAccount: 		utils.ZeroAddress,
		gasLimit:               big.NewInt(DefaultGasLimit),
		maxGasPrice:            big.NewInt(DefaultGasPrice),
		gasMultiplier:          big.NewFloat(DefaultGasMultiplier),
		http:                   http,
		prefix:                 chainCfg.Opts[PrefixOpt],
		networkId:              chainCfg.Opts[NetworkIdOpt],
		startBlock:             big.NewInt(0),
		endBlock:               big.NewInt(0),
		blockConfirmations:     big.NewInt(0),
	}

	//fmt.Printf("load config: http is %v\n prefix is %v\nnetworkId is %v\n id is %v\n", config.http, config.prefix, config.networkId, config.id)
	if contract, ok := chainCfg.Opts[BridgeOpt]; ok && contract != "" {
		config.bridgeContract = common.HexToAddress(contract)
		delete(chainCfg.Opts, BridgeOpt)
	} else {
		return nil, fmt.Errorf("must provide opts.bridge field for ethereum config")
	}

	if contract, ok := chainCfg.Opts[AssetOpt]; ok && contract != "" {
		config.assetContract = common.HexToAddress(contract)
		delete(chainCfg.Opts, AssetOpt)
	}

	config.internalAccount = common.HexToAddress(chainCfg.Opts[InternalAccount])
	delete(chainCfg.Opts, InternalAccount)

	config.erc20HandlerContract = common.HexToAddress(chainCfg.Opts[Erc20HandlerOpt])
	delete(chainCfg.Opts, Erc20HandlerOpt)

	config.erc721HandlerContract = common.HexToAddress(chainCfg.Opts[Erc721HandlerOpt])
	delete(chainCfg.Opts, Erc721HandlerOpt)

	config.genericHandlerContract = common.HexToAddress(chainCfg.Opts[GenericHandlerOpt])
	delete(chainCfg.Opts, GenericHandlerOpt)

	if gasPrice, ok := chainCfg.Opts[MaxGasPriceOpt]; ok {
		price := big.NewInt(0)
		_, pass := price.SetString(gasPrice, 10)
		if pass {
			config.maxGasPrice = price
			delete(chainCfg.Opts, MaxGasPriceOpt)
		} else {
			return nil, errors.New("unable to parse max gas price")
		}
	}

	if gasLimit, ok := chainCfg.Opts[GasLimitOpt]; ok {
		limit := big.NewInt(0)
		_, pass := limit.SetString(gasLimit, 10)
		if pass {
			config.gasLimit = limit
			delete(chainCfg.Opts, GasLimitOpt)
		} else {
			return nil, errors.New("unable to parse gas limit")
		}
	}

	if gasMultiplier, ok := chainCfg.Opts[GasMultiplier]; ok {
		multilier := big.NewFloat(1)
		_, pass := multilier.SetString(gasMultiplier)
		if pass {
			config.gasMultiplier = multilier
			delete(chainCfg.Opts, GasMultiplier)
		} else {
			return nil, errors.New("unable to parse gasMultiplier to float")
		}
	}

	if HTTP, ok := chainCfg.Opts[HttpOpt]; ok && HTTP == "true" {
		config.http = true
		delete(chainCfg.Opts, HttpOpt)
	} else if HTTP, ok := chainCfg.Opts[HttpOpt]; ok && HTTP == "false" {
		config.http = false
		delete(chainCfg.Opts, HttpOpt)
	}

	if startBlock, ok := chainCfg.Opts[StartBlockOpt]; ok && startBlock != "" {
		block := big.NewInt(0)
		_, pass := block.SetString(startBlock, 10)
		if pass {
			config.startBlock = block
			delete(chainCfg.Opts, StartBlockOpt)
		} else {
			return nil, fmt.Errorf("unable to parse %s", StartBlockOpt)
		}
	}

	if endBlock, ok := chainCfg.Opts[EndBlockOpt]; ok && endBlock != "" {
		block := big.NewInt(0)
		_, pass := block.SetString(endBlock, 10)
		if pass {
			config.endBlock = block
			delete(chainCfg.Opts, EndBlockOpt)
		} else {
			return nil, fmt.Errorf("unable to parse %s", EndBlockOpt)
		}
	}

	if blockConfirmations, ok := chainCfg.Opts[BlockConfirmationsOpt]; ok && blockConfirmations != "" {
		val := big.NewInt(DefaultBlockConfirmations)
		_, pass := val.SetString(blockConfirmations, 10)
		if pass {
			config.blockConfirmations = val
			delete(chainCfg.Opts, BlockConfirmationsOpt)
		} else {
			return nil, fmt.Errorf("unable to parse %s", BlockConfirmationsOpt)
		}
	} else {
		config.blockConfirmations = big.NewInt(DefaultBlockConfirmations)
		delete(chainCfg.Opts, BlockConfirmationsOpt)
	}

	if prefix, ok := chainCfg.Opts[PrefixOpt]; ok && prefix != "" {
		config.prefix = prefix
		delete(chainCfg.Opts, PrefixOpt)
	}

	if networkId, ok := chainCfg.Opts[NetworkIdOpt]; ok && networkId != "" {
		config.networkId = networkId
		delete(chainCfg.Opts, NetworkIdOpt)
	}

	if len(chainCfg.Opts) != 0 {
		return nil, fmt.Errorf("unknown Opts Encountered: %#v", chainCfg.Opts)
	}

	return config, nil
}
