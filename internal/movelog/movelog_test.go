package movelog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/karamble/dcrgaming-orbitgolf/internal/movelog"
	"github.com/karamble/dcrgaming-sdk/pkg/forfeit"
)

const matchID = "9bbccbcc99e2421852775868835efd6926eab532fb3286f1051f79f7572bb9b9"

func priv(t *testing.T) *secp256k1.PrivateKey {
	t.Helper()
	k, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return k
}

func logKey(t *testing.T, p *secp256k1.PrivateKey) *forfeit.LogKey {
	t.Helper()
	k, err := forfeit.LogKeyFrom(p, matchID)
	if err != nil {
		t.Fatalf("log key: %v", err)
	}
	k.Remember(book(t))
	return k
}

func book(t *testing.T) *movelog.Book {
	t.Helper()
	b, err := movelog.OpenBook(filepath.Join(t.TempDir(), "book.jsonl"))
	if err != nil {
		t.Fatalf("open book: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// table is two seats with their log keys and a chain over them.
func table(t *testing.T) (*movelog.Chain, [2]*forfeit.LogKey) {
	t.Helper()
	keys := [2]*forfeit.LogKey{logKey(t, priv(t)), logKey(t, priv(t))}
	roster := movelog.Roster{
		0: keys[0].Public().SerializeCompressed(),
		1: keys[1].Public().SerializeCompressed(),
	}
	c, err := movelog.NewChain(matchID, roster)
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	return c, keys
}

func rosterOf(keys [2]*forfeit.LogKey) movelog.Roster {
	return movelog.Roster{
		0: keys[0].Public().SerializeCompressed(),
		1: keys[1].Public().SerializeCompressed(),
	}
}

func TestASignedMoveVerifiesAndChains(t *testing.T) {
	c, keys := table(t)
	head, seq := c.Head()
	e, err := c.Move(keys[0], 0, 0, 3, 100)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if e.Seq != seq || e.PrevHash != head {
		t.Fatal("the entry did not take the head the chain offered")
	}
	if err := e.Verify(); err != nil {
		t.Fatalf("a freshly signed entry does not verify: %v", err)
	}
	if _, err := c.Move(keys[1], 1, 0, 4, 100); err != nil {
		t.Fatalf("the second seat could not move: %v", err)
	}
	if c.Len() != 2 {
		t.Fatalf("the chain holds %d entries, want 2", c.Len())
	}
	if newHead, newSeq := c.Head(); newHead == head || newSeq != 2 {
		t.Fatal("the chain did not advance")
	}
}

func TestATamperedShotBreaksTheSignature(t *testing.T) {
	c, keys := table(t)
	e, err := c.Move(keys[0], 0, 0, 3, 100)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	e.Shot = 4
	if err := e.Verify(); err == nil {
		t.Fatal("a move played into another shot still verified")
	}
}

func TestAnEntryOutOfOrderIsRefused(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	entries := c.Entries()
	skipped := entries[0]
	skipped.Seq = 5
	if err := c.Append(&skipped); err == nil {
		t.Fatal("an entry numbered out of order was appended")
	}
}

func TestAnEntryThatFollowsSomethingElseIsRefused(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	other, otherKeys := table(t)
	e, err := other.Move(otherKeys[0], 0, 0, 3, 100)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := c.Append(e); err == nil {
		t.Fatal("an entry from another table's chain was appended")
	}
}

func TestAnotherSeatsKeyIsRefused(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[1], 0, 0, 3, 100); err == nil {
		t.Fatal("seat 0's move was signed with seat 1's key")
	}
}

func TestASeatNotAtTheTableIsRefused(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 2, 0, 3, 100); err == nil {
		t.Fatal("a third seat moved at a heads-up table")
	}
}

func TestHeightsMayNotRunBackwards(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 500); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := c.Move(keys[1], 1, 0, 4, 499); err == nil {
		t.Fatal("an entry reported a height before the one it follows")
	}
}

func TestATranscriptRebuildsFromTheRosterAlone(t *testing.T) {
	c, keys := table(t)
	for i, seat := range []uint32{0, 1, 0, 1} {
		if _, err := c.Move(keys[seat], seat, 0, uint32(i), 100); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}
	blob, err := c.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again, err := movelog.Unmarshal(blob, rosterOf(keys))
	if err != nil {
		t.Fatalf("a transcript this process just wrote did not verify: %v", err)
	}
	wantHead, wantSeq := c.Head()
	gotHead, gotSeq := again.Head()
	if gotHead != wantHead || gotSeq != wantSeq {
		t.Fatal("the rebuilt chain ends somewhere else")
	}
}

func TestATranscriptWithATamperedMoveIsRefused(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	blob, err := c.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tampered := bytes.Replace(blob, []byte(`"shot":3`), []byte(`"shot":4`), 1)
	if bytes.Equal(tampered, blob) {
		t.Fatal("the test did not change the shot it meant to")
	}
	if _, err := movelog.Unmarshal(tampered, rosterOf(keys)); err == nil {
		t.Fatal("a transcript with an altered move verified")
	}
}

// The mechanism, stated as a test: a client that removed its own book and told
// two peers different stories about one move hands over its signing key.
func TestEquivocationAtOneMoveNumberPublishesTheKey(t *testing.T) {
	p := priv(t)
	signer := p.PubKey().SerializeCompressed()

	entry := func(shot uint8) *movelog.Entry {
		return &movelog.Entry{
			Version:  movelog.Version,
			PrevHash: movelog.GenesisHash(matchID),
			Seq:      7,
			Hole:     1,
			Seat:     0,
			Shot:     uint32(shot),
			Height:   100,
			Signer:   signer,
		}
	}
	three, four := entry(3), entry(4)
	hashA, err := three.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	hashB, err := four.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hashA == hashB {
		t.Fatal("two different shots hashed the same")
	}

	pos := forfeit.Position{Match: matchID, Domain: forfeit.DomainEntry, Seq: 7}
	sigA, err := forfeit.Sign(p, pos, hashA[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigB, err := forfeit.Sign(p, pos, hashB[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	got, err := forfeit.Recover(p.PubKey(), hashA[:], sigA, hashB[:], sigB)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !bytes.Equal(got.Serialize(), p.Serialize()) {
		t.Fatal("the two signatures did not give up the key that made them")
	}
}

// And the other half: an honest client cannot reach that state by accident.
func TestTheBookRefusesADifferentMoveAtAUsedNumber(t *testing.T) {
	key := logKey(t, priv(t))
	a := [32]byte{1}
	b := [32]byte{2}
	if _, err := key.Sign(forfeit.DomainEntry, 7, a[:]); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	if _, err := key.Sign(forfeit.DomainEntry, 7, b[:]); err == nil {
		t.Fatal("the key signed a second, different message at one position")
	}
	if _, err := key.Sign(forfeit.DomainEntry, 7, a[:]); err != nil {
		t.Fatalf("re-signing the same message was refused: %v", err)
	}
}

// The failure the durable book exists for: the process that signed move seven
// is gone, and the one that replaces it must still refuse to sign move seven
// over something else.
func TestABookRemembersAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.jsonl")
	p := priv(t)
	a := [32]byte{1}
	b := [32]byte{2}

	first, err := movelog.OpenBook(path)
	if err != nil {
		t.Fatalf("open book: %v", err)
	}
	before, err := forfeit.LogKeyFrom(p, matchID)
	if err != nil {
		t.Fatalf("log key: %v", err)
	}
	before.Remember(first)
	if _, err := before.Sign(forfeit.DomainEntry, 7, a[:]); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close book: %v", err)
	}

	second, err := movelog.OpenBook(path)
	if err != nil {
		t.Fatalf("reopen book: %v", err)
	}
	defer second.Close()
	after, err := forfeit.LogKeyFrom(p, matchID)
	if err != nil {
		t.Fatalf("log key: %v", err)
	}
	after.Remember(second)
	if _, err := after.Sign(forfeit.DomainEntry, 7, b[:]); err == nil {
		t.Fatal("a restarted client signed move seven over a different move")
	}
}

// A crash mid-append leaves a partial final line. That one is dropped; every
// complete record before it still binds.
func TestATornLastLineIsDroppedAndTheRestStillBinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.jsonl")
	p := priv(t)
	a := [32]byte{1}

	first, err := movelog.OpenBook(path)
	if err != nil {
		t.Fatalf("open book: %v", err)
	}
	key, err := forfeit.LogKeyFrom(p, matchID)
	if err != nil {
		t.Fatalf("log key: %v", err)
	}
	key.Remember(first)
	if _, err := key.Sign(forfeit.DomainEntry, 7, a[:]); err != nil {
		t.Fatalf("first signature: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close book: %v", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("reopen file: %v", err)
	}
	if _, err := f.WriteString(`{"match":"9bbc`); err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()

	second, err := movelog.OpenBook(path)
	if err != nil {
		t.Fatalf("a torn tail made the book unopenable: %v", err)
	}
	defer second.Close()
	if _, ok := second.Used(forfeit.Position{Match: matchID, Domain: forfeit.DomainEntry, Seq: 7}); !ok {
		t.Fatal("the complete record before the torn line was forgotten")
	}
}

func TestAnAbandonmentVerifiesAndNamesBothSeats(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	a, err := c.Abandon(keys[0], 0, 1, movelog.ReasonStall, 0, 244)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if err := a.Verify(); err != nil {
		t.Fatalf("a freshly signed abandonment does not verify: %v", err)
	}
	if err := c.CheckAbandon(a); err != nil {
		t.Fatalf("the chain refused its own abandonment: %v", err)
	}
	if a.Seat != 0 || a.Stalled != 1 {
		t.Fatalf("the abandonment says seat %d gave up on seat %d", a.Seat, a.Stalled)
	}
}

func TestAnAbandonmentAboutAnotherPositionIsRefused(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	a, err := c.Abandon(keys[0], 0, 1, movelog.ReasonStall, 0, 244)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	// The stalled seat moves after all, so the abandonment is about a head
	// the chain has moved past.
	if _, err := c.Move(keys[1], 1, 0, 4, 101); err != nil {
		t.Fatalf("their move: %v", err)
	}
	if err := c.CheckAbandon(a); err == nil {
		t.Fatal("an abandonment about an earlier head was accepted")
	}
}

func TestASeatCannotAbandonItself(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := c.Abandon(keys[0], 0, 0, movelog.ReasonStall, 0, 244); err == nil {
		t.Fatal("a seat gave up on itself")
	}
}

// An abandonment sits at its own position under its own domain, so saying two
// different things about one stall publishes the key - and saying it at the
// same sequence number as a move does not.
func TestTwoDifferentAbandonmentsAtOnePositionPublishTheKey(t *testing.T) {
	p := priv(t)
	signer := p.PubKey().SerializeCompressed()
	build := func(deadline uint32) *movelog.Abandon {
		return &movelog.Abandon{
			Version: movelog.Version, Head: movelog.GenesisHash(matchID), Seq: 4,
			Hole: 0, Seat: 0, Stalled: 1, Reason: movelog.ReasonStall,
			Deadline: deadline, Signer: signer,
		}
	}
	first, second := build(244), build(300)
	hashA, err := first.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	hashB, err := second.Hash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	pos := forfeit.Position{Match: matchID, Domain: forfeit.DomainLeaving, Seq: 4}
	sigA, err := forfeit.Sign(p, pos, hashA[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigB, err := forfeit.Sign(p, pos, hashB[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := forfeit.Recover(p.PubKey(), hashA[:], sigA, hashB[:], sigB)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !bytes.Equal(got.Serialize(), p.Serialize()) {
		t.Fatal("two abandonments at one position did not give up the key")
	}

	// And a move at the same number is a different position entirely: an
	// honest seat signs both without leaking anything.
	key := logKey(t, p)
	if _, err := key.Sign(forfeit.DomainEntry, 4, hashA[:]); err != nil {
		t.Fatalf("a move at the same number was refused: %v", err)
	}
	if _, err := key.Sign(forfeit.DomainLeaving, 4, hashB[:]); err != nil {
		t.Fatalf("an abandonment at the same number was refused: %v", err)
	}
}

// An expiry blames nobody, so it is the one abandonment whose signer may also
// be the seat that owed the move.
func TestAnExpiryMayNameItsOwnSigner(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := c.Abandon(keys[1], 1, 1, movelog.ReasonExpired, 0, 1108); err != nil {
		t.Fatalf("a seat could not report the match out of time on its own turn: %v", err)
	}
}

// The reason is signed over, so a stall cannot be re-read as an expiry.
func TestTheReasonIsCoveredByTheSignature(t *testing.T) {
	c, keys := table(t)
	if _, err := c.Move(keys[0], 0, 0, 3, 100); err != nil {
		t.Fatalf("move: %v", err)
	}
	a, err := c.Abandon(keys[0], 0, 1, movelog.ReasonStall, 0, 244)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	a.Reason = movelog.ReasonExpired
	if err := a.Verify(); err == nil {
		t.Fatal("an accusation of stalling was re-read as the clock running out")
	}
}

// The journal is what lets a match survive the process that was playing it.
func TestAJournalReplaysAMatch(t *testing.T) {
	c, keys := table(t)
	path := filepath.Join(t.TempDir(), "match.journal")
	j, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	for i, seat := range []uint32{0, 1, 0, 1} {
		e, err := c.Move(keys[seat], seat, 0, uint32(i), 100)
		if err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
		if err := j.Commit(e); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	rep, err := again.Replay(matchID, rosterOf(keys))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	wantHead, wantSeq := c.Head()
	gotHead, gotSeq := rep.Chain.Head()
	if gotHead != wantHead || gotSeq != wantSeq {
		t.Fatal("a replayed match ends somewhere else")
	}
	if rep.Pending != nil {
		t.Fatal("a fully committed journal reported an unfinished move")
	}
}

// The window this exists for: the key recorded a position, and the process died
// before the signed entry was written. The move must be re-signed from the
// reserved bytes, not chosen again, or the book refuses the position for good.
func TestAReservedMoveSurvivesToBeSignedAgain(t *testing.T) {
	c, keys := table(t)
	path := filepath.Join(t.TempDir(), "match.journal")
	j, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	head, seq := c.Head()
	intended := &movelog.Entry{
		Version: movelog.Version, PrevHash: head, Seq: seq, Hole: 0, Seat: 0,
		Shot: 3, Height: 100, Signer: keys[0].Public().SerializeCompressed(),
	}
	if err := j.Reserve(intended); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	j.Close()

	again, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	rep, err := again.Replay(matchID, rosterOf(keys))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Pending == nil {
		t.Fatal("an interrupted move was forgotten")
	}
	if rep.Pending.Shot != 3 || rep.Pending.Seq != seq {
		t.Fatalf("the reserved move came back as shot %d at %d", rep.Pending.Shot, rep.Pending.Seq)
	}
	if rep.Chain.Len() != 0 {
		t.Fatal("a reservation was replayed as a played move")
	}
}

func TestAJournalRefusesAnEntryOutsideTheRoster(t *testing.T) {
	c, keys := table(t)
	path := filepath.Join(t.TempDir(), "match.journal")
	j, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	e, err := c.Move(keys[0], 0, 0, 3, 100)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := j.Commit(e); err != nil {
		t.Fatalf("commit: %v", err)
	}
	j.Close()

	// Replayed against a roster that does not hold that key, the entry is
	// refused: a transcript is only as good as the signatures in it.
	other, otherKeys := table(t)
	_ = other
	again, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	if _, err := again.Replay(matchID, rosterOf(otherKeys)); err == nil {
		t.Fatal("a journal replayed against a roster that never signed it")
	}
}

// A crash mid-write leaves a partial line; everything before it still binds.
func TestAJournalDropsATornTail(t *testing.T) {
	c, keys := table(t)
	path := filepath.Join(t.TempDir(), "match.journal")
	j, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	e, err := c.Move(keys[0], 0, 0, 3, 100)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := j.Commit(e); err != nil {
		t.Fatalf("commit: %v", err)
	}
	j.Close()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("reopen file: %v", err)
	}
	if _, err := f.WriteString(`{"k":"ent`); err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()

	again, err := movelog.OpenJournal(path)
	if err != nil {
		t.Fatalf("a torn tail made the journal unopenable: %v", err)
	}
	defer again.Close()
	rep, err := again.Replay(matchID, rosterOf(keys))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if rep.Chain.Len() != 1 {
		t.Fatalf("the complete record before the torn line was lost (%d entries)", rep.Chain.Len())
	}
}
