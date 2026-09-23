package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/slog"
	"github.com/karamble/dcrgaming-orbitgolf/internal/session"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/sim"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/bridgetest"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/connect"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/gamingpb"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/schema"
	"github.com/karamble/dcrgaming-sdk/pkg/gaming/transport"
	"github.com/karamble/dcrgaming-sdk/pkg/identity"
	sdk "github.com/karamble/dcrgaming-sdk/pkg/runtime"
)

// testLog is off unless a probing test turns it on.
var testLog = slog.Disabled

const (
	tableSID  = "abcdef01"
	tableGCID = "1111111111111111111111111111111111111111111111111111111111111111"
	// The amounts the live test plays for: 0.001 DCR in, 0.01 DCR bonded.
	buyIn     = 100_000
	admission = 1_000_000
	until     = 810
)

// peer is one player: its own game, its own runtime, its own connection to the
// bridge. Two of them share nothing but the bridge, which is the arrangement
// the real thing has.
type peer struct {
	game *session.Game
	rt   *sdk.Runtime
	// dir is this peer's profile, and stop releases the runtime's hold on it
	// so the same profile can be reopened by a fresh process.
	dir    string
	name   string
	stop   context.CancelFunc
	done   chan struct{}
	srv    *bridgetest.Server
	params stdaddr.AddressParams
}

func standTwo(t *testing.T) (*bridgetest.Bridge, [2]*peer) {
	t.Helper()
	params := chaincfg.TestNet3Params()
	fake := bridgetest.New(bridgetest.Options{
		Game: session.GameID, Network: "testnet3", Params: params, Height: 800,
	})
	srv, err := fake.Serve("seat0", "seat1")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	t.Cleanup(srv.Close)

	var peers [2]*peer
	for i, name := range []string{"seat0", "seat1"} {
		p := &peer{dir: t.TempDir(), name: name, srv: srv, params: params}
		openPeer(t, p)
		peers[i] = p
	}
	return fake, peers
}

// openPeer starts a peer over its own profile directory with file-backed
// stores, so a restart reads back what the last process wrote rather than
// starting clean.
func openPeer(t *testing.T, p *peer) {
	t.Helper()
	g, err := session.New(p.dir)
	if err != nil {
		t.Fatalf("%s game: %v", p.name, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	id := g.Identity()
	conn, err := p.srv.Dial(ctx, p.name, func(cfg *transport.BridgeConfig) { connect.Stamp(cfg, id) })
	if err != nil {
		cancel()
		t.Fatalf("%s dial: %v", p.name, err)
	}
	seed, err := identity.Load(p.dir)
	if err != nil {
		cancel()
		t.Fatalf("%s identity: %v", p.name, err)
	}
	// Params explicitly: the fake bridge has not said hello. TickEvery is off
	// because this test drives the height itself.
	rt, err := sdk.Open(sdk.Config{
		Rules: g, Bridge: conn, Identity: seed, Dir: p.dir,
		SeatTags: session.SeatTags, Params: p.params, TickEvery: -1, Log: testLog,
	})
	if err != nil {
		cancel()
		t.Fatalf("%s runtime: %v", p.name, err)
	}
	g.Bind(rt)
	done := make(chan struct{})
	go func() { defer close(done); _ = rt.Run(ctx) }()

	p.game, p.rt, p.stop, p.done = g, rt, cancel, done
	t.Cleanup(func() { p.close() })
}

// close releases this peer's hold on its profile, the way a process exiting
// does. The runtime takes exclusive ownership of the stores, so it has to stop
// before anything may open the same directory again.
func (p *peer) close() {
	if p.stop == nil {
		return
	}
	p.stop()
	<-p.done
	_ = p.game.Close()
	_ = p.rt.Close()
	p.stop = nil
}

// advance moves the fake chain on and lets both runtimes see it, which is what
// a game does as blocks arrive.
func advance(t *testing.T, fake *bridgetest.Bridge, peers [2]*peer, blocks int64) {
	t.Helper()
	fake.Mine(blocks)
	tick(t, fake, peers)
}

func tick(t *testing.T, fake *bridgetest.Bridge, peers [2]*peer) {
	t.Helper()
	h := fake.Height()
	for _, p := range peers {
		p.rt.Tick(context.Background(), h)
	}
}

// waitFor polls until want is true, ticking as it goes, and says what it was
// waiting for when it gives up.
func waitFor(t *testing.T, fake *bridgetest.Bridge, peers [2]*peer, what string, want func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !want() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		tick(t, fake, peers)
		time.Sleep(10 * time.Millisecond)
	}
}

func invite(t *testing.T) string {
	t.Helper()
	inv := schema.Invite{
		Game: session.GameID, Kind: schema.InviteKindTable, SID: tableSID,
		BuyInAtoms: buyIn, Seats: 2, CSVBlocks: session.RefundBlocks, Until: until,
		AdmissionAtoms: admission, AdmissionBlocks: session.BondLockBlocks,
	}
	link, err := inv.String()
	if err != nil {
		t.Fatalf("render the invitation: %v", err)
	}
	return link
}

// settleFormation confirms both admission bonds and waits for both peers to
// bind themselves to the same roster.
//
// It deliberately does not let the chain run past the admission deadline while
// it waits. Admission shuts at that height, and a table whose joins have not
// been exchanged by then aborts rather than seats - which is a real property of
// the protocol, and a test that mined straight through it would be testing a
// table that never formed.
func settleFormation(t *testing.T, fake *bridgetest.Bridge, peers [2]*peer) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		bound := true
		for _, p := range peers {
			snap, err := p.rt.Snapshot(tableSID)
			if err != nil || snap.Phase != "settled" {
				bound = false
			}
		}
		if bound {
			return
		}
		if time.Now().After(deadline) {
			for i, p := range peers {
				snap, err := p.rt.Snapshot(tableSID)
				t.Logf("peer %d phase %q (%v) at height %d", i, snap.Phase, err, fake.Height())
			}
			t.Fatal("timed out waiting for both peers to bind to one roster")
		}
		if fake.Height() < until-2 {
			fake.Mine(1)
		}
		tick(t, fake, peers)
		time.Sleep(10 * time.Millisecond)
	}
}

// TestTwoPeersAreDrawnIntoDifferentSeats is the lifecycle as far as seating,
// with nothing stubbed between the two games: real mTLS, the real runtime on
// both sides, and the seat draw taken from a block.
func TestTwoPeersAreDrawnIntoDifferentSeats(t *testing.T) {
	_, _, bySeat := seated(t)
	if bySeat[0] == bySeat[1] {
		t.Fatal("both seats were filled by the same peer")
	}
}

// seated runs the lifecycle as far as a drawn, seated table and reports which
// peer the beacon put in which seat.
func seated(t *testing.T) (*bridgetest.Bridge, [2]*peer, [2]*peer) {
	t.Helper()
	fake, peers := standTwo(t)
	waitFor(t, fake, peers, "both games to subscribe", func() bool {
		return fake.Subscribers() == 2
	})
	if n := fake.Ask(&gamingpb.BridgeRequest{
		RequestId: "r1",
		Req: &gamingpb.BridgeRequest_AcceptInvite{
			AcceptInvite: &gamingpb.AcceptInvite{Invite: invite(t), Gcid: tableGCID},
		},
	}); n != 2 {
		t.Fatalf("the invitation reached %d games, want 2", n)
	}
	waitFor(t, fake, peers, "both admission bonds to be requested", func() bool {
		return len(fake.Spends()) >= 2
	})
	settleFormation(t, fake, peers)
	for fake.Height() <= until+2 {
		advance(t, fake, peers, 1)
	}
	waitFor(t, fake, peers, "both peers to be seated", func() bool {
		for _, p := range peers {
			if _, ok := p.game.View(tableSID); !ok {
				return false
			}
		}
		return true
	})

	var bySeat [2]*peer
	for _, p := range peers {
		view, _ := p.game.View(tableSID)
		if view.Seat > 1 {
			t.Fatalf("a peer was drawn into seat %d", view.Seat)
		}
		bySeat[view.Seat] = p
	}
	if bySeat[0] == nil || bySeat[1] == nil {
		t.Fatal("the draw did not fill both seats")
	}
	return fake, peers, bySeat
}

func fundPeers(t *testing.T, fake *bridgetest.Bridge, peers [2]*peer) {
	t.Helper()
	ctx := context.Background()
	waitFor(t, fake, peers, "signed rules before funding", func() bool {
		ready := true
		for _, p := range peers {
			if err := p.game.Prepare(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if err := p.game.Start(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if ok, _ := p.game.CanFund(tableSID); !ok {
				ready = false
			}
		}
		return ready
	})
	for _, p := range peers {
		if err := p.game.Fund(ctx, tableSID); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAWholeMatchIsPlayedAndSettledOverABridge runs the money path end to end
// against the fake bridge: stakes funded through it, every move carried by it,
// and a payout proposed to it by both seats.
func TestAWholeMatchIsPlayedAndSettledOverABridge(t *testing.T) {
	fake, peers, bySeat := seated(t)
	ctx := context.Background()

	fundPeers(t, fake, peers)
	advance(t, fake, peers, 4)
	waitFor(t, fake, peers, "both stakes to be funded and seen by both peers", func() bool {
		for _, p := range peers {
			for seat := uint32(0); seat < 2; seat++ {
				if _, atoms, ok := p.rt.Funded(tableSID, seat); !ok || atoms != buyIn {
					return false
				}
			}
		}
		return true
	})

	// Verify the roster's admission bonds on chain, then state the rules to
	// each other. Both are what the client's own chain loop does.
	waitFor(t, fake, peers, "both peers to verify the table", func() bool {
		ready := true
		for _, p := range peers {
			if err := p.game.Prepare(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if !p.game.Ready(tableSID) {
				ready = false
			}
		}
		return ready
	})
	for i, p := range peers {
		if err := p.game.Start(ctx, tableSID); err != nil {
			t.Fatalf("peer %d stating its rules: %v", i, err)
		}
	}
	waitFor(t, fake, peers, "both peers to agree the rules and the money", func() bool {
		ready := true
		for _, p := range peers {
			// The chain loop is what freezes the pot once everything holds.
			if err := p.game.Prepare(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if ok, _ := p.game.Playable(tableSID); !ok {
				ready = false
			}
		}
		return ready
	})

	// Every frame from here to the end of the match should be a move and
	// nothing else.
	before := len(fake.Sent())

	moves := 0
	for {
		view, _ := bySeat[0].game.View(tableSID)
		if view.Done {
			break
		}
		if moves >= 216 {
			t.Fatal("match exceeded stroke cap")
		}
		if err := bySeat[view.Turn].game.Play(ctx, tableSID, sim.Shot(0, 1)); err != nil {
			t.Fatal(err)
		}
		moves++
		waitFor(t, fake, peers, "the shot to reach the other seat", func() bool {
			a, _ := peers[0].game.View(tableSID)
			b, _ := peers[1].game.View(tableSID)
			return a.Moves == b.Moves && a.Head == b.Head && a.Grid == b.Grid
		})
	}
	if sent := len(fake.Sent()) - before; sent != moves {
		t.Fatalf("%d frames carried %d moves; a move is one frame and a played match sends nothing else", sent, moves)
	}

	first, _ := peers[0].game.View(tableSID)
	second, _ := peers[1].game.View(tableSID)
	if !first.Done || !second.Done {
		t.Fatal("the peers disagree that the match ended")
	}
	if first.Winner != second.Winner || first.Won != second.Won {
		t.Fatalf("the peers disagree on the result: %d/%v and %d/%v",
			first.Winner, first.Won, second.Winner, second.Won)
	}
	if first.Won || first.Score != [2]int{} {
		t.Fatal("capped holes must draw")
	}

	// Both peers verified the same log and both ask their own bridge to pay.
	if err := fake.SetPayoutVerdict(bridgetest.Approve); err != nil {
		t.Fatalf("set payout verdict: %v", err)
	}
	for i, p := range peers {
		if err := p.game.Settle(ctx, tableSID); err != nil {
			t.Fatalf("peer %d settling: %v", i, err)
		}
	}
	for i, p := range peers {
		if p.rt.PayoutFor(tableSID) == "" {
			t.Fatalf("peer %d proposed no payout", i)
		}
	}

	// And the transcript both sides hold audits to the same winner.
	blob, err := peers[0].game.Transcript(tableSID)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	other, err := peers[1].game.Transcript(tableSID)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	if string(blob) != string(other) {
		t.Fatal("the two peers hold different histories of the same match")
	}
}

// A process dies mid-match and comes back. Everything it needs is on disk or on
// the chain; nothing may depend on a hook that fires once, or on state that
// only ever lived in memory. If this fails on mainnet, two funded stakes wait
// out their refund locks.
func TestAMatchSurvivesARestartAgainstTheRealRuntime(t *testing.T) {
	fake, peers, bySeat := seated(t)
	ctx := context.Background()

	fundPeers(t, fake, peers)
	advance(t, fake, peers, 4)
	waitFor(t, fake, peers, "both peers to be ready to play", func() bool {
		ready := true
		for _, p := range peers {
			if err := p.game.Prepare(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if err := p.game.Start(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if ok, _ := p.game.Playable(tableSID); !ok {
				ready = false
			}
		}
		return ready
	})

	// Three moves in, then the first seat's process dies.
	for _, col := range []uint32{sim.Shot(3072, 200), sim.Shot(3072, 300), sim.Shot(0, 100)} {
		view, _ := bySeat[0].game.View(tableSID)
		if err := bySeat[view.Turn].game.Play(ctx, tableSID, col); err != nil {
			t.Fatalf("column %d: %v", col, err)
		}
		waitFor(t, fake, peers, "the move to reach the other seat", func() bool {
			a, _ := peers[0].game.View(tableSID)
			b, _ := peers[1].game.View(tableSID)
			return a.Moves == b.Moves
		})
	}
	before, ok := peers[0].game.View(tableSID)
	if !ok {
		t.Fatal("peer 0 has no table")
	}

	peers[0].close()
	openPeer(t, peers[0])
	// A restarted peer is never told about its table again: the hook that
	// opened it fires once, when the seats are drawn. It has to reopen from
	// what is on disk.
	if err := peers[0].game.Ensure(tableSID); err != nil {
		t.Fatalf("the table did not reopen after a restart: %v", err)
	}
	after, ok := peers[0].game.View(tableSID)
	if !ok {
		t.Fatal("the table did not come back")
	}
	if after.Moves != before.Moves || after.Head != before.Head {
		t.Fatalf("it came back at move %d/%s, was %d/%s",
			after.Moves, after.Head[:8], before.Moves, before.Head[:8])
	}
	if after.Hole != before.Hole || after.Turn != before.Turn || after.Score != before.Score {
		t.Fatalf("it came back on hole %d, seat %d to play, score %v; was %d/%d/%v",
			after.Hole, after.Turn, after.Score, before.Hole, before.Turn, before.Score)
	}

	// And it can carry on: the stakes are still known, the rules re-agree from
	// the durable frames, and no second payment is asked for.
	spends := len(fake.Spends())
	waitFor(t, fake, peers, "the revived peer to be playable again", func() bool {
		ready := true
		for _, p := range peers {
			if err := p.game.Prepare(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if err := p.game.Start(ctx, tableSID); err != nil {
				ready = false
				continue
			}
			if ok, _ := p.game.Playable(tableSID); !ok {
				ready = false
			}
		}
		return ready
	})
	if got := len(fake.Spends()); got != spends {
		t.Fatalf("a restart asked for %d more payments", got-spends)
	}

	view, _ := peers[0].game.View(tableSID)
	if err := bySeat[view.Turn].game.Play(ctx, tableSID, 1); err != nil {
		t.Fatalf("the match could not continue after the restart: %v", err)
	}
	waitFor(t, fake, peers, "the move after the restart to reach the other seat", func() bool {
		a, _ := peers[0].game.View(tableSID)
		b, _ := peers[1].game.View(tableSID)
		return a.Moves == b.Moves && a.Head == b.Head
	})
}
