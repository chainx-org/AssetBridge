package chainset

import (
	"github.com/JFJun/go-substrate-crypto/ss58"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/rjman-self/substrate-go/client"
	"github.com/rjman-self/substrate-go/expand"
	"strings"
)

/// ChainId Type
const (
	/// EthLike
	IdBSC       			msg.ChainId = 2
	/// Sub
	IdKusama    			msg.ChainId = 1
	IdPolkadot  			msg.ChainId = 5
	IdChainXBTCV1 			msg.ChainId = 3
	IdChainXPCXV1 			msg.ChainId = 7
	IdChainXBTCV2     		msg.ChainId = 9
	IdChainXPCXV2 			msg.ChainId = 11
)

const (
	NameUnimplemented		string = "unimplemented"
	/// EthLike
	NameBSC					string = "bsc"
	NameETH					string = "eth"

	/// SubBased
	NameKusama				string = "kusama"
	NamePolkadot			string = "polkadot"
	NameChainXAsset			string = "chainxasset"
	NameChainXPCX			string = "chainxpcx"
	NameChainX				string = "chainx"
	NameSherpaXAsset        string = "sherpaxasset"
	NameSherpaXPCX          string = "sherpaxpcx"
	NameSherpaX				string = "sherpax"
)


func InitializePrefixByName(name string, cli *client.Client) {
	prefixName := GetChainPrefix(name)
	switch prefixName {
	case NameChainXAsset:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case NameChainXPCX:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case NamePolkadot:
		cli.SetPrefix(ss58.PolkadotPrefix)
	case NameKusama:
		cli.SetPrefix(ss58.PolkadotPrefix)
	default:
		cli.SetPrefix(ss58.PolkadotPrefix)
		cli.Name = "Why is there no name?"
	}
}

func InitializePrefixById(id msg.ChainId, cli *client.Client) {
	switch id {
	case IdKusama:
		cli.SetPrefix(ss58.PolkadotPrefix)
	case IdChainXBTCV1:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case IdChainXBTCV2:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case IdChainXPCXV1:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case IdChainXPCXV2:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case IdPolkadot:
		cli.SetPrefix(ss58.PolkadotPrefix)
	default:
		cli.SetPrefix(ss58.PolkadotPrefix)
		cli.Name = "Why is there no name?"
	}
}

func GetChainPrefix(name string) string {
	if strings.HasPrefix(name, NameChainXAsset) {
		return NameChainXAsset
	} else if strings.HasPrefix(name, NameChainXPCX) {
		return NameChainXPCX
	} else if strings.HasPrefix(name, NameChainX) {
		return NameChainX
	} else if strings.HasPrefix(name, NameSherpaXAsset) {
		return NameSherpaXAsset
	} else if strings.HasPrefix(name, NameSherpaXPCX) {
		return NameSherpaXPCX
	} else if strings.HasPrefix(name, NameSherpaX) {
		return NameSherpaX
	} else if strings.HasPrefix(name, NameKusama) {
		return NameKusama
	} else if strings.HasPrefix(name, NamePolkadot) {
		return NamePolkadot
	} else if strings.HasPrefix(name, NameBSC) {
		return NameBSC
	} else if strings.HasPrefix(name, NameETH) {
		return NameETH
	} else {
		return NameUnimplemented
	}
}
