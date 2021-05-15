package chainset

import (
	"github.com/JFJun/go-substrate-crypto/ss58"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/rjman-self/substrate-go/client"
	"github.com/rjman-self/substrate-go/expand"
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

/// Chain name constants
const (
	NameUnimplemented		string = "unimplemented"
	/// EthLike
	NameBSC					string = "bsc"
	NameETH					string = "eth"
	NamePlaton				string = "platon"
	NameAlaya				string = "alaya"
	/// Sub-Based
	/// Kusama
	NameKusama				string = "kusama"
	/// Polkadot
	NamePolkadot			string = "polkadot"
	/// ChainX
	NameChainXAsset			string = "chainxasset"
	NameChainXPCX			string = "chainxpcx"
	NameChainX				string = "chainx"
	/// SherpaX
	NameSherpaXAsset        string = "sherpaxasset"
	NameSherpaXPCX          string = "sherpaxpcx"
	NameSherpaX				string = "sherpax"
	/// ChainX_V1
	NameChainXV1			string = "chainx_v1"
	NameChainXAssetV1		string = "chainxasset_v1"
)

var (
	chainNameSets = [...]string{ NameBSC, NameETH, NameKusama, NamePolkadot, NameChainXAsset, NameChainXPCX, NameChainX,
		NameSherpaXAsset, NameSherpaXPCX, NameSherpaX }
	ChainSets = []struct{
		Name 	string
		Type 	ChainType
	}{
		/// Eth-Like
		{NameBSC, 			EthLike},
		{NameETH, 			EthLike},
		{NamePlaton,          PlatonLike},
		{NameAlaya,           PlatonLike},
		/// Sub-Like
		{NamePolkadot, 		PolkadotLike},
		{NameKusama,			KusamaLike},
		{NameChainXV1, 		ChainXV1Like},
		{NameChainXAssetV1, 	ChainXV1AssetLike},
		{NameChainXAsset, 	ChainXAssetLike},
		{NameChainXPCX, 		ChainXLike},
		{NameChainX, 			ChainXLike},
		{NameSherpaXAsset, 	ChainXAssetLike},
		{NameSherpaXPCX, 		ChainXLike},
		{NameSherpaX,			ChainXLike},
	}
)

func (bc *BridgeCore) InitializeClientPrefix(cli *client.Client) {
	switch bc.ChainType {
	case PolkadotLike:
		cli.SetPrefix(ss58.PolkadotPrefix)
	case ChainXV1Like:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case ChainXV1AssetLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case ChainXLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case ChainXAssetLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	default:
		cli.SetPrefix(ss58.PolkadotPrefix)
	}
}

func (bc *BridgeCore) GetBasedChain() string {
	switch bc.ChainType {
	case ChainXV1Like:
		return NameChainX
	case ChainXV1AssetLike:
		return NameChainX
	case ChainXLike:
		return NameChainX
	case ChainXAssetLike:
		return NameChainX
	case PolkadotLike:
		return NamePolkadot
	case KusamaLike:
		return NameKusama
	default:
		return NameUnimplemented
	}
}