package chainset

import (
	"github.com/rjman-self/sherpax-utils/msg"
	"strings"
)

type ChainType int
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
	ChainXV1AssetLike
	ChainXLike
	ChainXAssetLike
)

type BridgeCore struct {
	ChainName		string
	ChainType		ChainType
	ChainBased		string
}

func NewBridgeCore(name string) *BridgeCore {
	prefix := GetChainPrefix(name)
	chainType := GetChainType(prefix)
	basedChain :=

	return &BridgeCore{
		ChainName: 		name,
		ChainType:   	chainType,
		ChainBased: 	basedChain,
	}
}

var NativeLimit msg.ChainId = 100

/// Chain id distinguishes Tx types(Native, Fungible...)
func IsNativeTransfer(id msg.ChainId) bool {
	return id <= NativeLimit
}

func GetChainType(prefix string) ChainType {
	for _, cs := range ChainSets {
		if prefix == cs.Name {
			return cs.Type
		}
	}
	return -1
}

func GetChainPrefix(name string) string {
	for _, j := range chainNameSets {
		if strings.HasPrefix(name, j) {
			return j
		}
	}
	return NameUnimplemented
}

