// Copyright 2020 ChainSafe Systems
// SPDX-License-Identifier: LGPL-3.0-only

package ethlike

import (
	"testing"

	"github.com/rjman-ljm/sherpax-utils/core"
)

func TestParseChainConfig(t *testing.T) {
	// No opts provided
	input := core.ChainConfig{
		Id:           0,
		Endpoint:     []string{"endpoint"},
		From:         "0x0",
		KeystorePath: "./keys",
		Insecure:     false,
		Opts:         map[string]string{},
	}

	_, err := parseChainConfig(&input)

	if err == nil {
		t.Error("config missing chainId field but no error reported")
	}

	// Empty bridgeContract provided
	input = core.ChainConfig{
		Id:           0,
		Endpoint:     []string{"endpoint"},
		From:         "0x0",
		KeystorePath: "./keys",
		Insecure:     false,
		Opts:         map[string]string{"bridge": ""},
	}

	_, err2 := parseChainConfig(&input)

	if err2 == nil {
		t.Error("config missing chainId field but no error reported")
	}

}

func TestExtraOpts(t *testing.T) {
	input := core.ChainConfig{
		Name:         "chain",
		Id:           1,
		Endpoint:     []string{"endpoint"},
		From:         "0x0",
		KeystorePath: "./keys",
		Insecure:     false,
		Opts: map[string]string{
			"bridge":        "0x1234",
			"gasLimit":      "10",
			"maxGasPrice":   "20",
			"gasMultiplier": "1",
			"http":          "true",
			"incorrect_opt": "error",
		},
	}

	_, err := parseChainConfig(&input)

	if err == nil {
		t.Error("Config should not accept incorrect opts.")
	}
}
