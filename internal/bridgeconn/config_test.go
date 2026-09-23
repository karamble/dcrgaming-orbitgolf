package bridgeconn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pair is a self-signed certificate and its key, in PEM, so Validate is
// exercised against something real rather than a string that looks like PEM.
func pair(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "dcrminigolf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: raw}))
}

func good(t *testing.T) Config {
	t.Helper()
	cert, key := pair(t)
	c := Defaults()
	c.ClientCert, c.ClientKey, c.BridgeCert = cert, key, cert
	return c
}

func TestCompleteSettingsValidate(t *testing.T) {
	if err := good(t).Validate(); err != nil {
		t.Fatalf("a complete configuration was refused: %v", err)
	}
}

func TestDefaultsAreIncompleteRatherThanWrong(t *testing.T) {
	c := Defaults()
	if c.Complete() {
		t.Fatal("a fresh profile reports credentials it does not have")
	}
	if err := c.Validate(); err == nil {
		t.Fatal("a configuration with nothing pasted validated")
	}
}

func TestEachBadFieldIsRefused(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"unknown network":    func(c *Config) { c.Network = "regtest" },
		"host is a name":     func(c *Config) { c.Host = "bridge.local" },
		"port is not a port": func(c *Config) { c.Port = "70000" },
		"empty key":          func(c *Config) { c.ClientKey = "" },
		"bridge cert is not pem": func(c *Config) {
			c.BridgeCert = "-----BEGIN CERTIFICATE----- nope"
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := good(t)
			mut(&c)
			if err := c.Validate(); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// A certificate and a key that are not each other's is the mistake a person
// actually makes: three PEMs pasted into the wrong three fields.
func TestAMismatchedKeyPairIsRefused(t *testing.T) {
	c := good(t)
	_, otherKey := pair(t)
	c.ClientKey = otherKey
	if err := c.Validate(); err == nil {
		t.Fatal("a certificate and an unrelated key were accepted")
	}
}

func TestSettingsSurviveARestartAndAreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "bridge.json")
	want := good(t)
	want.Network = "simnet"
	if err := Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("settings are mode %o; the private key in them is owner-only", mode)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Fatal("what was loaded is not what was saved")
	}
}

func TestAnIncompleteConfigurationIsNotSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.json")
	if err := Save(path, Defaults()); err == nil {
		t.Fatal("settings with nothing pasted were saved")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a refused save left a file behind")
	}
}

func TestMissingSettingsLoadAsDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("a profile with no settings yet is not an error: %v", err)
	}
	if got != Defaults() {
		t.Fatal("a missing file did not load as the defaults")
	}
}
