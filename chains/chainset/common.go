package chainset

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	utils "github.com/chainx-org/AssetBridge/shared/substrate"
	"github.com/rjman-ljm/go-substrate-crypto/ss58"
	"github.com/rjman-ljm/sherpax-utils/msg"
	"github.com/rjman-ljm/substrate-go/client"
	"github.com/rjman-ljm/substrate-go/expand"
	"github.com/rjman-ljm/substrate-go/expand/chainx/xevents"
)

/// ChainId Type
const (
	IdBSC   msg.ChainId = 2
	IdKovan msg.ChainId = 3
	IdHeco  msg.ChainId = 4

	IdKusama    			msg.ChainId = 21
	IdPolkadot  			msg.ChainId = 22
	IdChainXPCXV1 			msg.ChainId = 11
	IdChainXPCXV2 			msg.ChainId = 12
	IdChainXBTCV1 			msg.ChainId = 13
	IdChainXBTCV2     		msg.ChainId = 14
)

var MultiSigLimit msg.ChainId = 100
var SubstrateLimit msg.ChainId = 200

const XParameter uint8 = 255

// IsNativeTransfer Chain id distinguishes Tx types(Native, Fungible...)
func IsMultiSigTransfer(id msg.ChainId) bool {
	return id <= MultiSigLimit
}
func IsFungibleTransfer(id msg.ChainId) bool {
	return !IsMultiSigTransfer(id) && !IsSubstrateTransfer(id)
}

func IsSubstrateTransfer(id msg.ChainId) bool {
	return id >= SubstrateLimit
}

func (bc *BridgeCore) InitializeClientPrefix(cli *client.Client) {
	switch bc.ChainInfo.Type {
	case PolkadotLike:
		cli.SetPrefix(ss58.PolkadotPrefix)
	case ChainXV1Like:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ClientNameChainX
	case ChainXAssetV1Like:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ClientNameChainXAsset
	case ChainXLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ClientNameChainX
	case ChainXAssetLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ClientNameChainXAsset
	default:
		cli.SetPrefix(ss58.PolkadotPrefix)
	}
}

func (bc *BridgeCore) MakeCrossChainTansferCall(m msg.Message, meta *types.Metadata, assetId xevents.AssetId) (types.Call, error) {
	switch bc.ChainInfo.Type {
	case ChainXAssetLike:
		return bc.MakeXAssetTransferCall(m, meta, assetId)
	case ChainXAssetV1Like:
		return bc.MakeXAssetTransferCall(m, meta, assetId)
	default:
		return bc.MakeBalanceTransferCall(m, meta, assetId)
	}
}

func (bc *BridgeCore) MakeBalanceTransferCall(m msg.Message, meta *types.Metadata, assetId xevents.AssetId) (types.Call, error) {
	/// Get Recipient
	recipient := bc.GetSubChainRecipient(m)

	/// Get Amount
	sendAmount, err := bc.GetAmountToSub(m.Payload[0].([]byte), assetId)
	if err != nil {
		return types.Call{}, err
	}

	/// Get Call
	var c types.Call
	if bc.ChainInfo.Type == ChainXV1Like {
		c, err = types.NewCall(
			meta,
			string(utils.BalancesTransferKeepAliveMethod),
			XParameter,
			recipient,
			types.NewUCompact(sendAmount),
		)
	} else {
		c, err = types.NewCall(
			meta,
			string(utils.BalancesTransferKeepAliveMethod),
			recipient,
			types.NewUCompact(sendAmount),
		)
	}
	if err != nil {
		return types.Call{}, err
	}

	return c, nil
}

func (bc *BridgeCore) MakeXAssetTransferCall(m msg.Message, meta *types.Metadata, assetId xevents.AssetId) (types.Call, error) {
	/// GetRecipient
	recipient := bc.GetSubChainRecipient(m)

	/// GetAmount
	sendAmount, err := bc.GetAmountToSub(m.Payload[0].([]byte), assetId)
	if err != nil {
		return types.Call{}, err
	}

	/// Get Call
	var c types.Call
	if bc.ChainInfo.Type == ChainXAssetV1Like {
		c, err = types.NewCall(
			meta,
			string(utils.XAssetsTransferMethod),
			XParameter,
			recipient,
			types.NewUCompactFromUInt(uint64(assetId)),
			types.NewUCompact(sendAmount),
		)
	} else {
		c, err = types.NewCall(
			meta,
			string(utils.XAssetsTransferMethod),
			recipient,
			types.NewUCompactFromUInt(uint64(assetId)),
			types.NewUCompact(sendAmount),
		)
	}
	if err != nil {
		return types.Call{}, err
	}

	return c, nil
}