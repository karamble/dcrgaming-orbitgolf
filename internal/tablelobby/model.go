// Package tablelobby is what a table looks like while it is being built.
//
// A pure model: no SDK, no chain, no I/O. Everything it describes is derived
// elsewhere and handed in, which is what lets the same types drive a live table
// and a fixture, and lets the whole screen be tested without a bridge.
//
// It exists because a table takes minutes to form and the money moves during
// those minutes. A player who can see which stage the table is at, whose
// deposit is still confirming and what the whole thing costs does not have to
// guess whether anything is wrong - and when something is wrong, the screen is
// where it says so rather than a log nobody is reading.
package tablelobby

import "fmt"

// Stage is how far a table has got. Being at a stage means that stage is what
// the table is waiting for.
type Stage uint8

const (
	// StageInvitation is an invitation accepted and nothing posted yet.
	StageInvitation Stage = iota
	// StageAdmission is waiting for this seat's own admission bond.
	StageAdmission
	// StageRoster is waiting for the seats to exchange joins and bind.
	StageRoster
	// StageSeatDraw is waiting for the block the seats are drawn from.
	StageSeatDraw
	// StageStake is waiting for the stakes.
	StageStake
	// StageReady is a verified table that can be played.
	StageReady
	// StageClosed is a table that will not form. Deposits remain recoverable.
	StageClosed
)

// Steps are the rail, in order. StageClosed is not a step: a closed table shows
// where it stopped rather than a seventh box.
var Steps = []Stage{StageInvitation, StageAdmission, StageRoster, StageSeatDraw, StageStake, StageReady}

// Label is the rail's word for a stage.
func (s Stage) Label() string {
	switch s {
	case StageInvitation:
		return "INVITATION"
	case StageAdmission:
		return "ADMISSION BOND"
	case StageRoster:
		return "ROSTER"
	case StageSeatDraw:
		return "SEAT DRAW"
	case StageStake:
		return "STAKE"
	case StageReady:
		return "READY"
	case StageClosed:
		return "CLOSED"
	}
	return "UNKNOWN"
}

// Deposit purposes, as the runtime names them. This SDK has exactly two.
const (
	PurposeSeatBond = "seatbond"
	PurposeStake    = "stake"
)

// Deposit check states, as the runtime reports them.
const (
	CheckUnchecked   = "unchecked"
	CheckMissing     = "missing"
	CheckMismatch    = "mismatch"
	CheckConfirming  = "confirming"
	CheckVerified    = "verified"
	CheckSpending    = "spending"
	CheckSpent       = "spent"
	CheckUnavailable = "unavailable"
)

// Deposit is one payment a seat owes or has made.
type Deposit struct {
	Purpose               string
	Atoms                 int64
	Confirmations         int64
	RequiredConfirmations int64
	Check                 string
	Error                 string
	// Local says this peer checked the output itself, against the script it
	// derived and the value it expected. Never set from anything a peer
	// announced: otherwise a seat could talk its way onto the table.
	Local bool
}

// Complete reports a deposit that is done being waited for.
//
// A conjunction rather than a state, and every term earns its place. The check
// must be one this peer made locally - a peer saying its money is down is not
// evidence that it is. The policy depth must exist, because a required depth of
// zero is a policy nobody set rather than a depth already reached. And the
// confirmations must actually have arrived.
func (d Deposit) Complete() bool {
	switch d.Check {
	case CheckVerified, CheckSpending, CheckSpent:
	default:
		return false
	}
	return d.Local && d.RequiredConfirmations > 0 && d.Confirmations >= d.RequiredConfirmations
}

// Wrong reports a deposit that will not resolve by waiting.
func (d Deposit) Wrong() bool {
	return d.Check == CheckMissing || d.Check == CheckMismatch || d.Error != ""
}

// Short is what a deposit's dot says beneath it.
func (d Deposit) Short() string {
	switch {
	case d.Check == "":
		return "not posted"
	case !d.Local:
		// Somebody said this money is down and this peer has not looked.
		// Saying "verified" here would be repeating a claim as a finding.
		return "unverified here"
	case d.Complete():
		return "verified"
	case d.Check == CheckConfirming && d.RequiredConfirmations > 0:
		return fmt.Sprintf("%d of %d", d.Confirmations, d.RequiredConfirmations)
	default:
		return d.Check
	}
}

// Name is the deposit's own word, for a label beside its dot.
func Name(purpose string) string {
	switch purpose {
	case PurposeSeatBond:
		return "ENTRY"
	case PurposeStake:
		return "STAKE"
	}
	return purpose
}

// Seat is one player's side of the table.
type Seat struct {
	Number   uint32
	You      bool
	Name     string
	Deposits []Deposit
}

// Deposit returns a seat's deposit of a purpose, and whether it has one.
func (s Seat) Deposit(purpose string) (Deposit, bool) {
	for _, d := range s.Deposits {
		if d.Purpose == purpose {
			return d, true
		}
	}
	return Deposit{}, false
}

// Status is the seat's line: what this seat is waiting on, in its own words.
//
// Stale evidence is reported as such rather than as the last thing that was
// true, because a screen that keeps saying "verified" after the bridge has gone
// is a screen telling somebody their money is confirmed when nobody is looking.
func (s Seat) StatusAt(stale bool) string {
	if stale {
		return "recheck required"
	}
	return s.Status()
}

// Status is the seat's line assuming current evidence.
func (s Seat) Status() string {
	for _, purpose := range []string{PurposeSeatBond, PurposeStake} {
		d, ok := s.Deposit(purpose)
		if !ok {
			return "waiting for " + lower(Name(purpose))
		}
		if d.Wrong() {
			if d.Error != "" {
				return lower(Name(purpose)) + ": " + d.Error
			}
			return lower(Name(purpose)) + ": " + d.Check
		}
		if !d.Complete() {
			return lower(Name(purpose)) + " confirming · " + d.Short()
		}
	}
	return "ready"
}

func lower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}
	return string(out)
}

// Terms is what the table costs, from the invitation everyone agreed.
type Terms struct {
	BuyInAtoms   int64
	BondAtoms    int64
	Seats        uint32
	RefundBlocks uint32
	BondBlocks   uint32
	Until        uint32
}

// Pot is what the winner takes before fees, and PerSeat what one player pays in
// total. Bonds come back; they are not part of the pot.
func (t Terms) Pot() int64     { return t.BuyInAtoms * int64(t.Seats) }
func (t Terms) PerSeat() int64 { return t.BuyInAtoms + t.BondAtoms }

// View is the whole screen, detached.
type View struct {
	Details bool
	Network string
	Match   string
	Stage   Stage
	Seats   []Seat
	Terms   Terms
	Height  uint32

	// NextStep is what the table is waiting for and NextDetail why, in a
	// sentence a player can act on.
	NextStep   string
	NextDetail string

	// CanFund says the stake may be requested now.
	CanFund bool
	// Error is whatever went wrong last, and ClosedReason why a closed table
	// stopped.
	Error        string
	ClosedReason string
	// Demo marks a fixture, so a screen can say so and never be mistaken for
	// a real table.
	Demo bool
	// Stale says the evidence behind this screen is no longer current -
	// the bridge went away, or the chain height is unknown. Checks made
	// before a disconnect are not checks made now, so a stale table reports
	// nothing as verified rather than showing yesterday's certainty.
	Stale bool
}

// Progress is how far a deposit has come, 0 to 1, and zero on stale evidence.
func (v View) Progress(d Deposit) float64 {
	if v.Stale || !d.Local || d.RequiredConfirmations <= 0 {
		return 0
	}
	if d.Confirmations >= d.RequiredConfirmations {
		return 1
	}
	return float64(d.Confirmations) / float64(d.RequiredConfirmations)
}

// DCR renders atoms exactly. Integers all the way: a float would round
// somebody's stake.
func DCR(atoms int64) string {
	sign := ""
	if atoms < 0 {
		sign, atoms = "-", -atoms
	}
	return fmt.Sprintf("%s%d.%08d", sign, atoms/1e8, atoms%1e8)
}

// BlocksLeft is how long admission stays open, and false once it has closed or
// while the height is unknown.
func (v View) BlocksLeft() (uint32, bool) {
	if v.Height == 0 || v.Terms.Until == 0 || v.Height >= v.Terms.Until {
		return 0, false
	}
	return v.Terms.Until - v.Height, true
}

// Waiting reports the seat holding the table up, if one seat is.
func (v View) Waiting() (Seat, bool) {
	for _, s := range v.Seats {
		if s.Status() != "ready" {
			return s, true
		}
	}
	return Seat{}, false
}
