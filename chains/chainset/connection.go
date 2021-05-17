package chainset

import (
	"fmt"
	utils "github.com/Rjman-self/BBridge/shared/substrate"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
)

// queryStorage performs a storage lookup. Arguments may be nil, result must be a pointer.
func (bc *BridgeCore) queryStorage(api *gsrpc.SubstrateAPI, meta *types.Metadata, prefix, method string, arg1, arg2 []byte, result interface{}) (bool, error) {
	// Fetch account nonce
	key, err := types.CreateStorageKey(meta, prefix, method, arg1, arg2)
	if err != nil {
		return false, err
	}

	return api.RPC.State.GetStorageLatest(key, result)
}

func (bc *BridgeCore) ResourceIdToAssetId(api *gsrpc.SubstrateAPI, meta *types.Metadata, rId [32]byte) ([]byte, error) {
	var res []byte
	exists, err := bc.queryStorage(api, meta, utils.HandlerStoragePrefix, "CurrencyIds", rId[:], nil, &res)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, fmt.Errorf("resource %x not found on chain", rId)
	}

	return res, nil
}
