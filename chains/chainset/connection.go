package chainset

import (
	"bytes"
	"fmt"
	utils "github.com/chainx-org/AssetBridge/shared/substrate"
	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	"github.com/centrifuge/go-substrate-rpc-client/v3/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-ljm/substrate-go/expand/chainx/xevents"
)

type option struct {
	HasValue 		bool
}

type OptionAssetId struct {
	Option	 		option
	Value 			xevents.AssetId
}

func NewOptionAssetId(assetId xevents.AssetId) OptionAssetId {
	return OptionAssetId{option{true}, assetId}
}

func EncodeToBytes(value interface{}) ([]byte, error) {
	var buffer = bytes.Buffer{}
	err := scale.NewEncoder(&buffer).Encode(value)
	if err != nil {
		return buffer.Bytes(), err
	}
	return buffer.Bytes(), nil
}

// queryStorage performs a storage lookup. Arguments may be nil, result must be a pointer.
func (bc *BridgeCore) queryStorage(api *gsrpc.SubstrateAPI, meta *types.Metadata, prefix, method string, arg1, arg2 []byte, result interface{}) (bool, []byte, error) {
	// Fetch account nonce
	key, err := types.CreateStorageKey(meta, prefix, method, arg1, arg2)
	if err != nil {
		return false, nil, err
	}

	raw, err := api.RPC.State.GetStorageRawLatest(key)
	if err != nil {
		return false, nil, err
	}
	if len(*raw) == 0 {
		return false, nil, nil
	}

	result = *raw
	//fmt.Printf("converted data is %v\n", result)

	return true, *raw, nil
}

func (bc *BridgeCore) ResourceIdToAssetId(api *gsrpc.SubstrateAPI, meta *types.Metadata, rId [32]byte) ([]byte, error) {
	var res []byte

	rIdBytes, err := EncodeToBytes(rId)
	if err != nil {
		fmt.Println("encode rId err")
	}

	exists, assetId , err := bc.queryStorage(api, meta, utils.HandlerStoragePrefix, "CurrencyIds", rIdBytes, nil, &res)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("resource %x not found on chain", rId)
	}
	//fmt.Printf("rId %v parse assetId is %v\n", rId, assetId)

	return assetId, nil
}

func (bc *BridgeCore) AssetIdToResourceId(api *gsrpc.SubstrateAPI, meta *types.Metadata, assetId xevents.AssetId) ([]byte, error) {
	//optionAssetId := NewOptionAssetId(assetId)
	assetIdBytes, err := EncodeToBytes(assetId)
	if err != nil {
		fmt.Println("encode optionAssetId err")
	}

	var res []byte
	exists, rId, err := bc.queryStorage(api, meta, utils.HandlerStoragePrefix, "ResourceIds", assetIdBytes, nil, &res)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("resource %x not found on chain", rId)
	}
	//fmt.Printf("assetId %v parse rId is %v\n", assetId, rId)

	return rId, nil
}
