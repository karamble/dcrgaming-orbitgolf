// Package bridgeconn holds dcrminigolf's authenticated connection to a dcrpulse
// gaming bridge: what the operator pasted, where it is kept, and how it is
// proved before anything is played on it.
//
// The three PEM credentials are bridge credentials, not game data. They are
// never passed to a shell, a logger, a renderer or a command-line flag, and the
// file they live in is the operator's to protect.
package bridgeconn

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// MaxPEM bounds a pasted credential so a mistaken paste cannot be unbounded.
const MaxPEM = 32768

// Config is everything needed to reach one bridge.
type Config struct {
	Network    string `json:"network"`
	Host       string `json:"host"`
	Port       string `json:"port"`
	ClientCert string `json:"client_certificate"`
	ClientKey  string `json:"client_private_key"`
	BridgeCert string `json:"bridge_certificate"`
}

// Networks are the chains a bridge may be on. Explicit rather than inferred:
// a game that guessed would be a game that played for mainnet money believing
// it was on simnet.
var Networks = []string{"mainnet", "testnet3", "simnet"}

// Defaults are a fresh profile's settings: dcrpulse's own gaming port, and
// nothing pasted yet.
func Defaults() Config {
	return Config{Network: "mainnet", Host: "127.0.0.1", Port: "8443"}
}

// Address is the host and port to dial.
func (c Config) Address() (string, error) {
	host := strings.Trim(strings.TrimSpace(c.Host), "[]")
	if net.ParseIP(host) == nil {
		return "", errors.New("Enter a valid IPv4 or IPv6 address.")
	}
	port, err := strconv.Atoi(strings.TrimSpace(c.Port))
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("Port must be between 1 and 65535.")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// Validate reports whether these settings could reach a bridge at all.
//
// The key pair is checked here rather than at dial time so a mistyped paste is
// a sentence on the screen instead of a failed handshake.
func (c Config) Validate() error {
	if !known(c.Network) {
		return errors.New("Choose mainnet, testnet3 or simnet for the bridge network.")
	}
	if _, err := c.Address(); err != nil {
		return err
	}
	for _, p := range []string{c.ClientCert, c.ClientKey, c.BridgeCert} {
		if len(p) == 0 || len(p) > MaxPEM {
			return errors.New("Paste all three PEM credentials (maximum 32 KiB each).")
		}
	}
	if _, err := tls.X509KeyPair([]byte(c.ClientCert), []byte(c.ClientKey)); err != nil {
		return errors.New("The client certificate and private key are invalid or do not match.")
	}
	if roots := x509.NewCertPool(); !roots.AppendCertsFromPEM([]byte(c.BridgeCert)) {
		return errors.New("The bridge certificate is not valid PEM.")
	}
	return nil
}

func known(network string) bool {
	for _, n := range Networks {
		if n == network {
			return true
		}
	}
	return false
}

// Complete reports whether anything has been pasted yet, without judging it.
// The lobby uses it to tell "not set up" from "set up and refused".
func (c Config) Complete() bool {
	return c.ClientCert != "" && c.ClientKey != "" && c.BridgeCert != ""
}

// FieldErrors names individual setup problems without echoing credential data.
// Indices match address, port, client certificate, private key, bridge certificate.
func (c Config) FieldErrors() map[int]string {
	out := map[int]string{}
	if net.ParseIP(strings.Trim(strings.TrimSpace(c.Host), "[]")) == nil {
		out[0] = "Enter an IPv4 or IPv6 address"
	}
	p, err := strconv.Atoi(c.Port)
	if err != nil || p < 1 || p > 65535 {
		out[1] = "Port must be between 1 and 65535"
	}
	for i, value := range []string{c.ClientCert, c.ClientKey, c.BridgeCert} {
		if len(value) == 0 {
			out[i+2] = "Paste this credential from dcrpulse"
		} else if len(value) > MaxPEM {
			out[i+2] = "Credential exceeds 32 KiB"
		}
	}
	if out[2] == "" && out[3] == "" {
		if _, err := tls.X509KeyPair([]byte(c.ClientCert), []byte(c.ClientKey)); err != nil {
			out[2] = "Certificate / private key do not match"
			out[3] = "Check this key against the client certificate"
		}
	}
	if out[4] == "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.BridgeCert)) {
		out[4] = "Paste a valid bridge certificate"
	}
	return out
}

// Load reads saved settings, or returns the defaults if there are none.
func Load(path string) (Config, error) {
	c := Defaults()
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, errors.New("Could not read saved bridge settings.")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4*MaxPEM+1))
	if err != nil || len(data) > 4*MaxPEM {
		return c, errors.New("Saved bridge settings are unreadable or too large.")
	}
	if json.Unmarshal(data, &c) != nil {
		return Defaults(), errors.New("Saved bridge settings are invalid.")
	}
	if !known(c.Network) {
		c.Network = "mainnet"
	}
	return c, nil
}

// Save writes the settings with owner-only permissions, through a temporary
// file so a crash mid-write cannot leave half a credential behind.
func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("Could not create the settings directory.")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return errors.New("Could not encode settings.")
	}
	f, err := os.CreateTemp(dir, ".bridge-")
	if err != nil {
		return errors.New("Could not save bridge settings.")
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return errors.New("Could not protect the settings file.")
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errors.New("Could not save bridge settings.")
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return errors.New("Could not replace the saved bridge settings.")
	}
	return nil
}
