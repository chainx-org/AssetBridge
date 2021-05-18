package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rjman-ljm/sherpax-utils/msg"
	"github.com/rjman-ljm/substrate-go/expand"
	"github.com/rjman-ljm/substrate-go/models"
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


func (l *listener) markNew(e *models.ExtrinsicResponse) {
	msTx := MultiSigAsMulti{
		Executed:       false,
		Threshold:      e.MultiSigAsMulti.Threshold,
		MaybeTimePoint: e.MultiSigAsMulti.MaybeTimePoint,
		DestAddress:    e.MultiSigAsMulti.DestAddress,
		DestAmount:     e.MultiSigAsMulti.DestAmount,
		Others:         nil,
		StoreCall:      e.MultiSigAsMulti.StoreCall,
		MaxWeight:      e.MultiSigAsMulti.MaxWeight,
		OriginMsTx:     l.currentTx,
	}
	/// Mark voted
	msTx.Others = append(msTx.Others, e.MultiSigAsMulti.OtherSignatories)
	l.msTxAsMulti[l.currentTx] = msTx
}

func (l *listener) markVote(msTx MultiSigAsMulti, e *models.ExtrinsicResponse) {
	for k, ms := range l.msTxAsMulti {
		if !ms.Executed && ms.DestAddress == msTx.DestAddress && ms.DestAmount == msTx.DestAmount {
			//l.log.Info("relayer succeed vote", "Address", e.FromAddress)
			voteMsTx := l.msTxAsMulti[k]
			voteMsTx.Others = append(voteMsTx.Others, e.MultiSigAsMulti.OtherSignatories)
			l.msTxAsMulti[k] = voteMsTx
		}
	}
}

func (l *listener) markExecution(msTx MultiSigAsMulti) {
	for k, ms := range l.msTxAsMulti {
		if !ms.Executed && ms.DestAddress == msTx.DestAddress && ms.DestAmount == msTx.DestAmount {
			exeMsTx := l.msTxAsMulti[k]
			exeMsTx.Executed = true
			l.msTxAsMulti[k] = exeMsTx
		}
	}
}