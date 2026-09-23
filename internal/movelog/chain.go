package movelog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/karamble/dcrgaming-sdk/pkg/forfeit"
)

// Roster maps a seat to the log key that seat signs its moves with.
//
// It is the SDK's LogSeats for the table, which is derived from the roster the
// escrow scripts commit to. Nothing softer will do: a Bison Relay group's own
// membership list is administered by one member, is never attested, and can
// differ between members. The escrow roster is agreed on chain by everyone
// whose money is at stake.
type Roster map[uint32][]byte

// Chain is the ordered, signed history of one match.
type Chain struct {
	matchID string
	roster  Roster
	entries []Entry
	head    [32]byte
	seq     uint64
	height  uint32
}

// NewChain starts an empty chain for a match.
func NewChain(matchID string, roster Roster) (*Chain, error) {
	if matchID == "" {
		return nil, fmt.Errorf("a chain needs a match id")
	}
	if len(roster) != 2 {
		return nil, fmt.Errorf("roster has %d seats, this game has exactly 2", len(roster))
	}
	dup := make(Roster, len(roster))
	for seat, key := range roster {
		if len(key) != PubKeyLen {
			return nil, fmt.Errorf("seat %d key is %d bytes, want %d", seat, len(key), PubKeyLen)
		}
		if _, err := secp256k1.ParsePubKey(key); err != nil {
			return nil, fmt.Errorf("seat %d key: %w", seat, err)
		}
		dup[seat] = append([]byte(nil), key...)
	}
	return &Chain{matchID: matchID, roster: dup, head: GenesisHash(matchID)}, nil
}

// Head is the hash the next entry must chain to, and the sequence number it
// must carry.
func (c *Chain) Head() ([32]byte, uint64) { return c.head, c.seq }

// MatchID is the match this chain records, Len how many entries it holds.
func (c *Chain) MatchID() string { return c.matchID }
func (c *Chain) Len() int        { return len(c.entries) }

// Entries is a copy of the chain so far.
func (c *Chain) Entries() []Entry {
	out := append([]Entry(nil), c.entries...)
	for i := range out {
		out[i].Signer = append([]byte(nil), out[i].Signer...)
		out[i].Sig = append([]byte(nil), out[i].Sig...)
	}
	return out
}

// Signer is the key a seat signs with, and whether the seat is at this table.
func (c *Chain) Signer(seat uint32) ([]byte, bool) {
	key, ok := c.roster[seat]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), key...), true
}

// Append takes one entry, checking structure and nothing else.
//
// Structure is: the version it claims, the entry it follows, the number it
// carries, that its signer is the seat's own key from the roster, that block
// heights do not run backwards, and that the signature is real. Whether the
// move was legal orbital golf is the match's question.
func (c *Chain) Append(e *Entry) error {
	if e == nil {
		return fmt.Errorf("no entry")
	}
	if e.PrevHash != c.head {
		return fmt.Errorf("entry %d follows %x, the chain head is %x", e.Seq, e.PrevHash[:4], c.head[:4])
	}
	if e.Seq != c.seq {
		return fmt.Errorf("entry is numbered %d, the chain is at %d", e.Seq, c.seq)
	}
	want, ok := c.roster[e.Seat]
	if !ok {
		return fmt.Errorf("entry %d is from seat %d, which is not at this table", e.Seq, e.Seat)
	}
	if !bytes.Equal(want, e.Signer) {
		return fmt.Errorf("entry %d is signed by a key that is not seat %d's", e.Seq, e.Seat)
	}
	if e.Height < c.height {
		return fmt.Errorf("entry %d reports height %d, after height %d", e.Seq, e.Height, c.height)
	}
	if err := e.Verify(); err != nil {
		return err
	}
	digest, err := e.Hash()
	if err != nil {
		return err
	}
	c.entries = append(c.entries, *e)
	c.head = digest
	c.seq++
	c.height = e.Height
	return nil
}

// Move builds, signs and appends this seat's next entry.
//
// The sequence number comes from the chain, which is what makes one position
// mean one message. A caller that invented its own would be choosing the one
// thing that publishes its key.
func (c *Chain) Move(key *forfeit.LogKey, seat uint32, boardIndex uint8, shot uint32, height uint32) (*Entry, error) {
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
	e := &Entry{
		Version:  Version,
		PrevHash: head,
		Seq:      seq,
		Hole:     boardIndex,
		Seat:     seat,
		Shot:     shot,
		Height:   height,
		Signer:   append([]byte(nil), pub...),
	}
	digest, err := e.Hash()
	if err != nil {
		return nil, err
	}
	sig, err := key.Sign(forfeit.DomainEntry, seq, digest[:])
	if err != nil {
		return nil, err
	}
	e.Sig = sig
	if err := c.Append(e); err != nil {
		return nil, err
	}
	return e, nil
}

// entryJSON is an entry on the wire. JSON renders a transcript for inspection
// and transport; it is never a signature preimage. SigningBytes is.
type entryJSON struct {
	Version  uint16 `json:"version"`
	PrevHash string `json:"prev"`
	Seq      uint64 `json:"seq"`
	Hole     uint8  `json:"hole"`
	Seat     uint32 `json:"seat"`
	Shot     uint32 `json:"shot"`
	Height   uint32 `json:"height"`
	Signer   string `json:"signer"`
	Sig      string `json:"sig"`
}

type transcriptJSON struct {
	Match   string      `json:"match"`
	Entries []entryJSON `json:"entries"`
}

// Marshal writes the transcript.
//
// The roster is deliberately not in it. A verifier takes the roster from the
// table's escrow, never from the same document it is checking.
func (c *Chain) Marshal() ([]byte, error) {
	out := transcriptJSON{Match: c.matchID, Entries: make([]entryJSON, 0, len(c.entries))}
	for i := range c.entries {
		e := &c.entries[i]
		out.Entries = append(out.Entries, entryJSON{
			Version:  e.Version,
			PrevHash: hex.EncodeToString(e.PrevHash[:]),
			Seq:      e.Seq,
			Hole:     e.Hole,
			Seat:     e.Seat,
			Shot:     e.Shot,
			Height:   e.Height,
			Signer:   hex.EncodeToString(e.Signer),
			Sig:      hex.EncodeToString(e.Sig),
		})
	}
	return json.Marshal(out)
}

// Unmarshal rebuilds a chain from a transcript, verifying every entry against
// the roster the caller supplies.
func Unmarshal(blob []byte, roster Roster) (*Chain, error) {
	var in transcriptJSON
	if err := json.Unmarshal(blob, &in); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}
	c, err := NewChain(in.Match, roster)
	if err != nil {
		return nil, err
	}
	for i := range in.Entries {
		te := in.Entries[i]
		prev, err := hex.DecodeString(te.PrevHash)
		if err != nil || len(prev) != 32 {
			return nil, fmt.Errorf("entry %d: previous hash is not 32 bytes of hex", te.Seq)
		}
		signer, err := hex.DecodeString(te.Signer)
		if err != nil {
			return nil, fmt.Errorf("entry %d: signer is not hex", te.Seq)
		}
		sig, err := hex.DecodeString(te.Sig)
		if err != nil {
			return nil, fmt.Errorf("entry %d: signature is not hex", te.Seq)
		}
		e := &Entry{
			Version: te.Version,
			Seq:     te.Seq,
			Hole:    te.Hole,
			Seat:    te.Seat,
			Shot:    te.Shot,
			Height:  te.Height,
			Signer:  signer,
			Sig:     sig,
		}
		copy(e.PrevHash[:], prev)
		if err := c.Append(e); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// EncodeEntry renders one entry for the wire.
//
// The same field encoding a transcript uses, so a move that arrived alone and
// the same move read back out of a transcript are the same bytes. It is not a
// signature preimage; SigningBytes is.
func EncodeEntry(e *Entry) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("no entry")
	}
	if len(e.Sig) != SigLen {
		return nil, fmt.Errorf("signature is %d bytes, want %d", len(e.Sig), SigLen)
	}
	return json.Marshal(entryJSON{
		Version:  e.Version,
		PrevHash: hex.EncodeToString(e.PrevHash[:]),
		Seq:      e.Seq,
		Hole:     e.Hole,
		Seat:     e.Seat,
		Shot:     e.Shot,
		Height:   e.Height,
		Signer:   hex.EncodeToString(e.Signer),
		Sig:      hex.EncodeToString(e.Sig),
	})
}

// DecodeEntry reads one entry off the wire.
//
// It checks shape and nothing else. Whether the entry belongs to this chain,
// is next, and is signed by the seat it claims are all the chain's questions,
// and Append asks them.
func DecodeEntry(blob []byte) (*Entry, error) {
	var in entryJSON
	dec := json.NewDecoder(bytes.NewReader(blob))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("read move: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("trailing bytes after the move")
	}
	prev, err := hex.DecodeString(in.PrevHash)
	if err != nil || len(prev) != 32 {
		return nil, fmt.Errorf("previous hash is not 32 bytes of hex")
	}
	signer, err := hex.DecodeString(in.Signer)
	if err != nil {
		return nil, fmt.Errorf("signer is not hex")
	}
	sig, err := hex.DecodeString(in.Sig)
	if err != nil {
		return nil, fmt.Errorf("signature is not hex")
	}
	e := &Entry{
		Version: in.Version,
		Seq:     in.Seq,
		Hole:    in.Hole,
		Seat:    in.Seat,
		Shot:    in.Shot,
		Height:  in.Height,
		Signer:  signer,
		Sig:     sig,
	}
	copy(e.PrevHash[:], prev)
	return e, nil
}

// MarshalJSON lets an entry be handed straight to the runtime's Send, which
// encodes whatever body a game gives it.
//
// Without this an entry would have to be encoded by the caller, and a caller
// that passed the encoded bytes would have them encoded again - a []byte
// becomes a base64 string in JSON, and the far side reads a string where it
// expected a move.
func (e *Entry) MarshalJSON() ([]byte, error) { return EncodeEntry(e) }

// SeatOf is the seat a log key sits at, checked against the roster the escrow
// committed to rather than against whoever sent the message.
func (c *Chain) SeatOf(key []byte) (uint32, bool) {
	for seat, have := range c.roster {
		if bytes.Equal(have, key) {
			return seat, true
		}
	}
	return 0, false
}
