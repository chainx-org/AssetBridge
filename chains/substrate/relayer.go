package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v2/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v2/types"
)

type Relayer struct {
	kr                 signature.KeyringPair
	otherSignatories   []types.AccountID
	totalRelayers      uint64
	multiSignThreshold uint16
	currentRelayer     uint64
}

func NewRelayer(kr signature.KeyringPair, otherSignatories []types.AccountID, totalRelayers uint64,
	multiSignThreshold uint16, currentRelayer uint64) Relayer {
	return Relayer{
		kr:                 kr,
		otherSignatories:   otherSignatories,
		totalRelayers:      totalRelayers,
		multiSignThreshold: multiSignThreshold,
		currentRelayer:     currentRelayer,
	}
}
