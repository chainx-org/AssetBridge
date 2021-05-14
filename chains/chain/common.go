package chain

import (
	"github.com/JFJun/go-substrate-crypto/ss58"
	"github.com/Rjman-self/BBridge/config"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/rjman-self/substrate-go/client"
	"github.com/rjman-self/substrate-go/expand"
	"strings"
)

func InitializePrefixByName(name string, cli *client.Client) {
	prefixName := GetChainPrefix(name)
	switch prefixName {
	case config.NameChainXAsset:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case config.NameChainXPCX:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case config.NamePolkadot:
		cli.SetPrefix(ss58.PolkadotPrefix)
	case config.NameKusama:
		cli.SetPrefix(ss58.PolkadotPrefix)
	default:
		cli.SetPrefix(ss58.PolkadotPrefix)
		cli.Name = "Why is there no name?"
	}
}

func InitializePrefixById(id msg.ChainId, cli *client.Client) {
	switch id {
	case config.IdKusama:
		cli.SetPrefix(ss58.PolkadotPrefix)
	case config.IdChainXBTCV1:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case config.IdChainXBTCV2:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case config.IdChainXPCXV1:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case config.IdChainXPCXV2:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case config.IdPolkadot:
		cli.SetPrefix(ss58.PolkadotPrefix)
	default:
		cli.SetPrefix(ss58.PolkadotPrefix)
		cli.Name = "Why is there no name?"
	}
}

func GetChainPrefix(name string) string {
	if strings.HasPrefix(name, config.NameChainXAsset) {
		return config.NameChainXAsset
	} else if strings.HasPrefix(name, config.NameChainXPCX) {
		return config.NameChainXPCX
	} else if strings.HasPrefix(name, config.NameChainX) {
		return config.NameChainX
	} else if strings.HasPrefix(name, config.NameSherpaXAsset) {
		return config.NameSherpaXAsset
	} else if strings.HasPrefix(name, config.NameSherpaXPCX) {
		return config.NameSherpaXPCX
	} else if strings.HasPrefix(name, config.NameSherpaX) {
		return config.NameSherpaX
	} else if strings.HasPrefix(name, config.NameKusama) {
		return config.NameKusama
	} else if strings.HasPrefix(name, config.NamePolkadot) {
		return config.NamePolkadot
	} else if strings.HasPrefix(name, config.NameBSC) {
		return config.NameBSC
	} else if strings.HasPrefix(name, config.NameETH) {
		return config.NameETH
	} else {
		return config.NameUnimplemented
	}
}
