// Package movelog is the signed, hash-chained record of one match.
//
// Every move a seat makes is an entry signed by that seat and chained to the
// one before, so a finished match can be handed to anybody and checked offline:
// the moves, their order, and who made each one, without trusting either player
// or any server.
//
// The signatures are positional. A seat signs at most one entry at each
// sequence number, and the key it signs with derives its nonce from that
// position and nothing else - so telling one peer that move seven was shot
// three and another that it was shot four produces two signatures over one
// position, and those two signatures publish the signer's private key. That is
// the whole mechanism, and it is why the sequence number must never restart
// inside a match: holes are a field in an entry, never a reset of the counter.
//
// Like the SDK's own log, this package enforces structure - signature, signer,
// sequence, linkage - and nothing else. Whether a seat was entitled to move is
// a question about orbital golf, and answering it here would drag the rules
// into a package that exists to be independent of them. A verifier holds both
// and checks both.
package movelog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
	"github.com/karamble/dcrgaming-sdk/pkg/forfeit"
)

// Version is the entry format version. It is covered by the signature, so a
// verifier cannot be talked into reading one version's bytes as another's.
const Version uint16 = 1

const (
	// PubKeyLen is a compressed secp256k1 public key.
	PubKeyLen = 33
	// SigLen is a consensus Schnorr signature. No trailing hash-type byte:
	// nothing here is spending an output.
	SigLen = forfeit.SigLen
)

// Domain separation. These are frozen hash inputs: changing one invalidates
// every signature ever made under it.
var (
	entryTag = []byte("dcrminigolf/movelog/entry/v1")
	matchTag = []byte("dcrminigolf/movelog/match/v1")
)

// writeField length-prefixes a byte string so no two different field sequences
// can encode to the same bytes.
func writeField(w io.Writer, b []byte) {
	_ = binary.Write(w, binary.BigEndian, uint32(len(b)))
	_, _ = w.Write(b)
}

// GenesisHash is what the first entry of a match chains to.
//
// It binds the chain to its match, which is what stops an entry signed at one
// table being replayed into another at the same sequence number: every later
// entry's hash carries this one transitively.
func GenesisHash(matchID string) [32]byte {
	h := blake256.New()
	h.Write(matchTag)
	writeField(h, []byte(matchID))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Entry is one move.
type Entry struct {
	Version  uint16
	PrevHash [32]byte
	// Seq is the move number within the whole match, across all its holes.
	// It never restarts. See the package comment for what restarting costs.
	Seq  uint64
	Hole uint8
	Seat uint32
	Shot uint32
	// Height is the block height this entry's author saw when it moved.
	//
	// Recorded, not enforced. A verifier can check that heights do not go
	// backwards along the chain and nothing more: a peer that is genuinely
	// behind and one lying about it look identical. What it buys is a record
	// of tempo, which is what a stalled match needs evidence of.
	Height uint32
	Signer []byte // 33-byte compressed log public key
	Sig    []byte // 64-byte Schnorr signature over Hash
}

// checkShape catches the malformed before anything is hashed, so the canonical
// encoding can never be asked to encode something unbounded.
func (e *Entry) checkShape() error {
	if e.Version != Version {
		return fmt.Errorf("entry is version %d, want %d", e.Version, Version)
	}
	if len(e.Signer) != PubKeyLen {
		return fmt.Errorf("signer is %d bytes, want %d", len(e.Signer), PubKeyLen)
	}
	return nil
}

// SigningBytes is the canonical encoding the signature covers.
//
// Fixed and explicit rather than derived from a struct encoder: two
// implementations have to agree byte for byte or every signature between them
// is worthless, and general-purpose encodings leave far too much room to
// disagree about field order, integer width and how a string is escaped.
// Nothing here is negotiable at runtime.
func (e *Entry) SigningBytes() ([]byte, error) {
	if err := e.checkShape(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Write(entryTag)
	_ = binary.Write(&b, binary.BigEndian, e.Version)
	b.Write(e.PrevHash[:])
	_ = binary.Write(&b, binary.BigEndian, e.Seq)
	_ = binary.Write(&b, binary.BigEndian, e.Hole)
	_ = binary.Write(&b, binary.BigEndian, e.Seat)
	_ = binary.Write(&b, binary.BigEndian, e.Shot)
	_ = binary.Write(&b, binary.BigEndian, e.Height)
	writeField(&b, e.Signer)
	return b.Bytes(), nil
}

// Hash is what is signed, and what the next entry chains to.
func (e *Entry) Hash() ([32]byte, error) {
	raw, err := e.SigningBytes()
	if err != nil {
		return [32]byte{}, err
	}
	return blake256.Sum256(raw), nil
}

// Verify checks the entry's own signature against the key it names.
//
// It says nothing about whether that key belongs at this table - the chain
// checks that against the roster the escrow committed to, which is the only
// membership list worth verifying against.
func (e *Entry) Verify() error {
	if len(e.Sig) != SigLen {
		return fmt.Errorf("signature is %d bytes, want %d", len(e.Sig), SigLen)
	}
	digest, err := e.Hash()
	if err != nil {
		return err
	}
	pub, err := secp256k1.ParsePubKey(e.Signer)
	if err != nil {
		return fmt.Errorf("signer key: %w", err)
	}
	sig, err := schnorr.ParseSignature(e.Sig)
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	if !sig.Verify(digest[:], pub) {
		return fmt.Errorf("entry %d is not signed by the key it names", e.Seq)
	}
	return nil
}
