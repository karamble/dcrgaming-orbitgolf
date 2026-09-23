package match

import (
	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/sim"
	"reflect"
	"testing"
)

func TestTurnsValidationAndReplay(t *testing.T) {
	m, _ := New(0)
	for _, v := range []Move{{0, 1, sim.Shot(0, 1)}, {1, 0, sim.Shot(0, 1)}, {0, 0, 0}, {0, 9, 1}} {
		if m.Play(v) == nil {
			t.Fatalf("accepted invalid move %+v", v)
		}
	}
	var moves []Move
	for !m.Done() {
		v := Move{uint8(m.Index()), m.Turn(), sim.Shot(0, 1)}
		moves = append(moves, v)
		if err := m.Play(v); err != nil {
			t.Fatal(err)
		}
		if len(moves) > Holes*Seats*StrokeCap {
			t.Fatal("unbounded match")
		}
	}
	if _, won, done := m.Outcome(); won || !done {
		t.Fatal("expected drawn capped match")
	}
	if len(m.Results()) != 9 {
		t.Fatal("draw ended early")
	}
	replayed, err := Replay(0, moves)
	if err != nil || !reflect.DeepEqual(m, replayed) {
		t.Fatalf("replay differs: %v", err)
	}
	if m.Play(moves[0]) == nil {
		t.Fatal("finished match accepted shot")
	}
}

// Place a ball just before the cup to exercise scoring independently of aiming.
func nearCup(m *Match, s uint32, strokes int) {
	h := course.All()[m.index]
	m.state.Balls[s] = sim.Ball{Pos: course.Vec{X: h.Cup.X - 1000, Y: sim.Radius, Z: h.Cup.Z}, Strokes: strokes}
}

func TestEarlyClinchAndAlternatingOpeners(t *testing.T) {
	m, _ := New(0)
	for hole := 0; hole < 5; hole++ {
		if m.Turn() != uint32(hole%2) {
			t.Fatal("opener did not alternate")
		}
		nearCup(m, 0, 0)
		nearCup(m, 1, 1)
		for range 2 {
			if err := m.Play(Move{uint8(hole), m.Turn(), sim.Shot(0, 1)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if winner, won, done := m.Outcome(); winner != 0 || !won || !done {
		t.Fatal("5–0 should clinch")
	}
	if len(m.Results()) != 5 {
		t.Fatal("wrong clinch hole")
	}
}

func TestFinishedBallSkippedAndBallsDoNotCollide(t *testing.T) {
	m, _ := New(0)
	nearCup(m, 0, 0)
	other := m.state.Balls[1]
	if err := m.Play(Move{0, 0, sim.Shot(0, 1)}); err != nil {
		t.Fatal(err)
	}
	if m.state.Balls[1] != other {
		t.Fatal("other ball was moved")
	}
	if !m.state.Balls[0].Holed {
		t.Fatal("expected cup")
	}
	if err := m.Play(Move{0, 1, sim.Shot(0, 1)}); err != nil {
		t.Fatal(err)
	}
	if m.Turn() != 1 {
		t.Fatal("holed player offered another shot")
	}
}
