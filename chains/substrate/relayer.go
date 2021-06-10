package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
)

type Relayer struct {
	kr                signature.KeyringPair
	otherSignatories  []types.AccountID
	totalRelayers     uint64
	multiSigThreshold uint16
	relayerId         uint64
	maxWeight		  uint64
}

func NewRelayer(kr signature.KeyringPair, otherSignatories []types.AccountID, totalRelayers uint64,
	multiSigThreshold uint16, currentRelayer uint64, maxWeight uint64) Relayer {
	return Relayer{
		kr:                kr,
		otherSignatories:  otherSignatories,
		totalRelayers:     totalRelayers,
		multiSigThreshold: multiSigThreshold,
		relayerId:         currentRelayer,
		maxWeight:         maxWeight,
	}
}
