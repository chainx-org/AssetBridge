package chainset

const (
	NoneLike = iota
	EthLike
	PlatonLike

	SubLike
	PolkadotLike
	KusamaLike

	// ChainXV1Like ChainX V1 use Address Type
	ChainXV1Like
	ChainXAssetV1Like

	ChainXLike
	ChainXAssetLike
)

/// Chain name constants
const (
	NameUnimplemented		string = "unimplemented"

	NameBSC   				string = "bsc_"
	NameKovan 				string = "kovan_"
	NameHeco  				string = "heco_"
	NameETH   				string = "eth_"

	NamePlaton				string = "platon_"
	NameAlaya				string = "alaya_"

	NameKusama				string = "kusama_"
	NamePolkadot			string = "polkadot_"

	NameChainXV1			string = "chainx_v1_"
	NameChainXAssetV1		string = "chainx_asset_v1_"

	NameChainXAsset			string = "chainx_asset_"
	NameChainXPCX			string = "chainx_pcx_"
	NameChainX				string = "chainx_"

	NameSherpaXAsset        string = "sherpax_asset_"
	NameSherpaXPCX          string = "sherpax_pcx_"
	NameSherpaX				string = "sherpax_"
)

const(
	TokenBNB	string = "BNB"
	TokenETH	string = "ETH"
	TokenHT		string = "HT"

	TokenDOT	string = "DOT"
	TokenKSM	string = "KSM"
	TokenPCX	string = "PCX"

	TokenXBTC	string = "XBTC"
	TokenXBNB	string = "XBNB"
	TokenXETH	string = "XETH"
	TokenXUSD	string = "XUSD"
	TokenXHT	string = "XHT"
	TokenXDOT	string = "XDOT"
	TokenXKSM	string = "XKSM"

	TokenXAsset string = "XASSET"
)

type ChainInfo struct{
	Prefix 					string
	NativeToken 			string
	Type 					ChainType
}

var (
	ChainSets = [...]ChainInfo{
		{ NameBSC, 			TokenBNB, EthLike },
		{ NameKovan, 			TokenETH, EthLike },
		{ NameHeco, 			TokenETH, EthLike },
		{ NameETH, 			TokenHT, 	EthLike },
		{ NameKusama, 		TokenKSM, KusamaLike },
		{ NamePolkadot,		TokenDOT, PolkadotLike },
		{ NameChainXV1 , 		TokenPCX, ChainXV1Like},
		{ NameChainXAssetV1,  TokenPCX, ChainXAssetV1Like},
		{ NameChainXAsset,	TokenPCX, ChainXAssetLike},
		{ NameChainXPCX, 		TokenPCX, ChainXLike},
		{ NameChainX, 		TokenPCX, ChainXLike},
		{ NameSherpaXAsset,	TokenPCX, ChainXAssetLike},
		{ NameSherpaXPCX, 	TokenPCX, ChainXLike},
		{ NameSherpaX, 		TokenPCX, ChainXLike},
	}
)
