// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package utils

// An available method on the substrate chain
type Method string

var AddRelayerMethod Method = BridgePalletName + ".add_relayer"
var SetResourceMethod Method = BridgePalletName + ".set_resource"
var SetThresholdMethod Method = BridgePalletName + ".set_threshold"
var WhitelistChainMethod Method = BridgePalletName + ".whitelist_chain"
var ExampleTransferNativeMethod Method = "Example.transfer_native"
var ExampleTransferErc721Method Method = "Example.transfer_erc721"
var ExampleTransferHashMethod Method = "Example.transfer_hash"
var ExampleMintErc721Method Method = "Example.mint_erc721"
var ExampleTransferMethod Method = "Example.transfer"
var ExampleRemarkMethod Method = "Example.remark"
var Erc721MintMethod Method = "Erc721.mint"
var SudoMethod Method = "Sudo.sudo"

var BalancesTransferMethod Method = "Balances.transfer"
var BalancesTransferKeepAliveMethod Method = "Balances.transfer_keep_alive"
var SystemRemark Method = "System.remark"
var UtilityBatch Method = "Utility.batch"
var UtilityBatchAll Method = "Utility.batchall"
var MultisigAsMulti Method = "Multisig.as_multi"

/// ChainX Method
var XAssetsTransferMethod Method = "XAssets.transfer"