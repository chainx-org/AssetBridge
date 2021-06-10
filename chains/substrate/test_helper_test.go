package substrate

import (
	"github.com/ChainSafe/log15"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/chainx-org/AssetBridge/config"
	utils "github.com/chainx-org/AssetBridge/shared/substrate"
	"github.com/rjman-ljm/sherpax-utils/keystore"
	"github.com/rjman-ljm/sherpax-utils/msg"
)

var TestEndpoint = []string{"ws://127.0.0.1:9944"}

const TestRelayerThreshold = 2
const TestChainId = 1

var AliceKey = keystore.TestKeyRing.SubstrateKeys[keystore.AliceKey].AsKeyringPair()
var BobKey = keystore.TestKeyRing.SubstrateKeys[keystore.BobKey].AsKeyringPair()

var TestLogLevel = log15.LvlTrace
var AliceTestLogger = newTestLogger("Alice")

var ThisChain msg.ChainId = 1
var ForeignChain msg.ChainId = 2

const relayerThreshold = 2

type testContext struct {
	client         *utils.Client
	listener       *listener
	//router         *mockRouter
	writerAlice    *writer
	writerBob      *writer
	latestOutNonce msg.Nonce
	latestInNonce  msg.Nonce
	lSysErr        chan error
	wSysErr        chan error
}

var context testContext

func newTestLogger(name string) log15.Logger {
	tLog := log15.Root().New("chain", name)
	tLog.SetHandler(log15.LvlFilterHandler(TestLogLevel, tLog.GetHandler()))
	return tLog
}

// createAliceConnection creates and starts a connection with the Alice keypair
func createAliceConnection() (*Connection, chan error, error) {
	sysErr := make(chan error)
	alice := NewConnection(TestEndpoint[config.InitialEndPointId], TestEndpoint, "Alice", AliceKey, AliceTestLogger, make(chan int), sysErr)
	err := alice.Connect()
	if err != nil {
		return nil, nil, err
	}
	return alice, sysErr, err
}

// createAliceAndBobConnections creates and calls `Connect()` on two Connections using the Alice and Bob keypairs
func createAliceAndBobConnections() (*Connection, *Connection, chan error, error) {
	alice, sysErr, err := createAliceConnection()
	if err != nil {
		return nil, nil, nil, err
	}

	bob := NewConnection(TestEndpoint[config.InitialEndPointId], TestEndpoint, "Bob", BobKey, AliceTestLogger, make(chan int), sysErr)
	err = bob.Connect()
	if err != nil {
		return nil, nil, nil, err
	}

	return alice, bob, sysErr, nil
}

// getFreeBalance queries the balance for an account, storing the result in `res`
func getFreeBalance(c *Connection, res *types.U128) {
	var acct types.AccountInfo

	ok, err := c.queryStorage("System", "Account", c.key.PublicKey, nil, &acct)
	if err != nil {
		panic(err)
	} else if !ok {
		panic("no account data")
	}
	*res = acct.Data.Free
}
