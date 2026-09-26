// Package session is dcrminigolf's side of the SDK runtime.
//
// It implements the four methods a game owes - what it is called, what a table
// costs, what a message means, and what an operator should be shown - and
// builds the move exchange on top of them. It writes no bridge dispatcher, no
// spend book and no seating machine, because the runtime owns those.
//
// It depends on the runtime through [runtime.Game], the facade the SDK exposes
// for exactly this, rather than on *runtime.Runtime. The same code then runs
// against the real runtime and against a stand-in, and neither one is a special
// case.
package session

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/decred/slog"
	"github.com/karamble/dcrgaming-orbitgolf/internal/audit"

	"github.com/karamble/dcrgaming-orbitgolf/internal/manifest"
	"github.com/karamble/dcrgaming-orbitgolf/internal/match"
	"github.com/karamble/dcrgaming-orbitgolf/internal/movelog"
	"github.com/karamble/dcrgaming-orbitgolf/internal/payout"
	"github.com/karamble/dcrgaming-orbitgolf/internal/tablelobby"
	"github.com/karamble/dcrgaming-sdk/pkg/forfeit"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/connect"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/gamingpb"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/schema"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/wire"
	"github.com/karamble/dcrgaming-sdk/pkg/identity"
	"github.com/karamble/dcrgaming-sdk/pkg/membership"
	sdk "github.com/karamble/dcrgaming-sdk/pkg/runtime"
)

// What this game is called and which version of its payload contract it
// speaks. The bridge routes on both and checks them rather than believing them.
const (
	GameID  = "dcrminigolf"
	GameVer = 1
)

// RefundBlocks and BondLockBlocks are the money-lock terms this game tells the
// bridge, so it mints an invitation this game will accept and can disclose the
// durations before anybody pays.
//
// The locks are chosen against MatchDeadlineBlocks rather than picked.
// It must outlast any match this game can produce. MatchDeadlineBlocks is the
// ceiling on that, and this is comfortably beyond it: a stake whose refund
// matured while its match was still being played could be taken back by its
// owner, leaving a pot that can no longer pay the winner.
const (
	RefundBlocks   uint32 = 288
	BondLockBlocks uint32 = 288
)

// The game's own message kinds. The runtime carries them without reading them,
// and the bridge never sees inside either.
const (
	// KindMove is a signed move.
	KindMove schema.Kind = "m.move"
	// KindAbandon is a seat saying the other one stopped playing.
	KindAbandon schema.Kind = "m.abandon"
	// KindStart is a seat saying which game it is playing.
	KindStart schema.Kind = "m.start"
)

// MoveDeadlineBlocks is how long a seat has to move before the other may give
// up on the match.
//
// Blocks, not minutes. A deadline two machines read differently is one that
// decides money by whose clock ran fast, which is the same reason the SDK's own
// admission deadline is a height. Twelve blocks is roughly an hour: this is a
// game two people sit down to play, not a correspondence match, and a stake
// should not be tied up overnight because somebody closed their laptop.
//
// Frozen by game version. Both peers derive the deadline from the log rather
// than agreeing one, so there is nothing to negotiate and nothing to disagree
// about.
const MoveDeadlineBlocks uint32 = 12

// MatchDeadlineBlocks is how long a whole match may run, from its first move.
//
// A move deadline alone does not bound a match. Nine holes allow up to 216
// shots, so even at an hour each two seats could keep a table open for days
// without either ever being late - and the stakes would still be sitting in the
// escrow when their refund branches matured. A seat whose refund has matured
// can take its own stake back mid-match, leaving a pot that can no longer pay.
// The SDK says as much about why the refund timelock is in the agreed terms.
//
// So the match has its own ceiling, chosen against the refund lock rather than
// against how long a game feels: 144 blocks is roughly twelve hours, half the
// 288-block refund lock this game asks for, leaving room for a slow chain or a
// reorganisation. Past it either seat may end the match, whosever turn it is -
// otherwise a seat that liked the position could hold the table open by never
// quite being late.
const MatchDeadlineBlocks uint32 = 144

// SeatTags decide which keys a seat has, so they are frozen for good. Changing
// one later strands whatever those keys held. The SDK will not invent them,
// deliberately.
var SeatTags = identity.SeatTags{
	Session: "minigolf/session/v1",
	Log:     "minigolf/log/v1",
	Bond:    "minigolf/bond/v1",
}

// OpenerSeat is the seat that opens the first hole.
//
// It is seat 0, drawn by the SDK's seating beacon, and not "whoever created the
// table" as first intended - because nothing on the wire says who that was.
// schema.Invite carries terms and no host, dcrpulse creates tables rather than
// games, and both peers are asked to accept the same invitation, so neither can
// compute the creator. Seat 0 is the only answer both sides reach alike, and it
// is a better one: the beacon is a block hash nobody could see when they chose
// their key, and it is settled before anybody funds.
const OpenerSeat uint32 = 0

// Runtime is the part of the SDK runtime this game uses.
//
// [runtime.Game] is the facade the SDK offers a game, plus Funded: a payout has
// to divide exactly what the table actually holds, and the runtime is the only
// thing that knows what confirmed. *runtime.Runtime satisfies this as it
// stands, so nothing in the SDK changes to supply it.
type Runtime interface {
	sdk.Game
	// Funded is what a seat actually put in. A payout must divide exactly
	// what the table holds, and the runtime is the only thing that knows.
	Funded(match string, seat uint32) (outpoint string, atoms int64, ok bool)
	// Terms are the table's agreed money terms, and Snapshot its lifecycle
	// state. Both are needed to know when a table is ready to be played.
	Terms(match string) membership.Terms
	Snapshot(match string) (sdk.TableSnapshot, error)
	// CheckAdmissionBonds verifies every seat's admission output on chain,
	// and RefreshDeposits re-checks the rest. The SDK is explicit that a
	// seated phase is agreement on the roster and not permission to play:
	// a game checks the deposits itself before it starts.
	CheckAdmissionBonds(ctx context.Context, match string) error
	RefreshDeposits(ctx context.Context, match string) (sdk.TableSnapshot, error)
	// Fund asks the bridge for this seat's stake. Idempotent and internally
	// single-flighted; never wrap it in a retry of your own.
	Fund(ctx context.Context, match string) error
	// PayoutFor is the bridge-owned destination this table pays to. Empty
	// until the bridge has named one.
	PayoutFor(match string) string
}

// Game is dcrminigolf's rules, and the state of every table it has open.
type Game struct {
	dir string

	mu     sync.Mutex
	rt     Runtime
	tables map[string]*table
	// busy is the single-flight set for per-table jobs.
	busy map[string]bool
}

// table is one match in progress.
type table struct {
	seat uint32
	// key is bound to the durable book above and is the only key that signs
	// for this table.
	key     *forfeit.LogKey
	chain   *movelog.Chain
	play    *match.Match
	book    *movelog.Book
	journal *movelog.Journal
	// pending is a move reserved but never signed, recovered from the
	// journal. It must be re-signed from those exact bytes.
	pending *movelog.Entry
	settled bool
	// abandoned records that one seat gave up on the other, and which seat
	// was said to have stopped.
	abandoned       bool
	abandonEvidence *movelog.Abandon
	stalled         uint32
	expired         bool
	// staked latches once every seat's stake has been seen confirmed on
	// chain. It does not un-latch: a later check that could not reach the
	// chain reports "unavailable", which means this peer could not ask, not
	// that the money left escrow. Treating the two the same stops a match
	// mid-play over one unanswered call.
	staked bool
	// phase is the runtime's lifecycle word for this table, and verified
	// says this peer has checked the roster's admission bonds on chain.
	phase    string
	verified bool
	// snap is the last lifecycle snapshot this peer took, kept so the
	// seating screen can be drawn without asking the runtime again on
	// every frame.
	snap sdk.TableSnapshot
	seen bool
	// starts is what each seat said it is playing, keyed by its log key, and
	// startConflict why the table stopped if two seats disagree.
	starts        map[string]manifest.Manifest
	startSent     bool
	startNoted    bool // our start reached the bridge before a restart
	startConflict string
	// unresolved marks a payment that was dispatched and never answered.
	unresolved bool
	// pendingAbandon is a well-formed claim whose deadline the chain has not
	// reached yet. Held rather than refused: waiting makes it true.
	pendingAbandon *movelog.Abandon
	// allowed is this peer's own verdict that the outcome is settled and may
	// be paid. Never set from anything a peer said.
	allowed bool
	// policy is what the table holds, frozen once every stake confirmed.
	policy *payout.Policy
	// blocked is why this table must not be settled, if something went wrong
	// that waiting will not fix.
	blocked string
	// paid is this seat's stake having been spent by the settlement.
	paid bool
}

// New returns a game that keeps its signing books under dir.
//
// dir has to outlive the process. A book that does not is a key with no memory
// of what it has already signed, and the first restart mid-match publishes it.
func New(dir string) (*Game, error) {
	if dir == "" {
		return nil, fmt.Errorf("a game needs somewhere durable to keep its signing books")
	}
	return &Game{dir: dir, tables: map[string]*table{}}, nil
}

// Bind attaches the runtime.
//
// Separate from New because the two are circular by construction: the runtime
// is built around the rules, and the rules need the runtime to speak. Nothing
// the runtime asks for before a table exists - Identity, Terms - needs it.
func (g *Game) Bind(rt Runtime) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rt = rt
}

// runtime returns the bound runtime, or an error rather than a nil panic.
func (g *Game) runtime() (Runtime, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.rt == nil {
		return nil, fmt.Errorf("this game has no runtime bound")
	}
	return g.rt, nil
}

// Identity introduces the game to the bridge.
func (g *Game) Identity() connect.Identity {
	return connect.Identity{
		GameID:        GameID,
		GameVer:       GameVer,
		ClientVersion: "dcrminigolf/0.1",
		Capabilities: []gamingpb.Capability{
			gamingpb.Capability_CAP_ACCEPT_INVITE,
		},
		MinRefundBlocks: RefundBlocks,
		BondLockBlocks:  BondLockBlocks,
	}
}

// Terms states what this game will sit at.
//
// Everything financial is left at zero on purpose. The economics belong to the
// invitation dcrpulse minted - buy-in, seats, admission bond, deadline and
// refund lock - and a game that stated its own would be substituting its terms
// for the ones a player was shown before they paid.
func (g *Game) Terms(sid string) (membership.Terms, error) {
	if sid == "" {
		return membership.Terms{}, fmt.Errorf("a table needs a session id")
	}
	return membership.Terms{GameVer: GameVer}, nil
}

// Seated is called when a table has finished forming, which is when play may
// start. The roster it is handed is the log keys, derived from the roster the
// escrow committed to.
// Ensure opens a seated table this peer is not already playing.
//
// Idempotent, and deliberately not reached only through the Seated hook: that
// hook fires once, when the runtime draws the seats, and never again. A process
// that restarts mid-match is never told about the table it was playing, so
// unless it can reopen one from what is on disk, the stakes are unreachable by
// the game for the rest of the refund lock.
func (g *Game) Ensure(sid string) error {
	g.mu.Lock()
	_, have := g.tables[sid]
	g.mu.Unlock()
	if have {
		return nil
	}
	return g.open(sid)
}

func (g *Game) Seated(_ context.Context, sid string, _ map[uint32][]byte) {
	if err := g.Ensure(sid); err != nil {
		log.Errorf("table %s was seated but could not be opened: %v", sid, err)
		// A table that cannot be opened is one this peer will not play at.
		// There is nothing to send about it: the other seat learns the same
		// way it learns about any silence.
		return
	}
	if v, ok := g.View(sid); ok {
		log.Infof("table %s seated: this peer is seat %d of %d, match %s",
			sid, v.Seat, match.Seats, v.MatchID)
	}
}

// open builds the chain, the rules and the durable book for a seated table.
func (g *Game) open(sid string) error {
	rt, err := g.runtime()
	if err != nil {
		return err
	}
	matchID, ok := rt.MatchID(sid)
	if !ok {
		return fmt.Errorf("table %s has no match id yet", sid)
	}
	seats, ok := rt.LogSeats(sid)
	if !ok {
		return fmt.Errorf("table %s has no roster yet", sid)
	}
	seat, ok := rt.Seat(sid)
	if !ok {
		return fmt.Errorf("table %s has no seat for this peer", sid)
	}
	roster := make(movelog.Roster, len(seats))
	for s, key := range seats {
		roster[s] = key
	}
	journal, err := movelog.OpenJournal(filepath.Join(g.dir, matchID+".journal"))
	if err != nil {
		return err
	}
	// Whatever this peer already played is replayed from its own journal, and
	// every entry is re-verified against the roster the escrow committed to on
	// the way back in. A transcript is worth exactly its signatures.
	rep, err := journal.Replay(matchID, roster)
	if err != nil {
		journal.Close()
		return fmt.Errorf("this table's journal could not be replayed: %w", err)
	}
	chain := rep.Chain
	play, err := match.Replay(OpenerSeat, audit.Moves(chain))
	if err != nil {
		journal.Close()
		return fmt.Errorf("this table's moves are not a playable match: %w", err)
	}
	key, err := rt.LogKey(sid)
	if err != nil {
		return err
	}
	book, err := movelog.OpenBook(filepath.Join(g.dir, matchID+".book"))
	if err != nil {
		return err
	}
	key.Remember(book)

	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.tables[sid]; exists {
		book.Close()
		journal.Close()
		return nil // already open; seating twice is not a second table
	}
	// The key is kept, not re-fetched. rt.LogKey builds a fresh LogKey every
	// call, and a fresh one has no book: asking for it again at signing time
	// silently swaps the durable equivocation guard for an empty in-memory
	// map, per signature. This is the only key that may ever sign for this
	// table.
	t := &table{
		seat: seat, chain: chain, play: play, book: book, key: key, journal: journal,
		pending: rep.Pending,
	}
	if a := rep.Abandon; a != nil {
		t.abandonEvidence = a
		t.abandoned, t.stalled, t.expired = true, a.Stalled, a.Reason == movelog.ReasonExpired
	}
	for _, note := range rep.Notes {
		if note == "start" {
			t.startNoted = true
		}
		if note == "paid" {
			// A settled table that came back looking unsettled would
			// propose its payout all over again.
			t.paid, t.settled = true, true
		}
	}
	g.tables[sid] = t
	if chain.Len() > 0 {
		log.Infof("table %s: resumed at move %d, hole %d, seat %d to play",
			sid, chain.Len(), play.Index(), play.Turn())
	}
	return nil
}

// Handle takes one message addressed to this game.
//
// The runtime has already framed, routed, reassembled and checked the sender.
// What it has not done is look inside, so everything that makes a move a move
// is checked here: that it is the next entry on this table's chain, signed by
// the seat it claims, and a legal move for that seat to make now.
//
// The authenticated sender is deliberately not what decides whose move this is.
// The entry names a seat and carries that seat's own signature, checked against
// the roster the escrow committed to - which is a stronger statement than which
// Bison Relay identity a frame arrived from.
func (g *Game) Handle(_ context.Context, in sdk.Message) error {
	t, err := g.table(in.Match)
	if err != nil {
		return err
	}
	switch in.Kind {
	case KindMove:
		return g.handleMove(t, in.Body)
	case KindAbandon:
		return g.handleAbandon(t, in.Body)
	case KindStart:
		return g.handleStart(t, in.Body)
	default:
		return fmt.Errorf("unknown message kind %q", in.Kind)
	}
}

func (g *Game) handleMove(t *table, body []byte) error {
	e, err := movelog.DecodeEntry(body)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if e.Seat == t.seat {
		return fmt.Errorf("a move arrived claiming to be this peer's own seat %d", e.Seat)
	}
	if t.abandoned {
		return fmt.Errorf("a move arrived for a match this peer has given up on")
	}
	// The rules decide first. A chain that recorded a move the match then
	// refused would be one move ahead of the game for good: every later move
	// would append cleanly and apply to a position the two peers no longer
	// share, and they would settle different outcomes without ever noticing.
	mv := match.Move{Hole: e.Hole, Seat: e.Seat, Shot: e.Shot}
	if err := t.play.CanPlay(mv); err != nil {
		return err
	}
	if err := t.chain.Append(e); err != nil {
		return err
	}
	if err := t.journal.Commit(e); err != nil {
		return err
	}
	return t.play.Play(mv)
}

// handleAbandon takes the other seat's claim that this one stopped playing.
//
// Every part of the claim is recomputed from the log this peer already holds.
// Nothing is taken on the claimant's word, because an abandonment is worth
// money: it turns a hole somebody is losing into a void that returns their
// stake, and a signature proves only who said it, not that it was true.
//
// Two different refusals, deliberately. A claim that names the wrong seat, the
// wrong hole or a deadline this log does not imply is rejected outright: it
// can never become true. A claim that is correct but early is held, because it
// becomes true by waiting - and both peers, applying this to the same log,
// reach the same answer.
func (g *Game) handleAbandon(t *table, body []byte) error {
	a, err := movelog.DecodeAbandon(body)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if a.Seat == t.seat {
		return fmt.Errorf("an abandonment arrived claiming to be this peer's own seat %d", a.Seat)
	}
	if t.play.Done() {
		return fmt.Errorf("the match finished; there was nothing to abandon")
	}
	// Structure and signature first: is this a real statement by a real seat
	// about this chain's head?
	if err := t.chain.CheckAbandon(a); err != nil {
		return err
	}
	if int(a.Hole) != t.play.Index() {
		return fmt.Errorf("it names hole %d; this match is on hole %d", a.Hole, t.play.Index())
	}
	last, ok := t.chain.LastHeight()
	if !ok {
		return fmt.Errorf("no move has been made, so nobody can be late for one")
	}
	first, _ := t.chain.FirstHeight()

	switch a.Reason {
	case movelog.ReasonStall:
		if a.Stalled != t.play.Turn() {
			return fmt.Errorf("it names seat %d as stalled; seat %d owes the move",
				a.Stalled, t.play.Turn())
		}
		if a.Seat == t.play.Turn() {
			return fmt.Errorf("seat %d owes the move and cannot say the other is holding it up", a.Seat)
		}
		want, ok := due(last, MoveDeadlineBlocks)
		if !ok || a.Deadline != want {
			return fmt.Errorf("it names deadline %d; this log says %d", a.Deadline, want)
		}
	case movelog.ReasonExpired:
		want, ok := due(first, MatchDeadlineBlocks)
		if !ok || a.Deadline != want {
			return fmt.Errorf("it names deadline %d; this log says %d", a.Deadline, want)
		}
	default:
		return fmt.Errorf("unknown abandonment reason %d", a.Reason)
	}

	// Correct, but is it yet true? The chain says, and only the chain.
	t.pendingAbandon = a
	return nil
}

// due adds a span to a height and reports whether it could be computed.
//
// Heights come off the wire, so the arithmetic is checked: a claimed height near
// the top of the range would otherwise wrap and produce a deadline in the past,
// which is a way to abandon a losing hole immediately.
func due(base, span uint32) (uint32, bool) {
	if base > ^uint32(0)-span {
		return 0, false
	}
	return base + span, true
}

// adjudicate applies a held abandonment once the chain has passed its deadline.
// Called from the driver's chain loop, where a tip is available.
func (g *Game) adjudicate(t *table, height uint32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := t.pendingAbandon
	if a == nil || height < a.Deadline {
		return
	}
	if err := t.journal.CommitAbandon(a); err != nil {
		log.Errorf("table %s: the abandonment could not be recorded: %v", t.chain.MatchID(), err)
		return
	}
	t.abandoned, t.stalled, t.expired = true, a.Stalled, a.Reason == movelog.ReasonExpired
	t.abandonEvidence = a
	t.pendingAbandon = nil
	log.Infof("table %s: seat %d gave up on the match at block %d (%s)",
		t.chain.MatchID(), a.Seat, a.Deadline, a.Reason)
}

// Deadline is the height by which the seat to move owes one, and false before
// the first move has been made.
func (g *Game) Deadline(sid string) (uint32, bool) {
	t, err := g.table(sid)
	if err != nil {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	last, ok := t.chain.LastHeight()
	if !ok {
		return 0, false
	}
	return last + MoveDeadlineBlocks, true
}

// Abandon gives up on a match the other seat has stopped playing.
//
// Refused before the deadline, and refused on this peer's own turn: a seat that
// could abandon while it was the one holding things up would have found a way
// to leave a losing hole without playing it. That it can still leave a losing
// hole by waiting out the deadline is an accepted limitation of this release,
// and is disclosed before anybody funds.
func (g *Game) Abandon(ctx context.Context, sid string) error {
	rt, err := g.runtime()
	if err != nil {
		return err
	}
	t, err := g.table(sid)
	if err != nil {
		return err
	}
	tip, err := rt.Chain(ctx)
	if err != nil {
		return err
	}

	g.mu.Lock()
	key := t.key
	if t.play.Done() {
		g.mu.Unlock()
		return fmt.Errorf("the match is over")
	}
	if t.abandoned {
		g.mu.Unlock()
		return nil
	}
	last, ok := t.chain.LastHeight()
	if !ok {
		g.mu.Unlock()
		return fmt.Errorf("no move has been made yet")
	}
	first, _ := t.chain.FirstHeight()
	height := uint32(tip.Height)
	moveDue, moveOK := due(last, MoveDeadlineBlocks)
	matchDue, matchOK := due(first, MatchDeadlineBlocks)
	if !moveOK || !matchOK {
		g.mu.Unlock()
		return fmt.Errorf("this log reports heights no deadline can be computed from")
	}

	// Whose turn it is stops mattering once the match itself is out of time.
	// Before that, a seat cannot give up while it is the one owing a move.
	deadline, reason := moveDue, movelog.ReasonStall
	if height < matchDue {
		if t.play.Turn() == t.seat {
			g.mu.Unlock()
			return fmt.Errorf("it is this peer's own move; nobody else is holding this up")
		}
		if height < moveDue {
			g.mu.Unlock()
			return fmt.Errorf("seat %d has until block %d; the chain is at %d",
				t.play.Turn(), moveDue, height)
		}
	} else {
		deadline, reason = matchDue, movelog.ReasonExpired
	}
	stalled := t.play.Turn()
	a, err := t.chain.Abandon(key, t.seat, stalled, reason, uint8(t.play.Index()), deadline)
	if err != nil {
		g.mu.Unlock()
		return err
	}
	if err := t.journal.CommitAbandon(a); err != nil {
		g.mu.Unlock()
		return err
	}
	t.abandoned, t.stalled, t.expired = true, stalled, reason == movelog.ReasonExpired
	t.abandonEvidence = a
	g.mu.Unlock()

	return rt.Send(ctx, sid, KindAbandon, a, wire.ClassDurable)
}

// Play makes this peer's own move: signs it, records it and sends it.
//
// The order matters. The chain hands out the sequence number, the log key signs
// at that position and the durable book remembers it before the signature
// exists - so a crash anywhere in here costs a move, never a key.
func (g *Game) Play(ctx context.Context, sid string, shot uint32) error {
	rt, err := g.runtime()
	if err != nil {
		return err
	}
	t, err := g.table(sid)
	if err != nil {
		return err
	}
	tip, err := rt.Chain(ctx)
	if err != nil {
		return err
	}

	g.mu.Lock()
	key := t.key
	g.mu.Unlock()
	// Nothing is signed until the money is in: both bonds confirmed, both
	// stakes verified on chain, both payout destinations announced and both
	// seats stating the same rules. Playing first and checking afterwards is
	// how a seat ends up in a match nobody funded.
	if ok, why := g.Playable(sid); !ok {
		return fmt.Errorf("this table is not ready to play: %s", why)
	}
	g.mu.Lock()
	if t.play.Done() {
		g.mu.Unlock()
		return fmt.Errorf("the match is over")
	}
	// A seat that came back to find the match given up on must not sign
	// another move. The position it would sign at is one the abandonment
	// already spoke about, and the move would be refused by the only peer
	// that could accept it.
	if t.abandoned {
		g.mu.Unlock()
		return fmt.Errorf("this match was given up on; it settles void")
	}
	if t.play.Turn() != t.seat {
		g.mu.Unlock()
		return fmt.Errorf("it is seat %d's move, not this peer's", t.play.Turn())
	}
	boardIndex := uint8(t.play.Index())
	// Refuse an illegal move before signing it. A signature over a move the
	// other seat will reject is a signature at a position that can never be
	// used again for the move that replaces it.
	if err := t.play.CanPlay(match.Move{Hole: boardIndex, Seat: t.seat, Shot: shot}); err != nil {
		g.mu.Unlock()
		return err
	}
	// Written down before it is signed. The key records the position as it
	// signs, so a crash between the two would leave the book holding a digest
	// for a move this peer can no longer reconstruct - and it would refuse to
	// sign anything else there, which is the seat unable to move again.
	intended := &movelog.Entry{
		Version: movelog.Version, PrevHash: head(t), Seq: next(t),
		Hole: boardIndex, Seat: t.seat, Shot: shot, Height: uint32(tip.Height),
		Signer: key.Public().SerializeCompressed(),
	}
	if err := t.journal.Reserve(intended); err != nil {
		g.mu.Unlock()
		return err
	}
	e, err := t.chain.Move(key, t.seat, boardIndex, shot, uint32(tip.Height))
	if err != nil {
		g.mu.Unlock()
		return err
	}
	if err := t.journal.Commit(e); err != nil {
		g.mu.Unlock()
		return err
	}
	if err := t.play.Play(match.Move{Hole: boardIndex, Seat: t.seat, Shot: shot}); err != nil {
		g.mu.Unlock()
		return err
	}
	g.mu.Unlock()

	// The entry itself, not its encoding: the runtime encodes whatever body
	// it is given, and handing it bytes would encode them twice.
	return rt.Send(ctx, sid, KindMove, e, wire.ClassDurable)
}

// State is what the dashboard shows an operator.
//
// Answered from the bridge's request loop, so it takes the game's lock briefly
// and calls nothing back into the runtime.
func (g *Game) State(_ context.Context) sdk.State {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := sdk.State{Tables: make([]sdk.TableState, 0, len(g.tables))}
	for sid, t := range g.tables {
		status := fmt.Sprintf("hole %d, seat %d to move", t.play.Index()+1, t.play.Turn())
		if t.play.Done() {
			winner, won, _ := t.play.Outcome()
			status = "void"
			if won {
				status = fmt.Sprintf("seat %d won", winner)
			}
		}
		score := t.play.Score()
		st.Tables = append(st.Tables, sdk.TableState{
			Match:  sid,
			Status: status,
			Seats:  match.Seats,
			Detail: map[string]string{
				"score": fmt.Sprintf("%d-%d", score[0], score[1]),
				"moves": fmt.Sprint(t.chain.Len()),
			},
		})
	}
	switch len(st.Tables) {
	case 0:
		st.Summary = "no table"
	case 1:
		st.Summary = st.Tables[0].Status
	default:
		st.Summary = fmt.Sprintf("%d tables", len(st.Tables))
	}
	return st
}

// View is a detached snapshot of one table, for a screen to draw.
type View struct {
	Entries               []movelog.Entry
	Results               []match.Result
	BuyIn, Bond           int64
	StakeCheck, BondCheck string
	// Grid is a copy of the hole being played. A value, so a screen can
	// never read a square while a move is being applied to it.
	Grid    match.HoleState
	Seat    uint32
	Hole    int
	Turn    uint32
	Score   [match.Seats]int
	Done    bool
	Winner  uint32
	Won     bool
	Moves   int
	MatchID string
	// Head is the chain head, hex. Two peers holding different heads have
	// different histories of the same match, which is the one thing a
	// multi-peer test has to be able to see.
	Head string
	// Abandoned says a seat gave up on the match, and Stalled which seat
	// owed the move. Expired distinguishes the match running out of time,
	// where nobody is accused of anything. Either way it settles void.
	Abandoned bool
	Stalled   uint32
	Expired   bool
	// Phase is the runtime's lifecycle word, and Verified says this peer
	// has confirmed every seat's admission bond on chain.
	Phase    string
	Verified bool
	// Paid says this seat's stake was spent by the settlement.
	Paid bool
	// Blocked is why this table must not be settled, if anything.
	Blocked string
	// Deadline is the height by which the seat to move owes one, and
	// MatchDeadline the height by which the whole match must be finished.
	Deadline      uint32
	MatchDeadline uint32
}

// View reports a table's state without handing out anything that could change
// under the caller.
func (g *Game) View(sid string) (View, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tables[sid]
	if !ok {
		return View{}, false
	}
	winner, won, done := t.play.Outcome()
	v := View{
		Entries: t.chain.Entries(), Results: t.play.Results(),
		BuyIn: int64(t.snap.Record.Terms.BuyInAtoms), Bond: int64(t.snap.Record.Terms.BondAtoms),
		Grid: *t.play.Hole(),
		Seat: t.seat, Hole: t.play.Index(), Turn: t.play.Turn(),
		Score: t.play.Score(), Done: done, Winner: winner, Won: won,
		Moves: t.chain.Len(), MatchID: t.chain.MatchID(), Head: headHex(t.chain),
		Abandoned: t.abandoned, Stalled: t.stalled, Expired: t.expired,
		Phase: t.phase, Verified: t.verified, Paid: t.paid, Blocked: t.blocked,
	}
	for _, d := range t.snap.Deposits {
		if d.Seat != t.seat {
			continue
		}
		if d.Purpose == tablelobby.PurposeStake {
			v.StakeCheck = d.Check
		}
		if d.Purpose == tablelobby.PurposeSeatBond {
			v.BondCheck = d.Check
		}
	}
	if last, ok := t.chain.LastHeight(); ok {
		v.Deadline = last + MoveDeadlineBlocks
	}
	if first, ok := t.chain.FirstHeight(); ok {
		v.MatchDeadline = first + MatchDeadlineBlocks
	}
	if t.abandoned {
		v.Done, v.Won = true, false
	}
	return v, true
}

// Transcript is the signed history of a table, for an auditor.
func (g *Game) Transcript(sid string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tables[sid]
	if !ok {
		return nil, fmt.Errorf("no table %s", sid)
	}
	return t.chain.Marshal()
}

// Close releases every table's book.
func (g *Game) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var first error
	for _, t := range g.tables {
		if err := t.book.Close(); err != nil && first == nil {
			first = err
		}
		if err := t.journal.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (g *Game) table(sid string) (*table, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tables[sid]
	if !ok {
		return nil, fmt.Errorf("no table %s is open", sid)
	}
	return t, nil
}

// Settle proposes this table's payout for approval.
//
// Called by a driver once the match is over, rather than from inside Handle or
// Play: those run on the runtime's own delivery path, and a game that called
// back into the runtime from there would be arguing with the machinery that
// just called it.
//
// Both peers propose, and that is the cooperative model rather than a
// duplicate. Each verifies the result against its own copy of the signed log
// and asks its own bridge to pay accordingly; neither is trusted to report the
// other's outcome, and a person approves at each end.
//
// A match nobody won is Void, which hands every seat its own stake back. The
// runtime works out those amounts itself, so the game says only that nobody
// won rather than inventing an even split.
func (g *Game) Settle(ctx context.Context, sid string) error {
	rt, err := g.runtime()
	if err != nil {
		return err
	}
	t, err := g.table(sid)
	if err != nil {
		return err
	}

	g.mu.Lock()
	if t.blocked != "" {
		why := t.blocked
		g.mu.Unlock()
		return fmt.Errorf("%s", why)
	}
	if !t.play.Done() && !t.abandoned {
		g.mu.Unlock()
		return fmt.Errorf("the match is not over")
	}
	if t.settled {
		g.mu.Unlock()
		return nil
	}
	if t.policy == nil {
		g.mu.Unlock()
		return fmt.Errorf("this table's stakes were never agreed, so there is nothing to divide")
	}
	winner, won, _ := t.play.Outcome()
	if t.abandoned {
		// A match nobody finished has no winner. Nothing is forfeited: with
		// no verb that takes another seat's bond, an abandonment that paid
		// the seat still present would be one the other seat never signs.
		won = false
	}
	policy := *t.policy
	// Set before the runtime asks, because it asks through WillCoSign on this
	// same call, and the lock must be released before it does: the mutex is
	// not reentrant.
	t.allowed = true
	g.mu.Unlock()

	out := sdk.Outcome{Void: true}
	if won {
		shares, err := policy.Winner(winner)
		if err != nil {
			return err
		}
		out = sdk.Outcome{Shares: shares}
	}
	if err := rt.Settle(ctx, sid, out); err != nil {
		return err
	}

	g.mu.Lock()
	t.settled = true
	g.mu.Unlock()
	return nil
}

// Tables are the sessions this game has open, newest state included. The
// client asks rather than being told, because a table can arrive while no
// screen is looking at it.
func (g *Game) Tables() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.tables))
	for sid := range g.tables {
		out = append(out, sid)
	}
	sort.Strings(out)
	return out
}

// headHex renders a chain head for a status report.
func headHex(c *movelog.Chain) string {
	head, _ := c.Head()
	return hex.EncodeToString(head[:])
}

// UseLogger gives the game's own subsystem logger. Call it before any table is
// opened; a game with no logger set says nothing, which is the default.
func UseLogger(l slog.Logger) { log = l }

var log = slog.Disabled

// Prepare advances a table towards play.
//
// Called from the driver's chain loop rather than from a message handler,
// because it does chain I/O. It follows the order the SDK asks for: wait for a
// complete seated roster, verify every seat's admission bond on chain, then
// re-check the deposits. A seated phase is agreement on who is at the table; it
// is not evidence that anybody paid.
func (g *Game) Prepare(ctx context.Context, sid string) error {
	rt, err := g.runtime()
	if err != nil {
		return err
	}
	t, err := g.table(sid)
	if err != nil {
		return err
	}
	snap, err := rt.Snapshot(sid)
	if err != nil {
		return err
	}

	g.mu.Lock()
	t.phase, t.snap, t.seen = snap.Phase, snap, true
	verified := t.verified
	g.mu.Unlock()

	if snap.Record.RecoveryOnly || snap.Record.Aborted {
		return nil
	}
	if want := int(rt.Terms(sid).Seats); want == 0 || len(snap.Seats) != want {
		return nil
	}
	if !verified {
		if err := rt.CheckAdmissionBonds(ctx, sid); err != nil {
			return fmt.Errorf("admission bonds are not confirmed: %w", err)
		}
		g.mu.Lock()
		t.verified = true
		g.mu.Unlock()
		log.Infof("table %s: every seat's admission bond is confirmed on chain", sid)
	}
	if tip, err := rt.Chain(ctx); err == nil && tip.Height > 0 {
		g.adjudicate(t, uint32(tip.Height))
	}
	refreshed, err := rt.RefreshDeposits(ctx, sid)
	if err != nil {
		return fmt.Errorf("deposits could not be checked: %w", err)
	}
	g.mu.Lock()
	t.snap = refreshed
	t.staked = stakesConfirmed(refreshed, int(rt.Terms(sid).Seats), t.staked)
	g.mu.Unlock()
	g.notePayout(t, refreshed)
	g.freeze(sid)
	return nil
}

// notePayout notices that the settlement went through.
//
// The runtime has a Settled hook and it never fires - nothing in the SDK calls
// the function that would deliver it - so the only way to learn a payout
// completed is to watch this seat's own stake become spent.
//
// This seat's own, specifically. The bridge reports financial state for
// deposits it is the authority for, so after a payout the *other* seat's stake
// reads as missing rather than spent, and reading that as a failure would be
// exactly backwards.
func (g *Game) notePayout(t *table, snap sdk.TableSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t.paid {
		return
	}
	for _, d := range snap.Deposits {
		if d.Purpose != tablelobby.PurposeStake || d.Seat != t.seat {
			continue
		}
		if d.Check != tablelobby.CheckSpent && d.Check != tablelobby.CheckSpending {
			continue
		}
		t.paid = true
		t.allowed = false // nothing more to co-sign for this table
		if err := t.journal.Note("paid"); err != nil {
			log.Errorf("table %s: the payout could not be recorded: %v", t.chain.MatchID(), err)
		}
		log.Infof("table %s: the stake was spent; the payout has been made", t.chain.MatchID())
		return
	}
}

// Paid reports that this seat's stake has been spent by the settlement, which
// is the only evidence available that the payout actually happened.
func (g *Game) Paid(sid string) bool {
	t, err := g.table(sid)
	if err != nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return t.paid
}

// freeze records what the table actually holds, once, as soon as every seat's
// stake is confirmed.
//
// After this the amounts never move. Settle is called on a timer and after a
// restart, and every one of those calls must name the same numbers: the bridge
// identifies a payout by the transaction it builds, so two proposals that
// differ by an atom are two transactions, each half-signed, and the pot waits
// out the refund locks.
func (g *Game) freeze(sid string) {
	rt, err := g.runtime()
	if err != nil {
		return
	}
	t, err := g.table(sid)
	if err != nil {
		return
	}
	if ok, _ := g.Playable(sid); !ok {
		return
	}
	stakes := make([]int64, match.Seats)
	for seat := range stakes {
		_, atoms, ok := rt.Funded(sid, uint32(seat))
		if !ok {
			return
		}
		stakes[seat] = atoms
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if t.policy != nil {
		// Already frozen. If the table no longer holds what was agreed,
		// stop: settling against numbers that moved is how a proposal is
		// built that the other seat will never match.
		if !t.policy.Same(stakes) {
			t.blocked = "the stakes this table holds changed after they were agreed"
			log.Errorf("table %s: %s", sid, t.blocked)
		}
		return
	}
	p, err := payout.Freeze(stakes)
	if err != nil {
		t.blocked = err.Error()
		log.Errorf("table %s: the stakes cannot be divided: %v", sid, err)
		return
	}
	t.policy = &p
	id, _ := p.ID()
	log.Infof("table %s: stakes agreed, pot %d atoms (%x)", sid, p.Pot(), id[:4])
}

// WillCoSign is the last refusal before this peer's signature joins a payout.
//
// Asked once per seat by the runtime, with no lock of its own held, so it must
// be quick and must not call back in. It answers from one fact: whether this
// peer has itself reached a settled outcome for this table. A peer's claim
// never sets it, and neither does the screen.
func (g *Game) WillCoSign(sid string, _ uint32) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tables[sid]
	if !ok || t.blocked != "" || t.policy == nil {
		return false
	}
	return t.allowed
}

// Lobby is the seating screen's view of a table while it forms.
//
// Derived from the last snapshot this peer took rather than from anything a
// peer said. stale marks evidence that is no longer current - a disconnected
// bridge, or an unknown chain height - and collapses every claim on the screen
// rather than leaving yesterday's certainty on display.
func (g *Game) Lobby(sid string, height uint32, stale bool) (tablelobby.View, bool) {
	rt, err := g.runtime()
	if err != nil {
		return tablelobby.View{}, false
	}
	t, err := g.table(sid)
	if err != nil {
		return tablelobby.View{}, false
	}
	g.mu.Lock()
	snap, seen, verified, seat := t.snap, t.seen, t.verified, t.seat
	g.mu.Unlock()
	if !seen {
		return tablelobby.View{}, false
	}
	terms := rt.Terms(sid)
	_, _, funded := rt.Funded(sid, seat)
	return derive(snap, terms, seat, verified, funded, height, stale), true
}

// derive turns a runtime snapshot into the seating view.
func derive(snap sdk.TableSnapshot, terms membership.Terms, mine uint32, verified, funded bool, height uint32, stale bool) tablelobby.View {
	v := tablelobby.View{
		Match:  snap.Record.Match,
		Height: height,
		Stale:  stale,
		Terms: tablelobby.Terms{
			BuyInAtoms:   int64(terms.BuyInAtoms),
			BondAtoms:    int64(terms.BondAtoms),
			Seats:        terms.Seats,
			RefundBlocks: terms.CSVBlocks,
			BondBlocks:   terms.BondLockBlocks,
			Until:        terms.Until,
		},
	}

	count := int(terms.Seats)
	if count < int(match.Seats) {
		count = int(match.Seats)
	}
	v.Seats = make([]tablelobby.Seat, count)
	for i := range v.Seats {
		v.Seats[i] = tablelobby.Seat{
			Number: uint32(i),
			You:    uint32(i) == mine,
			Name:   fmt.Sprintf("Seat %d", i),
		}
		if uint32(i) == mine {
			v.Seats[i].Name = "You"
		}
	}
	for _, d := range snap.Deposits {
		if int(d.Seat) >= len(v.Seats) {
			continue
		}
		v.Seats[d.Seat].Deposits = append(v.Seats[d.Seat].Deposits, tablelobby.Deposit{
			Purpose:               d.Purpose,
			Atoms:                 d.Atoms,
			Confirmations:         d.Confirmations,
			RequiredConfirmations: d.RequiredConfirmations,
			Check:                 d.Check,
			Error:                 d.Error,
			// The runtime sets these two only after re-deriving the
			// expected script and comparing the output itself.
			Local: d.Check == tablelobby.CheckVerified || d.Check == tablelobby.CheckConfirming,
		})
	}

	v.Stage = stage(snap, terms, verified, v)
	if snap.Record.Aborted || snap.Record.RecoveryOnly {
		v.ClosedReason = snap.Record.Reason
		if v.ClosedReason == "" {
			v.ClosedReason = snap.Record.RecoveryReason
		}
	}
	v.CanFund = !funded && v.Stage == tablelobby.StageStake && !stale
	v.NextStep, v.NextDetail = tablelobby.Guidance(v)
	return v
}

// stage is the first thing the table is still waiting for, most advanced first.
func stage(snap sdk.TableSnapshot, terms membership.Terms, verified bool, v tablelobby.View) tablelobby.Stage {
	if snap.Record.Aborted || snap.Record.RecoveryOnly {
		return tablelobby.StageClosed
	}
	staked := 0
	for _, s := range v.Seats {
		if d, ok := s.Deposit(tablelobby.PurposeStake); ok && d.Complete() {
			staked++
		}
	}
	if verified && staked == len(v.Seats) && len(v.Seats) > 0 {
		return tablelobby.StageReady
	}
	for _, s := range v.Seats {
		if _, ok := s.Deposit(tablelobby.PurposeStake); ok {
			return tablelobby.StageStake
		}
	}
	if snap.Phase == "seated" && len(snap.Seats) == int(terms.Seats) {
		return tablelobby.StageStake
	}
	if len(snap.Record.Joins) == int(terms.Seats) && terms.Seats > 0 {
		return tablelobby.StageSeatDraw
	}
	if snap.Record.SeatBond.Outpoint != "" {
		return tablelobby.StageRoster
	}
	if snap.Record.Match != "" {
		return tablelobby.StageAdmission
	}
	return tablelobby.StageInvitation
}

// Ready reports whether this peer has verified the table and may play.
func (g *Game) Ready(sid string) bool {
	t, err := g.table(sid)
	if err != nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return t.verified
}

// head and next are the chain's position, for building an entry before the
// chain is asked to sign one. Called with the game's lock held.
func head(t *table) [32]byte { h, _ := t.chain.Head(); return h }
func next(t *table) uint64   { _, seq := t.chain.Head(); return seq }

// stakesConfirmed reports whether every seat's stake is confirmed on chain,
// given what was true before.
//
// A stake that reads "unavailable" is one this peer could not ask about, which
// is not the same as one that is gone: the chain lookup behind it fails on any
// transient, and a table that stopped being playable every time a call went
// unanswered would abandon matches over nothing. So that answer keeps whatever
// was already known. Every other answer is an answer, and counts.
func stakesConfirmed(snap sdk.TableSnapshot, seats int, was bool) bool {
	if seats <= 0 {
		return false
	}
	confirmed, unknown := 0, false
	for _, d := range snap.Deposits {
		if d.Purpose != tablelobby.PurposeStake {
			continue
		}
		switch d.Check {
		case tablelobby.CheckVerified:
			confirmed++
		case tablelobby.CheckUnavailable:
			unknown = true
		}
	}
	if confirmed == seats {
		return true
	}
	if unknown {
		return was
	}
	return false
}
