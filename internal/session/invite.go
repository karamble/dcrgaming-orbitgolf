package session

import (
	"context"
	"fmt"

	"github.com/karamble/dcrgaming-orbitgolf/internal/match"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/schema"
	"github.com/karamble/dcrgaming-sdk/pkg/membership"
)

// The amounts this build is willing to play for.
//
// A floor because the settlement fee and the dust limit come out of the pot: a
// buy-in small enough that a two-seat payout cannot be built produces a table
// that can never settle at all, to anybody, and both stakes wait out the refund
// lock. A ceiling because this is how much a release whose settlement path has
// not yet completed on mainnet is willing to have at risk. Neither is a
// protocol limit; both are this game's own policy.
const (
	MinBuyInAtoms uint64 = 50_000
	MaxBuyInAtoms uint64 = 200_000
)

// ResolveInvite refuses a table this game cannot honour, before any coin moves.
//
// This is the only moment a refusal is free. Accepting an invitation starts the
// runtime's admission worker, which pays the seat bond without asking again -
// so a table that is going to fail must fail here, not after 0.01 DCR is locked
// behind a timelock this game does not hold the key to.
//
// The terms come back unchanged. The economics are the operator's, and the SDK
// checks that a game has not substituted its own; the only thing this may do is
// decline.
func (g *Game) ResolveInvite(_ context.Context, _ schema.Invite, terms membership.Terms) (membership.Terms, error) {
	if terms.Seats != match.Seats {
		return terms, fmt.Errorf("this game is heads-up; that table seats %d", terms.Seats)
	}
	// The refund lock must outlast the match. A stake whose refund matures
	// while the game is still being played can be taken back by its owner,
	// leaving a pot that can no longer pay the winner.
	if terms.CSVBlocks <= MatchDeadlineBlocks {
		return terms, fmt.Errorf(
			"that table's refund lock is %d blocks and a match may run %d; "+
				"a stake could mature before the game ends",
			terms.CSVBlocks, MatchDeadlineBlocks)
	}
	if terms.CSVBlocks < RefundBlocks {
		return terms, fmt.Errorf("that table's refund lock is %d blocks; this game plays at %d",
			terms.CSVBlocks, RefundBlocks)
	}
	if terms.BondLockBlocks < BondLockBlocks {
		return terms, fmt.Errorf("that table's admission lock is %d blocks; this game plays at %d",
			terms.BondLockBlocks, BondLockBlocks)
	}
	if terms.BuyInAtoms < MinBuyInAtoms {
		return terms, fmt.Errorf(
			"a buy-in of %d atoms cannot cover the settlement fee and the dust limit; "+
				"this game needs at least %d", terms.BuyInAtoms, MinBuyInAtoms)
	}
	if terms.BuyInAtoms > MaxBuyInAtoms {
		return terms, fmt.Errorf("this build refuses a buy-in above %d atoms; that table asks %d",
			MaxBuyInAtoms, terms.BuyInAtoms)
	}
	log.Infof("accepting a table: buy-in %d, bond %d, refund lock %d blocks, admission closes at %d",
		terms.BuyInAtoms, terms.BondAtoms, terms.CSVBlocks, terms.Until)
	return terms, nil
}
