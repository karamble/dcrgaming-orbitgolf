// Package audit is what a third party does with a finished match.
//
// It is the point of the whole design: hand somebody a transcript and the
// roster the escrow committed to, and they reach the outcome themselves,
// trusting neither player. Nothing here talks to a bridge, a wallet or a peer.
package audit

import (
	"fmt"

	"github.com/karamble/dcrgaming-orbitgolf/internal/match"
	"github.com/karamble/dcrgaming-orbitgolf/internal/movelog"
)

// Moves is the move list a chain records, in order.
func Moves(c *movelog.Chain) []match.Move {
	entries := c.Entries()
	out := make([]match.Move, 0, len(entries))
	for _, e := range entries {
		out = append(out, match.Move{Hole: e.Hole, Seat: e.Seat, Shot: e.Shot})
	}
	return out
}

// Match verifies a transcript and replays it.
//
// Two separate checks, and both are needed. Unmarshal establishes that every
// entry is signed by the seat it claims, follows the one before and is numbered
// in order - that the history is the one both seats signed. Replay establishes
// that those moves are legal orbital golf and what they add up to. A transcript
// can be perfectly signed and still describe a move out of turn, and a legal
// move list can be one nobody signed.
func Match(blob []byte, roster movelog.Roster, creator uint32) (*match.Match, error) {
	c, err := movelog.Unmarshal(blob, roster)
	if err != nil {
		return nil, fmt.Errorf("transcript: %w", err)
	}
	m, err := match.Replay(creator, Moves(c))
	if err != nil {
		return nil, fmt.Errorf("replay: %w", err)
	}
	return m, nil
}
