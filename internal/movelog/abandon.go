package movelog

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
	"github.com/karamble/dcrgaming-sdk/pkg/forfeit"
)

// abandonTag domain-separates an abandonment from an entry, so neither can be
// read as the other.
var abandonTag = []byte("dcrminigolf/movelog/abandon/v1")

// Reason says why a match was given up on. They are different statements and
// the signature covers which one was made: one blames a seat, the other blames
// nobody, and a record that conflated them would accuse people of stalling
// because a clock ran out.
type Reason uint8

const (
	// ReasonStall is the other seat did not move in time.
	ReasonStall Reason = 0
	// ReasonExpired is the match itself ran out of time. Whosever turn it
	// was, nobody is accused of anything.
	ReasonExpired Reason = 1
)

func (r Reason) String() string {
	if r == ReasonExpired {
		return "expired"
	}
	return "stall"
}

// Abandon is a seat saying the match is over: either because the other seat
// stopped playing, or because the match ran out of time.
//
// It is signed under forfeit.DomainLeaving - "a seat saying it is getting up" -
// which is the SDK's own word for this and keeps it at a different position
// from any move. One abandonment per owed move number per seat: saying two
// different things about one stall publishes the signer's key, exactly as
// equivocating about a move does.
//
// It is a statement, not an enforcement. Nothing is forfeited, because with the
// SDK's seizure verbs gone there is nothing that can be. What it buys is a
// signed record of when a seat stopped and who said so, and an agreed moment to
// stop waiting.
type Abandon struct {
	Version uint16
	// Head is the chain it was said about, and Seq the move that was owed.
	Head [32]byte
	Seq  uint64
	// Hole is which hole was in play.
	Hole uint8
	// Seat is who said it, Stalled who owed the move, and Reason which of
	// the two statements this is. On an expiry the two seats may be the
	// same: whoever owed the move when the match ran out of time is a fact,
	// not an accusation.
	Seat    uint32
	Stalled uint32
	Reason  Reason
	// Deadline is the block height by which the move was owed. The chain
	// says whether it has passed; a clock would not, and two machines would
	// disagree about it.
	Deadline uint32
	Signer   []byte
	Sig      []byte
}

func (a *Abandon) checkShape() error {
	if a.Version != Version {
		return fmt.Errorf("abandonment is version %d, want %d", a.Version, Version)
	}
	if len(a.Signer) != PubKeyLen {
		return fmt.Errorf("signer is %d bytes, want %d", len(a.Signer), PubKeyLen)
	}
	if a.Reason != ReasonStall && a.Reason != ReasonExpired {
		return fmt.Errorf("abandonment reason %d is not one this game makes", a.Reason)
	}
	if a.Reason == ReasonStall && a.Seat == a.Stalled {
		return fmt.Errorf("seat %d says it stalled itself", a.Seat)
	}
	return nil
}

// SigningBytes is the canonical encoding the signature covers.
func (a *Abandon) SigningBytes() ([]byte, error) {
	if err := a.checkShape(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Write(abandonTag)
	_ = binary.Write(&b, binary.BigEndian, a.Version)
	b.Write(a.Head[:])
	_ = binary.Write(&b, binary.BigEndian, a.Seq)
	_ = binary.Write(&b, binary.BigEndian, a.Hole)
	_ = binary.Write(&b, binary.BigEndian, a.Seat)
	_ = binary.Write(&b, binary.BigEndian, a.Stalled)
	_ = binary.Write(&b, binary.BigEndian, uint8(a.Reason))
	_ = binary.Write(&b, binary.BigEndian, a.Deadline)
	writeField(&b, a.Signer)
	return b.Bytes(), nil
}

// Hash is what is signed.
func (a *Abandon) Hash() ([32]byte, error) {
	raw, err := a.SigningBytes()
	if err != nil {
		return [32]byte{}, err
	}
	return blake256.Sum256(raw), nil
}

// Verify checks the signature against the key the statement names.
func (a *Abandon) Verify() error {
	if len(a.Sig) != SigLen {
		return fmt.Errorf("signature is %d bytes, want %d", len(a.Sig), SigLen)
	}
	digest, err := a.Hash()
	if err != nil {
		return err
	}
	pub, err := secp256k1.ParsePubKey(a.Signer)
	if err != nil {
		return fmt.Errorf("signer key: %w", err)
	}
	sig, err := schnorr.ParseSignature(a.Sig)
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	if !sig.Verify(digest[:], pub) {
		return fmt.Errorf("the abandonment is not signed by the key it names")
	}
	return nil
}

// Abandon builds and signs this seat's abandonment of the current chain head.
//
// The chain is not appended to. An abandonment is about the log rather than in
// it: the move that was owed never arrived, so there is no entry to follow, and
// a chain that grew without one would be a chain nobody could replay.
func (c *Chain) Abandon(key *forfeit.LogKey, seat, stalled uint32, reason Reason, boardIndex uint8, deadline uint32) (*Abandon, error) {
	if key == nil {
		return nil, fmt.Errorf("no log key")
	}
	if key.Match() != c.matchID {
		return nil, fmt.Errorf("log key is bound to match %q, this chain is match %q", key.Match(), c.matchID)
	}
	pub, ok := c.roster[seat]
	if !ok {
		return nil, fmt.Errorf("seat %d is not at this table", seat)
	}
	if !bytes.Equal(pub, key.Public().SerializeCompressed()) {
		return nil, fmt.Errorf("this log key is not seat %d's", seat)
	}
	head, seq := c.Head()
	a := &Abandon{
		Version: Version, Head: head, Seq: seq, Hole: boardIndex,
		Seat: seat, Stalled: stalled, Reason: reason, Deadline: deadline,
		Signer: append([]byte(nil), pub...),
	}
	digest, err := a.Hash()
	if err != nil {
		return nil, err
	}
	sig, err := key.Sign(forfeit.DomainLeaving, seq, digest[:])
	if err != nil {
		return nil, err
	}
	a.Sig = sig
	return a, nil
}

// CheckAbandon verifies another seat's abandonment against this chain.
func (c *Chain) CheckAbandon(a *Abandon) error {
	if a == nil {
		return fmt.Errorf("no abandonment")
	}
	head, seq := c.Head()
	if a.Head != head {
		return fmt.Errorf("the abandonment is about %x, this chain is at %x", a.Head[:4], head[:4])
	}
	if a.Seq != seq {
		return fmt.Errorf("the abandonment names move %d, this chain is at %d", a.Seq, seq)
	}
	want, ok := c.roster[a.Seat]
	if !ok {
		return fmt.Errorf("seat %d is not at this table", a.Seat)
	}
	if !bytes.Equal(want, a.Signer) {
		return fmt.Errorf("the abandonment is signed by a key that is not seat %d's", a.Seat)
	}
	if _, ok := c.roster[a.Stalled]; !ok {
		return fmt.Errorf("it names seat %d, which is not at this table", a.Stalled)
	}
	return a.Verify()
}

type abandonJSON struct {
	Version  uint16 `json:"version"`
	Head     string `json:"head"`
	Seq      uint64 `json:"seq"`
	Hole     uint8  `json:"hole"`
	Seat     uint32 `json:"seat"`
	Stalled  uint32 `json:"stalled"`
	Reason   uint8  `json:"reason"`
	Deadline uint32 `json:"deadline"`
	Signer   string `json:"signer"`
	Sig      string `json:"sig"`
}

// MarshalJSON lets an abandonment be handed to the runtime's Send.
func (a *Abandon) MarshalJSON() ([]byte, error) {
	if len(a.Sig) != SigLen {
		return nil, fmt.Errorf("signature is %d bytes, want %d", len(a.Sig), SigLen)
	}
	return json.Marshal(abandonJSON{
		Version: a.Version, Head: hex.EncodeToString(a.Head[:]), Seq: a.Seq,
		Hole: a.Hole, Seat: a.Seat, Stalled: a.Stalled, Reason: uint8(a.Reason),
		Deadline: a.Deadline,
		Signer:   hex.EncodeToString(a.Signer), Sig: hex.EncodeToString(a.Sig),
	})
}

// DecodeAbandon reads one off the wire, checking shape and nothing else.
func DecodeAbandon(blob []byte) (*Abandon, error) {
	var in abandonJSON
	dec := json.NewDecoder(bytes.NewReader(blob))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("read abandonment: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing bytes after the abandonment")
	}
	head, err := hex.DecodeString(in.Head)
	if err != nil || len(head) != 32 {
		return nil, fmt.Errorf("head is not 32 bytes of hex")
	}
	signer, err := hex.DecodeString(in.Signer)
	if err != nil {
		return nil, fmt.Errorf("signer is not hex")
	}
	sig, err := hex.DecodeString(in.Sig)
	if err != nil {
		return nil, fmt.Errorf("signature is not hex")
	}
	a := &Abandon{
		Version: in.Version, Seq: in.Seq, Hole: in.Hole, Seat: in.Seat,
		Stalled: in.Stalled, Reason: Reason(in.Reason), Deadline: in.Deadline,
		Signer: signer, Sig: sig,
	}
	copy(a.Head[:], head)
	return a, nil
}

// LastHeight is the block height the most recent entry reported, and false for
// a chain with no entries yet.
func (c *Chain) LastHeight() (uint32, bool) {
	if len(c.entries) == 0 {
		return 0, false
	}
	return c.entries[len(c.entries)-1].Height, true
}

// FirstHeight is the block height the first entry reported, and false for a
// chain with no entries yet. It is where a match's own clock starts.
func (c *Chain) FirstHeight() (uint32, bool) {
	if len(c.entries) == 0 {
		return 0, false
	}
	return c.entries[0].Height, true
}
