package chainset

import "github.com/rjman-self/sherpax-utils/msg"

type ChainType uint8

const (
	/// Eth-Like
	EthLike = iota
	PlatonLike

	/// Sub-Like
	SubLike
	PolkadotLike
	KusamaLike
	/// ChainX V1 use Address Type
	ChainXV1Like
	/// ChainX V2 use MultiAddress Type
	ChainXLike
)

type BridgeCore struct {
	ChainType		ChainType
	FromChainId		string
	ToChainId		string
}

var NativeLimit msg.ChainId = 100

/// Chain id distinguishes Tx types(Native, Fungible...)
func IsNativeTransfer(id msg.ChainId) bool {
	return id <= NativeLimit
}