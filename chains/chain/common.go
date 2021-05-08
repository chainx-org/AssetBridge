package chain

import (
	"github.com/Rjman-self/BBridge/config"
	"strings"
)

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
