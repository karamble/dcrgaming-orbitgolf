package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/karamble/dcrgaming-orbitgolf/internal/match"
	"github.com/karamble/dcrgaming-orbitgolf/internal/movelog"
	"github.com/karamble/dcrgaming-sdk/pkg/membership"
)

// Receipt separates signed evidence from locally observed descriptive metadata.
// Roster is a reference, not an independent trust anchor for its own transcript.
type Receipt struct {
	Version          int              `json:"version"`
	Match            string           `json:"match"`
	Table            string           `json:"table"`
	Network          string           `json:"network,omitempty"`
	Terms            membership.Terms `json:"accepted_terms"`
	ExportedAt       time.Time        `json:"exported_at"`
	Transcript       json.RawMessage  `json:"transcript"`
	Roster           movelog.Roster   `json:"reference_roster"`
	Abandonment      json.RawMessage  `json:"signed_abandonment,omitempty"`
	Results          []match.Result   `json:"round_results"`
	Head             string           `json:"chain_head"`
	Complete         bool             `json:"match_complete"`
	BuyIn            int64            `json:"buy_in_atoms"`
	Bond             int64            `json:"bond_atoms"`
	StakeObservation string           `json:"stake_observation"`
	TrustNotice      string           `json:"trust_notice"`
}

func (g *Game) Receipt(sid string) (Receipt, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tables[sid]
	if !ok {
		return Receipt{}, fmt.Errorf("no table %s", sid)
	}
	body, err := t.chain.Marshal()
	if err != nil {
		return Receipt{}, err
	}
	r := Receipt{Version: 1, Match: t.chain.MatchID(), Table: sid, Terms: t.snap.Record.Terms, ExportedAt: time.Now().UTC(), Transcript: body,
		Roster: movelog.Roster{}, Results: t.play.Results(), Head: headHex(t.chain),
		Complete: t.play.Done() || t.abandoned,
		BuyIn:    int64(t.snap.Record.Terms.BuyInAtoms), Bond: int64(t.snap.Record.Terms.BondAtoms),
		TrustNotice: "Verify signed moves against a roster independently obtained from escrow. The included roster is reference material, not proof of escrow membership. Round results, terms, completion and payment observations are descriptive metadata, not signed payment receipts. An incomplete transcript does not prove a completed match; abandonment is separate signed evidence."}
	for seat := uint32(0); seat < 2; seat++ {
		r.Roster[seat], _ = t.chain.Signer(seat)
	}
	if t.abandonEvidence != nil {
		r.Abandonment, err = json.Marshal(t.abandonEvidence)
		if err != nil {
			return Receipt{}, err
		}
	}
	for _, d := range t.snap.Deposits {
		if d.Seat == t.seat && d.Purpose == "stake" {
			r.StakeObservation = d.Check
		}
	}
	return r, nil
}

// Export writes a new owner-only file; identifiers never become path components.
func (r Receipt) Export(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "match-*.json")
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err = enc.Encode(r); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	ok = true
	return filepath.Abs(path)
}
