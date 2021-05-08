// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	log "github.com/ChainSafe/log15"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rjman-self/sherpax-utils/msg"
	"strconv"

	"github.com/rjman-self/sherpax-utils/core"
)

func parseStartBlock(cfg *core.ChainConfig) uint64 {
	if blk, ok := cfg.Opts["StartBlock"]; ok {
		res, err := strconv.ParseUint(blk, 10, 32)
		if err != nil {
			panic(err)
		}
		return res
	}
	return 0
}
func parseEndBlock(cfg *core.ChainConfig) uint64 {
	if blk, ok := cfg.Opts["EndBlock"]; ok {
		res, err := strconv.ParseUint(blk, 10, 32)
		if err != nil {
			panic(err)
		}
		return res
	}
	return 0
}

func parseLostAddress(cfg *core.ChainConfig) string {
	if lostAddress, ok := cfg.Opts["LostAddress"]; ok {
		return lostAddress
	} else {
		return ""
	}
}

func parseUseExtended(cfg *core.ChainConfig) bool {
	if b, ok := cfg.Opts["useExtendedCall"]; ok {
		res, err := strconv.ParseBool(b)
		if err != nil {
			panic(err)
		}
		return res
	}
	return false
}

func parseOtherRelayer(cfg *core.ChainConfig) []types.AccountID {
	var otherSignatories []types.AccountID
	if totalRelayer, ok := cfg.Opts["TotalRelayer"]; ok {
		total, _ := strconv.ParseUint(totalRelayer, 10, 32)
		for i := uint64(1); i < total; i++ {
			relayedKey := "OtherRelayer" + string(strconv.Itoa(int(i)))
			if relayer, ok := cfg.Opts[relayedKey]; ok {
				address, _ := types.NewAddressFromHexAccountID(relayer)
				otherSignatories = append(otherSignatories, address.AsAccountID)
			} else {
				log.Warn("Please set config 'OtherRelayer' from 1 to ...!")
				log.Error("Polkadot OtherRelayer Not Found", "OtherRelayerNumber", i)
			}
		}
	} else {
		log.Error("Please set config opts 'TotalRelayer'.")
	}
	return otherSignatories
}

func parseMultiSignConfig(cfg *core.ChainConfig) (uint64, uint64, uint16) {
	total := uint64(3)
	current := uint64(1)
	threshold := uint64(2)
	if totalRelayer, ok := cfg.Opts["TotalRelayer"]; ok {
		total, _ = strconv.ParseUint(totalRelayer, 10, 32)
	}
	if currentRelayerNumber, ok := cfg.Opts["CurrentRelayerNumber"]; ok {
		current, _ = strconv.ParseUint(currentRelayerNumber, 10, 32)
		if current == 0 {
			log.Error("Please set config opts 'CurrentRelayerNumber' from 1 to ...!")
		}
	}
	if multiSignThreshold, ok := cfg.Opts["MultiSignThreshold"]; ok {
		threshold, _ = strconv.ParseUint(multiSignThreshold, 10, 32)
	}
	return total, current, uint16(threshold)
}

func parseMultiSignAddress(cfg *core.ChainConfig) types.AccountID {
	if multisignAddress, ok := cfg.Opts["MultiSignAddress"]; ok {
		multiSignPk, _ := types.HexDecodeString(multisignAddress)
		multiSignAccount := types.NewAccountID(multiSignPk)
		return multiSignAccount
	} else {
		log.Error("Polkadot MultiAddress Not Found")
	}
	return types.AccountID{}
}

func parseUrl(cfg *core.ChainConfig) string {
	if len(cfg.Endpoint) > 0 {
		return cfg.Endpoint
	}
	return ""
}

func parseMaxWeight(cfg *core.ChainConfig) uint64 {
	if weight, ok := cfg.Opts["MaxWeight"]; ok {
		res, _ := strconv.ParseUint(weight, 10, 32)
		return res
	}
	return 2269800000
}

func parseDestId(cfg *core.ChainConfig) msg.ChainId {
	if id, ok := cfg.Opts["DestId"]; ok {
		res, err := strconv.ParseUint(id, 10, 32)
		if err != nil {
			panic(err)
		}
		return msg.ChainId(res)
	}
	return 0
}

func parseResourceId(cfg *core.ChainConfig) msg.ResourceId {
	if resource, ok := cfg.Opts["ResourceId"]; ok {
		return msg.ResourceIdFromSlice(common.FromHex(resource))
	}
	return msg.ResourceIdFromSlice(common.FromHex("0x0000000000000000000000000000000000000000000000000000000000000000"))
}
