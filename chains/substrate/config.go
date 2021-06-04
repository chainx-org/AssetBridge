// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	log "github.com/ChainSafe/log15"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rjman-ljm/go-substrate-crypto/ss58"
	"github.com/rjman-ljm/sherpax-utils/msg"
	"strconv"

	"github.com/rjman-ljm/sherpax-utils/core"
)

// Chain specific options
var (
	StartBlockOpt         = "startBlock"
	EndBlockOpt           = "endBlock"
	LostAddressOpt        = "lostAddress"
	UseExtendedCallOpt	  = "useExtendedCall"
	TotalRelayerOpt       = "totalRelayer"
	//OtherRelayerOpt	  = "otherRelayer"
	RelayerIdOpt = "relayerId"
	MultiSigAddressOpt      = "multiSigAddress"
	MaxWeightOpt            = "maxWeight"
	DestIdOpt               = "destId"
	ResourceIdOpt           = "resourceId"
	MultiSigThresholdOpt    = "multiSigThreshold"

	OtherRelayerOpt       = "otherRelayer"
)


func parseStartBlock(cfg *core.ChainConfig) uint64 {
	if blk, ok := cfg.Opts[StartBlockOpt]; ok {
		res, err := strconv.ParseUint(blk, 10, 32)
		if err != nil {
			panic(err)
		}
		return res
	}
	return 0
}
func parseEndBlock(cfg *core.ChainConfig) uint64 {
	if blk, ok := cfg.Opts[EndBlockOpt]; ok {
		res, err := strconv.ParseUint(blk, 10, 32)
		if err != nil {
			panic(err)
		}
		return res
	}
	return 0
}

func parseLostAddress(cfg *core.ChainConfig) string {
	if lostAddress, ok := cfg.Opts[LostAddressOpt]; ok {
		return lostAddress
	} else {
		return ""
	}
}

func parseUseExtended(cfg *core.ChainConfig) bool {
	if b, ok := cfg.Opts[UseExtendedCallOpt]; ok {
		res, err := strconv.ParseBool(b)
		if err != nil {
			panic(err)
		}
		return res
	}
	return false
}

func parseOtherRelayers(cfg *core.ChainConfig) []types.AccountID {
	var otherSignatories []types.AccountID

	for _, relayer := range cfg.OtherRelayer {
		relayerPubkey, err := ss58.DecodeToPub(relayer)
		if err != nil {
			log.Error("otherRelayer set error")
		}
		address := types.NewAddressFromAccountID(relayerPubkey)
		otherSignatories = append(otherSignatories, address.AsAccountID)
	}
	return otherSignatories
}

func parseOtherRelayer(cfg *core.ChainConfig) []types.AccountID {
	var otherSignatories []types.AccountID
	if totalRelayer, ok := cfg.Opts[TotalRelayerOpt]; ok {
		total, _ := strconv.ParseUint(totalRelayer, 10, 32)
		for i := uint64(1); i < total; i++ {
			relayedKey := OtherRelayerOpt + strconv.Itoa(int(i))
			if relayer, ok := cfg.Opts[relayedKey]; ok {
				address, _ := types.NewAddressFromHexAccountID(relayer)
				otherSignatories = append(otherSignatories, address.AsAccountID)
			} else {
				log.Warn("Please set config 'OtherRelayer' from 1 to ...!")
				log.Error("Polkadot OtherRelayer Not Found", "OtherRelayerNumber", i)
			}
		}
	} else {
		//log.Error("Please set config opts 'TotalRelayer'.")
	}
	return otherSignatories
}

func parseMultiSigConfig(cfg *core.ChainConfig) (uint64, uint64, uint16) {
	total := uint64(3)
	current := uint64(1)
	threshold := uint64(2)
	if totalRelayer, ok := cfg.Opts[TotalRelayerOpt]; ok {
		total, _ = strconv.ParseUint(totalRelayer, 10, 32)
	}
	if currentRelayerNumber, ok := cfg.Opts[RelayerIdOpt]; ok {
		current, _ = strconv.ParseUint(currentRelayerNumber, 10, 32)
		if current == 0 {
			log.Error("Please set config opts 'CurrentRelayerNumber' from 1 to ...!")
		}
	}
	if multiSigThreshold, ok := cfg.Opts[MultiSigThresholdOpt]; ok {
		threshold, _ = strconv.ParseUint(multiSigThreshold, 10, 32)
	}
	return total, current, uint16(threshold)
}

func parseMultiSigAddress(cfg *core.ChainConfig) types.AccountID {
	if multiSigAddress, ok := cfg.Opts[MultiSigAddressOpt]; ok {
		multiSigPk, err := ss58.DecodeToPub(multiSigAddress)
		if err != nil {
			log.Error("MultiSigAddressOpt set Error")
		}
		multiSigAccount := types.NewAccountID(multiSigPk)
		return multiSigAccount
	} else {
		//log.Error("Polkadot MultiAddress Not Found")
	}
	return types.AccountID{}
}

func parseMaxWeight(cfg *core.ChainConfig) uint64 {
	if weight, ok := cfg.Opts[MaxWeightOpt]; ok {
		res, _ := strconv.ParseUint(weight, 10, 32)
		return res
	}
	return 2269800000
}

func parseDestId(cfg *core.ChainConfig) msg.ChainId {
	if id, ok := cfg.Opts[DestIdOpt]; ok {
		res, err := strconv.ParseUint(id, 10, 32)
		if err != nil {
			panic(err)
		}
		return msg.ChainId(res)
	}
	return 0
}

func parseResourceId(cfg *core.ChainConfig) msg.ResourceId {
	if resource, ok := cfg.Opts[ResourceIdOpt]; ok {
		return msg.ResourceIdFromSlice(common.FromHex(resource))
	}
	return msg.ResourceIdFromSlice(common.FromHex("0x0000000000000000000000000000000000000000000000000000000000000000"))
}
