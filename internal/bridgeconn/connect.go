package bridgeconn

import (
	"context"
	"errors"
	"strings"

	"github.com/decred/slog"
	sdkconnect "github.com/karamble/dcrgaming-sdk/pkg/gaming/connect"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/transport"
)

var log = slog.Disabled

// UseLogger configures the subsystem before any connection is opened.
func UseLogger(l slog.Logger) { log = l }

// Connect dials the bridge and proves the credentials, without asking for
// anything: no invitations, no capabilities, no payments, no peer messages.
// It is what the settings screen's Connect button does.
func Connect(ctx context.Context, c Config, id sdkconnect.Identity) (*transport.Bridge, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	addr, _ := c.Address()
	cfg := transport.BridgeConfig{
		Log: log, Addr: addr,
		ClientCert: []byte(c.ClientCert),
		ClientKey:  []byte(c.ClientKey),
		BridgeCert: []byte(c.BridgeCert),
	}
	sdkconnect.Stamp(&cfg, id)
	b, err := transport.Dial(ctx, cfg)
	if err != nil {
		return nil, errors.New("Could not open the secure bridge connection.")
	}
	if err := Check(ctx, b, c.Network, id.GameID); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

// Check confirms the bridge is the one these settings meant: the right game,
// on the right chain.
//
// The network is checked rather than trusted because it decides whether the
// money is real. A bridge that answers for another chain is refused here rather
// than discovered after a stake is funded.
func Check(ctx context.Context, b *transport.Bridge, network, gameID string) error {
	reply, err := b.Hello(ctx, network)
	if err != nil {
		if strings.Contains(err.Error(), "this game is set up for") {
			return errors.New("Network mismatch: the bridge must match the network selected in settings.")
		}
		return errors.New("Bridge unavailable or authentication refused. Check address, port and certificates.")
	}
	if reply.GetGame() != gameID {
		return errors.New("This bridge credential is not registered for dcrminigolf.")
	}
	if reply.GetNetwork() != network {
		return errors.New("Network mismatch: the bridge must match the network selected in settings.")
	}
	return nil
}
