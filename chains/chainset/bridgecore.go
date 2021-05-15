package chainset

import (
	"strings"
)

type ChainType int

type BridgeCore struct {
	ChainName		string
	ChainInfo    	*ChainInfo
}

func NewBridgeCore(name string) *BridgeCore {
	prefix := GetChainPrefix(name)
	chainInfo := GetChainInfo(prefix)

	return &BridgeCore{
		ChainName: name,
		ChainInfo: chainInfo,
	}
}

func GetChainInfo(prefix string) *ChainInfo {
	for _, cs := range ChainSets {
		if prefix == cs.Prefix {
			return &cs
		}
	}

	/// Not implemented yet
	return &ChainInfo{
		Prefix: "CheckYourConfigFile",
		NativeToken:  "NONE",
		Type:   NoneLike,
	}
}

func GetChainPrefix(name string) string {
	for _, j := range ChainSets {
		if strings.HasPrefix(name, j.Prefix) {
			return j.Prefix
		}
	}
	return NameUnimplemented
}
