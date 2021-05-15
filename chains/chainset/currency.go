package chainset

import "github.com/rjman-self/substrate-go/expand/chainx/xevents"

// The Eth-Like precision is 18 bits.
//var SingleEthLike = 1e18

// The currency of Sub-Like
const (
	SingleKSM int64 = 1e12
	SingleDOT int64 = 1e10
	SinglePCX int64 = 1e8
)

// The precision-difference constant between Eth-Like and Sub-Like
const(
	DiffKSM    = 1000000     	/// KSM    is 12 digits
	DiffDOT    = 100000000   	/// DOT    is 10 digits
	DiffXBTC   = 10000000000 	/// XBTC   is 8  digits
	DiffPCX    = 10000000000 	/// PCX	   is 8  digits
	DiffXAsset = 10000000000 	/// XAsset is 8  digits
)

/// Fixed handling fee for cross-chain transactions
const (
	FixedKSMFee = SingleKSM * 3 / 100	/// 0.03KSM
	FixedDOTFee = SingleDOT * 5 / 10	/// 0.5DOT
	FixedPCXFee = SinglePCX * 1 / 10	/// 0.1PCX
)

/// Additional formalities rate excluding fixed handling fee
var ExtraFeeRate int64 = 1000

type Currency struct {
	/// Set the token of the native token to zero
	AssetId			xevents.AssetId
	Name			string
	Difference		int64
	FixedFee		int64
	ExtraFeeRate    int64
}

var currencies = []Currency{
	{OriginAsset, 	"KSM", 	DiffKSM, 		FixedKSMFee, ExtraFeeRate},
	{OriginAsset, 	"DOT", 	DiffDOT, 		FixedDOTFee, ExtraFeeRate},
	{OriginAsset, 	"PCX", 	DiffPCX, 		FixedPCXFee, ExtraFeeRate},
	{AssetXBTC, 		"XBTC", 	DiffXBTC, 		0,			 0},
	{AssetXBNB, 		"XBNB", 	DiffXAsset, 	0,			 0},
	{AssetXETH, 		"XETH", 	DiffXAsset, 	0,			 0},
	{AssetXUSD, 		"XUSD", 	DiffXAsset, 	0,			 0},
	{AssetXHT, 		"XHT", 	DiffXAsset, 	0,			 0},
}

/// AssetId Type
const (
	OriginAsset			xevents.AssetId = 0
	AssetXBTC			xevents.AssetId = 1
	AssetXBNB			xevents.AssetId = 2
	AssetXETH			xevents.AssetId = 3
	AssetXUSD			xevents.AssetId = 4
	AssetXHT			xevents.AssetId = 5
)

func (bc *BridgeCore) getCurrency(assetId xevents.AssetId) Currency {
	/// If token has assetId, return ChainX currency
	for _, currency := range currencies {
		if assetId == currency.AssetId {
			return currency
		}
	}

	/// If token is native token, check the from chain
	token := "which_token"
	switch bc.ChainType {
	case :

	}
}
