module github.com/Rjman-self/BBridge

go 1.15

replace github.com/centrifuge/go-substrate-rpc-client/v2 v2.1.0 => github.com/rjman-self/go-substrate-rpc-client/v2 v2.1.1-0.20210228105504-31eab1ed089b

replace github.com/gogo/protobuf => github.com/regen-network/protobuf v1.3.3-alpha.regen.1

require (
	github.com/ChainSafe/chainbridge-substrate-events v0.0.0-20201109140720-16fa3b0b7ccb
	github.com/ChainSafe/log15 v1.0.0
	github.com/JFJun/go-substrate-crypto v1.0.1
	github.com/btcsuite/btcd v0.21.0-beta // indirect
	github.com/centrifuge/go-substrate-rpc-client v2.0.0+incompatible // indirect
	github.com/centrifuge/go-substrate-rpc-client/v2 v2.1.0
	github.com/ethereum/go-ethereum v1.10.2
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/prometheus/client_golang v1.8.0
	github.com/prometheus/common v0.15.0 // indirect
	github.com/rjman-self/platdot-utils v1.0.9
	github.com/rjman-self/substrate-go v1.5.2
	github.com/rjmand/go-substrate-rpc-client/v2 v2.5.0
	github.com/stretchr/testify v1.7.0
	github.com/urfave/cli/v2 v2.3.0
	google.golang.org/protobuf v1.25.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
