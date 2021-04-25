// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package substrate

import (
	"bytes"
	"github.com/centrifuge/go-substrate-rpc-client/v2/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v2/types"
	"github.com/rjman-self/platdot-utils/msg"
	"github.com/rjman-self/substrate-go/expand/chainx/xevents"
	"math/big"
)

/// AssetId Type
const (
	XBTC			xevents.AssetId = 1
)

var AmountError = MultiSignTx{
	BlockNumber:   -2,
	MultiSignTxId: 0,
}

var NotExecuted = MultiSignTx{
	BlockNumber:   -1,
	MultiSignTxId: 0,
}

var YesVoted = MultiSignTx{
	BlockNumber:   -1,
	MultiSignTxId: 1,
}

type TimePointSafe32 struct {
	Height types.OptionU32
	Index  types.U32
}

type Round struct {
	blockHeight *big.Int
	blockRound  *big.Int
}

type Dest struct {
	DepositNonce msg.Nonce
	DestAddress  string
	DestAmount   string
}

func EncodeCall(call types.Call) []byte {
	var buffer = bytes.Buffer{}
	encoderGoRPC := scale.NewEncoder(&buffer)
	_ = encoderGoRPC.Encode(call)
	return buffer.Bytes()
}
