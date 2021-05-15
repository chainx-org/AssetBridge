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

/// AssetId Type
const (
	AssetXBTC			xevents.AssetId = 1
	AssetXBNB			xevents.AssetId = 2
)