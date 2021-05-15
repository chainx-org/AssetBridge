package chainset

import (
	"github.com/JFJun/go-substrate-crypto/ss58"
	utils "github.com/Rjman-self/BBridge/shared/substrate"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/sherpax-utils/msg"
	"github.com/rjman-self/substrate-go/client"
	"github.com/rjman-self/substrate-go/expand"
	"github.com/rjman-self/substrate-go/expand/chainx/xevents"
)

/// ChainId Type
const (
	IdBSC       			msg.ChainId = 2
	IdKusama    			msg.ChainId = 1
	IdPolkadot  			msg.ChainId = 5
	IdChainXBTCV1 			msg.ChainId = 3
	IdChainXPCXV1 			msg.ChainId = 7
	IdChainXBTCV2     		msg.ChainId = 9
	IdChainXPCXV2 			msg.ChainId = 11
)

var NativeLimit msg.ChainId = 100

// IsNativeTransfer Chain id distinguishes Tx types(Native, Fungible...)
func IsNativeTransfer(id msg.ChainId) bool {
	return id <= NativeLimit
}

func (bc *BridgeCore) InitializeClientPrefix(cli *client.Client) {
	switch bc.ChainInfo.Type {
	case PolkadotLike:
		cli.SetPrefix(ss58.PolkadotPrefix)
	case ChainXV1Like:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case ChainXV1AssetLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	case ChainXLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXpcx
	case ChainXAssetLike:
		cli.SetPrefix(ss58.ChainXPrefix)
		cli.Name = expand.ChainXbtc
	default:
		cli.SetPrefix(ss58.PolkadotPrefix)
	}
}

func (bc *BridgeCore) MakeBalanceTransferCall(m msg.Message, meta *types.Metadata, assetId xevents.AssetId) (types.Call, error) {
	recipient := bc.GetSubChainRecipient(m)

	var c types.Call
	sendAmount, err := bc.GetSendToSubChainAmount(m.Payload[0].([]byte), assetId)
	if err != nil {
		return types.Call{}, err
	}

	c, err = types.NewCall(
		meta,
		string(utils.BalancesTransferKeepAliveMethod),
		recipient,
		types.NewUCompact(sendAmount),
	)
	if err != nil {
		return types.Call{}, err
	}
	return c, nil
}

func (bc *BridgeCore) MakeXAssetTransferCall() {

}