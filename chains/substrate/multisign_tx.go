package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-self/substrate-go/expand"
	"github.com/rjman-self/sherpax-utils/msg"
)

type MultiSignTxId uint64
type BlockNumber int64
type OtherSignatories []string

type MultiSignTx struct {
	BlockNumber   BlockNumber
	MultiSignTxId MultiSignTxId
}

type MultiSigAsMulti struct {
	OriginMsTx     MultiSignTx
	Executed       bool
	Threshold      uint16
	Others         []OtherSignatories
	MaybeTimePoint expand.TimePointSafe32
	DestAddress    string
	DestAmount     string
	StoreCall      bool
	MaxWeight      uint64
	DepositNonce   msg.Nonce
	YesVote        []types.AccountID
}
