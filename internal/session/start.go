package session

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
	"github.com/karamble/dcrgaming-orbitgolf/internal/manifest"
	"github.com/karamble/dcrgaming-orbitgolf/internal/match"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/sim"
	"github.com/karamble/dcrgaming-sdk/pkg/forfeit"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/wire"
)

// Ours is the game this build plays. Both seats must state the same one before
// either signs a move.
func Ours() manifest.Manifest {
	return manifest.Manifest{
		Version: manifest.Version, GameVersion: GameVer,
		Holes: match.Holes, StrokeCap: match.StrokeCap,
		SimulationVersion: sim.Version, CourseHash: course.Hash(),
		MoveDeadlineBlocks:  MoveDeadlineBlocks,
		MatchDeadlineBlocks: MatchDeadlineBlocks,
		OpenerRule:          manifest.OpenerAlternating,
		PayoutRule:          manifest.PayoutWinnerTakesPot,
	}
}

// startTag domain-separates what a seat signs to say which game it is playing.
var startTag = []byte("dcrminigolf/start/v1")

// startDigest is what a start signature covers: this table, and this game.
func startDigest(matchID string, m manifest.Manifest) ([32]byte, error) {
	h, err := m.Hash()
	if err != nil {
		return [32]byte{}, err
	}
	b := append(append([]byte{}, startTag...), []byte(matchID)...)
	return blake256.Sum256(append(b, h[:]...)), nil
}

type startMessage struct {
	Manifest manifest.Manifest `json:"manifest"`
	Signer   string            `json:"signer"`
	Sig      string            `json:"sig"`
}

// announceStart says once, durably, which game this seat is playing.
//
// Signed under DomainHead rather than DomainEntry: a move at sequence zero
// already occupies the entry domain, and two different messages at one position
// publish the signing key. Saying two different things about which game is
// being played publishes it too, which is the point - a seat cannot promise one
// rulebook to one peer and a different one to another.
func (g *Game) announceStart(ctx context.Context, sid string) error {
	rt, err := g.runtime()
	if err != nil {
		return err
	}
	t, err := g.table(sid)
	if err != nil {
		return err
	}
	g.mu.Lock()
	if t.startSent {
		g.mu.Unlock()
		return nil
	}
	seat := t.seat
	matchID := t.chain.MatchID()
	g.mu.Unlock()

	ours := Ours()
	digest, err := startDigest(matchID, ours)
	if err != nil {
		return err
	}
	g.mu.Lock()
	key := t.key
	g.mu.Unlock()
	if key == nil {
		return fmt.Errorf("table %s has no signing key", sid)
	}
	sig, err := key.Sign(forfeit.DomainHead, 0, digest[:])
	if err != nil {
		return err
	}
	signer := key.Public().SerializeCompressed()

	g.mu.Lock()
	t.startSent = true
	if t.starts == nil {
		t.starts = map[string]manifest.Manifest{}
	}
	t.starts[hex.EncodeToString(signer)] = ours
	g.mu.Unlock()

	g.mu.Lock()
	noted := t.startNoted
	g.mu.Unlock()
	if noted {
		// Sent before a restart; Bison Relay delivers it, so it is not sent again.
		return nil
	}
	log.Infof("table %s: seat %d stating its rules", sid, seat)
	if err := rt.Send(ctx, sid, KindStart, startMessage{
		Manifest: ours,
		Signer:   hex.EncodeToString(signer),
		Sig:      hex.EncodeToString(sig),
	}, wire.ClassDurable); err != nil {
		return err
	}
	if err := t.journal.Note("start"); err != nil {
		log.Errorf("table %s: the sent rules could not be recorded: %v", sid, err)
	}
	return nil
}

// handleStart takes the other seat's statement of which game it is playing.
func (g *Game) handleStart(t *table, body []byte) error {
	var in startMessage
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("read start: %w", err)
	}
	signer, err := hex.DecodeString(in.Signer)
	if err != nil {
		return fmt.Errorf("start signer is not hex")
	}
	sig, err := hex.DecodeString(in.Sig)
	if err != nil {
		return fmt.Errorf("start signature is not hex")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// The signer must be a seat at this table, checked against the roster the
	// escrow committed to rather than against who the frame arrived from.
	seat, ok := t.chain.SeatOf(signer)
	if !ok {
		return fmt.Errorf("a start arrived from a key that is not at this table")
	}
	if seat == t.seat {
		return fmt.Errorf("a start arrived claiming to be this peer's own seat %d", seat)
	}

	digest, err := startDigest(t.chain.MatchID(), in.Manifest)
	if err != nil {
		return err
	}
	pub, err := secp256k1.ParsePubKey(signer)
	if err != nil {
		return err
	}
	parsed, err := schnorr.ParseSignature(sig)
	if err != nil {
		return err
	}
	if !parsed.Verify(digest[:], pub) {
		return fmt.Errorf("the start is not signed by the key it names")
	}

	key := hex.EncodeToString(signer)
	if t.starts == nil {
		t.starts = map[string]manifest.Manifest{}
	}
	if old, seen := t.starts[key]; seen && !old.Same(in.Manifest) {
		// Sticky, and it stops the table. A seat that stated two different
		// rulebooks has also just published its own signing key.
		t.startConflict = fmt.Sprintf("seat %d stated two different rulebooks", seat)
		return fmt.Errorf("%s", t.startConflict)
	}
	t.starts[key] = in.Manifest
	if !in.Manifest.Same(Ours()) {
		t.startConflict = fmt.Sprintf("seat %d is playing a different rulebook", seat)
		return fmt.Errorf("%s", t.startConflict)
	}
	return nil
}

// StartAgreed reports whether every seat has stated this same game.
func (g *Game) StartAgreed(sid string) bool {
	t, err := g.table(sid)
	if err != nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if t.startConflict != "" || len(t.starts) != int(match.Seats) {
		return false
	}
	ours := Ours()
	for _, m := range t.starts {
		if !m.Same(ours) {
			return false
		}
	}
	return true
}

// StartProblem is why the table stopped, if the seats disagree about the game.
func (g *Game) StartProblem(sid string) string {
	t, err := g.table(sid)
	if err != nil {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return t.startConflict
}

// Start states this seat's rules to the table, once. Called from the driver's
// chain loop after the table is verified.
func (g *Game) Start(ctx context.Context, sid string) error {
	return g.announceStart(ctx, sid)
}

// SignStart builds the wire body of a start message for a given key and
// manifest. Exported so a test can speak as the other seat, which is the only
// way to check what happens when the two disagree.
func SignStart(key *forfeit.LogKey, matchID string, m manifest.Manifest) ([]byte, error) {
	digest, err := startDigest(matchID, m)
	if err != nil {
		return nil, err
	}
	sig, err := key.Sign(forfeit.DomainHead, 0, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(startMessage{
		Manifest: m,
		Signer:   hex.EncodeToString(key.Public().SerializeCompressed()),
		Sig:      hex.EncodeToString(sig),
	})
}
