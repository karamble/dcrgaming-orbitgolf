package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/karamble/dcrgaming-orbitgolf/internal/tablelobby"
	sdk "github.com/karamble/dcrgaming-sdk/pkg/runtime"
)

// CanFund reports whether this seat may ask for its stake, and why not.
//
// The ladder is the one dcrstakewars uses, and every rung is load-bearing. The
// last two are the duplicate-payment guards: a seat that already has a funded
// outpoint, or already has a stake request written in the spend book, must not
// ask again - the runtime would refuse it, but the button should never offer it.
func (g *Game) CanFund(sid string) (bool, string) {
	rt, err := g.runtime()
	if err != nil {
		return false, "no bridge"
	}
	t, err := g.table(sid)
	if err != nil {
		return false, "no table"
	}
	g.mu.Lock()
	snap, seen, verified, seat := t.snap, t.seen, t.verified, t.seat
	g.mu.Unlock()
	if !seen {
		return false, "the table has not been checked yet"
	}
	switch {
	case snap.Record.RecoveryOnly || snap.Record.Aborted:
		return false, "this table is closed; recover the deposit in dcrpulse"
	case len(snap.Seats) != int(rt.Terms(sid).Seats):
		return false, "the table is not seated yet"
	case !verified:
		return false, "the admission bonds are not all confirmed"
	case !g.StartAgreed(sid):
		if why := g.StartProblem(sid); why != "" {
			return false, why
		}
		return false, "waiting for both seats to state the same rules"
	case rt.PayoutFor(sid) == "":
		return false, "the bridge has not named a payout destination"
	}
	if _, _, funded := rt.Funded(sid, seat); funded {
		return false, "this seat's stake is already funded"
	}
	// A stake request already written down is a payment that may be in
	// flight. Asking again is how a stake gets paid twice.
	for _, p := range snap.Payments {
		if p.Purpose == tablelobby.PurposeStake {
			return false, "a stake payment is already requested; approve it in dcrpulse"
		}
	}
	return true, ""
}

// Fund asks the bridge for this seat's stake, once.
//
// No retry loop and no dedup of our own: the runtime writes the obligation down
// before it asks and refuses a second dispatch itself, and a guard layered on
// top of that produces an ambiguous obligation rather than safety.
func (g *Game) Fund(ctx context.Context, sid string) error {
	rt, err := g.runtime()
	if err != nil {
		return err
	}
	if ok, why := g.CanFund(sid); !ok {
		return fmt.Errorf("%s", why)
	}
	if !g.claim(sid, "fund") {
		return nil // already asking
	}
	defer g.release(sid, "fund")

	if err := rt.Fund(ctx, sid); err != nil {
		// "Could not ask" is not "the answer was no", and this one is
		// neither: the request went out and its id never came back. Asking
		// again pays twice, so it stops here and an operator reconciles.
		if errors.Is(err, sdk.ErrUnresolvedPayment) {
			g.mu.Lock()
			if t, ok := g.tables[sid]; ok {
				t.unresolved = true
			}
			g.mu.Unlock()
			log.Errorf("table %s: stake dispatch unresolved; reconcile the bridge request id, do not pay again", sid)
			return err
		}
		return err
	}
	log.Infof("table %s: stake requested", sid)
	return nil
}

// Unresolved reports a payment this peer asked for and never heard back about.
// It must be reconciled with the bridge's own record, never re-sent.
func (g *Game) Unresolved(sid string) bool {
	t, err := g.table(sid)
	if err != nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return t.unresolved
}

// Playable reports whether the money is in and the game may start, and why not.
//
// This is the whole "we may play" predicate: every seat's admission bond
// verified, every seat's stake verified on chain, every seat's payout
// destination announced, and both seats stating the same rules. Nothing is
// signed before all of it holds.
func (g *Game) Playable(sid string) (bool, string) {
	rt, err := g.runtime()
	if err != nil {
		return false, "no bridge"
	}
	t, err := g.table(sid)
	if err != nil {
		return false, "no table"
	}
	g.mu.Lock()
	snap, seen, verified, staked := t.snap, t.seen, t.verified, t.staked
	g.mu.Unlock()
	if !seen {
		return false, "the table has not been checked yet"
	}
	if !verified {
		return false, "the admission bonds are not all confirmed"
	}
	if !g.StartAgreed(sid) {
		if why := g.StartProblem(sid); why != "" {
			return false, why
		}
		return false, "waiting for both seats to state the same rules"
	}
	seats := int(rt.Terms(sid).Seats)
	if !staked {
		confirmed := 0
		for _, d := range snap.Deposits {
			if d.Purpose == tablelobby.PurposeStake && d.Check == tablelobby.CheckVerified {
				confirmed++
			}
		}
		return false, fmt.Sprintf("%d of %d stakes confirmed", confirmed, seats)
	}
	if len(snap.Record.Payouts) != seats {
		return false, "waiting for both payout destinations"
	}
	return true, ""
}

// claim takes a single-flight slot for a table's job, and release gives it back.
func (g *Game) claim(sid, job string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy == nil {
		g.busy = map[string]bool{}
	}
	key := sid + ":" + job
	if g.busy[key] {
		return false
	}
	g.busy[key] = true
	return true
}

func (g *Game) release(sid, job string) {
	g.mu.Lock()
	delete(g.busy, sid+":"+job)
	g.mu.Unlock()
}
