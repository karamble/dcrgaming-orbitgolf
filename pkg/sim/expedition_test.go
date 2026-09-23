package sim

import (
	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
	"reflect"
	"testing"
)

func TestRampRollsBackIncludingLowGravity(t *testing.T) {
	for _, gravity := range []int64{3, 7} {
		h := course.Hole{Gravity: gravity, Cup: course.V(20, 0, 0), Platforms: []course.Platform{
			{Z: 10 * course.Unit, W: 12 * course.Unit, D: 4 * course.Unit},
			{W: 12 * course.Unit, D: 16 * course.Unit, Y: 3 * course.Unit, Rise: -3 * course.Unit},
		}}
		start := Ball{Pos: course.V(0, 0, 5)}
		start.Pos.Y = ground(h, start.Pos.X, start.Pos.Z) + Radius
		r, err := Simulate(h, start, Shot(3072, 80))
		if err != nil || r.Penalty || r.Ball.Pos.Z <= 8*course.Unit || r.Ball.Pos.Y != Radius {
			t.Fatalf("gravity %d: ball failed to roll back onto flat: %+v %v", gravity, r.Ball, err)
		}
		climbed := false
		for _, p := range r.Path {
			if p.Z < start.Pos.Z {
				climbed = true
			}
		}
		if !climbed {
			t.Fatal("fixture never climbed before reversing")
		}
	}
}

func TestTimedGatePhaseIsPartOfShot(t *testing.T) {
	h := flat()
	h.Cup = course.V(0, 0, -40)
	h.Gates = []course.Gate{{Z: -3 * course.Unit, W: 20 * course.Unit, H: 3 * course.Unit, D: course.Unit / 3, Period: 360, Open: 144, Laser: true}}
	open, _ := Simulate(h, Tee(h), ShotAt(3072, 300, 0))
	closed, _ := Simulate(h, Tee(h), ShotAt(3072, 300, 180))
	for phase := uint32(0); phase < course.Cycle; phase += course.PhaseStep {
		a, _ := Simulate(h, Tee(h), ShotAt(3072, 300, phase))
		b, _ := Simulate(h, Tee(h), ShotAt(3072, 300, phase))
		if !reflect.DeepEqual(a, b) {
			t.Fatal("timed obstacle replay differs", phase)
		}
	}
	if open.Penalty || closed.Penalty || open.Bounces != 0 || closed.Bounces == 0 || open.Ball.Pos == closed.Ball.Pos {
		t.Fatalf("phase did not control crossing: open=%+v closed=%+v", open.Ball, closed.Ball)
	}
	if Phase(ShotAt(3072, 300, 180)) != 180 || Validate(ShotAt(3072, 300, 720)) == nil || Validate(ShotAt(3072, 300, 1)) == nil {
		t.Fatal("bad phase validation")
	}
	for _, laser := range []bool{false, true} {
		g := h.Gates[0]
		g.Laser = laser
		for tick := 0; tick < course.Cycle; tick++ {
			a, aa := course.GateWall(g, tick)
			b, bb := course.GateWall(g, tick+course.Cycle)
			if a != b || aa != bb {
				t.Fatal("obstacle cycle drifted")
			}
		}
	}
}

// Frozen safe routes found by the offline survey. These are existence proofs,
// not optimal scores or claims about human difficulty. No penalties are needed.
func TestEveryExpeditionCanBeFinished(t *testing.T) {
	routes := [][]uint32{
		{0x326398, 0x2e2b34, 0x2c7694, 0x31fb0c},
		{0x3806e4, 0x2b93ac, 0x1e31f3c0, 0x3302a8, 0x2dd384},
		{0x31b7d4, 0x2c2370, 0x2bcea8, 0x31d7e8},
		{0x31c3e8, 0x31d5f4, 0x2e1fd4, 0x2ccbe8},
		{0x2f2fe8, 0x2de5a4, 0x3177d4, 0x2f2ad0, 0x333208, 0x28761c},
		{0x37a30c, 0x2c0fe8, 0x1e3247d4, 0x3276e4, 0x2be7c0, 0x3a0de0},
		{0x31a75c, 0x29fed0, 0x32770c, 0x297f34},
		{0x316384, 0x2d4ae4, 0x3306f8, 0x2c6fac, 0x2096f8},
		{0x3283d4, 0x2bf30c, 0x3c340348, 0x331694, 0x290680},
	}
	for index, h := range course.All() {
		ball := Tee(h)
		for _, shot := range routes[index] {
			r, err := Simulate(h, ball, shot)
			if err != nil || r.Penalty {
				t.Fatalf("%s unsafe route: %v", h.ID, err)
			}
			ball = r.Ball
		}
		if !ball.Holed || ball.Strokes > h.Par || ball.Strokes > 12 {
			t.Fatalf("%s cannot be finished within par: %+v", h.ID, ball)
		}
	}
}

func TestTunnelCeilingBlocksAnAscendingBall(t *testing.T) {
	h := course.Hole{Gravity: 7, Cup: course.V(0, 0, -20), Tee: course.V(0, 0, 4), Platforms: []course.Platform{
		{Z: 4 * course.Unit, W: 10 * course.Unit, D: 4 * course.Unit},
		{W: 10 * course.Unit, D: 4 * course.Unit, Y: 2 * course.Unit, Rise: -2 * course.Unit},
		{Z: -10 * course.Unit, W: 10 * course.Unit, D: 16 * course.Unit},
	}, Walls: []course.Wall{{Y: 3 * course.Unit, Z: -4 * course.Unit, W: 10 * course.Unit, H: course.Unit, D: 4 * course.Unit, Ceiling: true}}}
	r, err := Simulate(h, Tee(h), Shot(3072, 700))
	if err != nil || r.Bounces == 0 {
		t.Fatal("launch did not hit roof", err)
	}
	for _, p := range r.Path {
		if p.Z < -2*course.Unit && p.Z > -6*course.Unit && p.Y > 3*course.Unit-Radius {
			t.Fatal("ball passed through tunnel roof", p)
		}
	}
}

func TestExpeditionCourseGeometry(t *testing.T) {
	for _, h := range course.All() {
		if h.Tee.Z-h.Cup.Z < 120*course.Unit {
			t.Errorf("%s remains too short", h.ID)
		}
		if ground(h, h.Tee.X, h.Tee.Z) != h.Tee.Y || ground(h, h.Cup.X, h.Cup.Z) != h.Cup.Y {
			t.Errorf("%s tee/cup floats above deck", h.ID)
		}
		up, down := false, false
		for _, p := range h.Platforms {
			up = up || p.Rise < 0
			down = down || p.Rise > 0
		}
		// The two-launch moon course descends through airborne gaps.
		if !up || (!down && h.ID != "moon-01") {
			t.Errorf("%s lacks elevation variety", h.ID)
		}
		for _, g := range h.Gates {
			if course.Cycle%g.Period != 0 || g.Open <= 48 || g.Open >= g.Period {
				t.Fatal("invalid timed obstacle")
			}
			if ground(h, g.X, g.Z) != g.Y {
				t.Errorf("%s gate floats above floor", h.ID)
			}
		}
	}
}
