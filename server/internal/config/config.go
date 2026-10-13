// Package config reads and validates local or authenticated private-network configuration.
package config

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
)

const listenAddrKey = "KAIWU_LISTEN_ADDR"

// Config holds server configuration. Validate must precede resource acquisition.
type Config struct {
	ListenAddr string
	Workbench  Workbench
}

// Load reads configuration without binding sockets or resolving hostnames.
func Load(lookupEnv func(string) (string, bool)) (Config, error) {
	addr, present := lookupEnv(listenAddrKey)
	if !present {
		addr = "127.0.0.1:7331"
	}
	cfg := Config{ListenAddr: addr}
	host, _, _ := net.SplitHostPort(addr)
	if ip, e := netip.ParseAddr(host); e == nil && ip.IsPrivate() && !ip.IsLoopback() {
		var err error
		cfg.Workbench, err = LoadWorkbench(lookupEnv)
		if err != nil {
			return cfg, errors.New(listenAddrKey + ": private-network listening requires valid authenticated workbench configuration")
		}
	}
	return cfg, cfg.Validate()
}

// Validate allows loopback, or a specific private IP with authenticated Web enabled.
// Errors deliberately omit the input, which could contain personal data.
func (cfg Config) Validate() error {
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return errors.New(listenAddrKey + ": expected numeric loopback IP and port")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || addr.Is4In6() || addr.Zone() != "" || (!addr.IsLoopback() && !addr.IsPrivate()) {
		return errors.New(listenAddrKey + ": only numeric loopback or specific private IPs are allowed")
	}
	if !addr.IsLoopback() && (len(cfg.Workbench.Token) < 32 || cfg.Workbench.Validate() != nil) {
		return errors.New(listenAddrKey + ": private-network listening requires valid authenticated workbench configuration")
	}
	if port == "" {
		return errors.New(listenAddrKey + ": port must be between 1 and 65535")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			return errors.New(listenAddrKey + ": port must contain decimal digits only")
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New(listenAddrKey + ": port must be between 1 and 65535")
	}
	return nil
}
