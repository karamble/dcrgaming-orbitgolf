// Package payout is what a table holds and who is owed it.
//
// Frozen on purpose. The runtime is asked to settle more than once - on a
// timer, after a restart, after a transport failure - and every proposal has to
// name the same amounts, because the bridge identifies a payout by the
// transaction it builds. Two proposals that differ by one atom are two
// transactions, each collecting half the signatures it needs, and the pot sits
// in escrow until the refund locks mature.
//
// So the amounts are computed once, from what the chain says each seat actually
// staked, and reused verbatim thereafter.
package payout

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/decred/dcrd/crypto/blake256"
)

// Version is the policy format, covered by the identifier.
const Version uint16 = 1

// MaxAtoms is Decred's supply ceiling, used only to refuse arithmetic that has
// already gone wrong somewhere else.
const MaxAtoms int64 = 21e14

var tag = []byte("dcrminigolf/payout/v1")

// Policy is what each seat staked, and therefore what may be paid out.
type Policy struct {
	Version uint16
	Stakes  []int64
}

// Freeze records what the seats actually staked.
//
// The observed amounts, not the buy-in: the runtime only requires a stake to be
// at least the buy-in, so a seat that overpaid would break a policy that
// assumed it, and the settlement must divide exactly what the table holds.
func Freeze(stakes []int64) (Policy, error) {
	p := Policy{Version: Version, Stakes: append([]int64(nil), stakes...)}
	return p, p.Validate()
}

// Validate refuses a policy that could not describe a funded table.
func (p Policy) Validate() error {
	if p.Version != Version {
		return fmt.Errorf("policy is version %d, want %d", p.Version, Version)
	}
	if len(p.Stakes) < 2 {
		return fmt.Errorf("a table has at least two seats, this policy has %d", len(p.Stakes))
	}
	var total int64
	for seat, atoms := range p.Stakes {
		if atoms <= 0 {
			return fmt.Errorf("seat %d staked %d atoms", seat, atoms)
		}
		if total > MaxAtoms-atoms {
			return fmt.Errorf("the stakes total more than exists")
		}
		total += atoms
	}
	return nil
}

// Pot is everything the table holds.
func (p Policy) Pot() int64 {
	var total int64
	for _, atoms := range p.Stakes {
		total += atoms
	}
	return total
}

// Winner is the allocation that pays one seat the whole pot.
//
// Every seat is named, including the ones paid nothing: the runtime checks the
// shares against the seats it holds, and an explicit zero is the difference
// between "paid nothing" and "not considered".
func (p Policy) Winner(seat uint32) (map[uint32]int64, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if int(seat) >= len(p.Stakes) {
		return nil, fmt.Errorf("seat %d is not at this table", seat)
	}
	shares := make(map[uint32]int64, len(p.Stakes))
	for i := range p.Stakes {
		shares[uint32(i)] = 0
	}
	shares[seat] = p.Pot()
	return shares, nil
}

// Same reports whether the table still holds what this policy froze.
func (p Policy) Same(stakes []int64) bool {
	if len(stakes) != len(p.Stakes) {
		return false
	}
	for i, atoms := range stakes {
		if p.Stakes[i] != atoms {
			return false
		}
	}
	return true
}

// ID identifies this exact allocation, for a log or a receipt.
func (p Policy) ID() ([32]byte, error) {
	if err := p.Validate(); err != nil {
		return [32]byte{}, err
	}
	var b bytes.Buffer
	b.Write(tag)
	_ = binary.Write(&b, binary.BigEndian, p.Version)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(p.Stakes)))
	for _, atoms := range p.Stakes {
		_ = binary.Write(&b, binary.BigEndian, atoms)
	}
	return blake256.Sum256(b.Bytes()), nil
}
