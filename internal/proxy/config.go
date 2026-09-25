package proxy

import (
	"fmt"
	"net"
)

const SupportedClientBuild = 54261

type Config struct {
	ClientBuild  uint32
	BNetAddress  string
	RESTAddress  string
	WorldAddress string
	LegacyAuth   string
	TLSCertFile  string
	TLSKeyFile   string
	CUFDataDir   string
}

func DefaultConfig() Config {
	return Config{
		ClientBuild:  SupportedClientBuild,
		BNetAddress:  "127.0.0.1:7000",
		RESTAddress:  "127.0.0.1:8081",
		WorldAddress: "127.0.0.1:7002",
		LegacyAuth:   "127.0.0.1:3724",
		CUFDataDir:   "AccountData/redscarf-cuf",
	}
}

func (c Config) Validate() error {
	if c.ClientBuild != SupportedClientBuild {
		return fmt.Errorf("client build %d is unsupported; only 3.4.3.%d is in scope", c.ClientBuild, SupportedClientBuild)
	}
	for name, address := range map[string]string{
		"BNetAddress":  c.BNetAddress,
		"RESTAddress":  c.RESTAddress,
		"WorldAddress": c.WorldAddress,
		"LegacyAuth":   c.LegacyAuth,
	} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return fmt.Errorf("%s %q: %w", name, address, err)
		}
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("TLS certificate and key must be configured together")
	}
	return nil
}
