package sim

import (
	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
	"math"
	"os"
	"sort"
	"testing"
)

// Development-only search for regression routes. Never part of runtime rules.
func TestSurvey(t *testing.T) {
	if os.Getenv("ORBIT_SURVEY") == "" {
		t.Skip("offline course survey")
	}
	for _, h := range course.All() {
		var targets []course.Vec
		for _, w := range h.Walls {
			if w.H == 2*course.Unit && w.D == course.Unit && !w.Ceiling {
				x := int64(0)
				if w.X < 0 {
					x = w.X + w.W/2 + 2*course.Unit
				} else {
					x = w.X - w.W/2 - 2*course.Unit
				}
				targets = append(targets, course.Vec{X: x, Y: w.Y, Z: w.Z - 3*course.Unit})
			}
		}
		for _, w := range h.Walls {
			if w.Ceiling {
				targets = append(targets, course.Vec{X: 0, Y: w.Y - 3*course.Unit, Z: w.Z - w.D/2 - course.Unit})
			}
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].Z > targets[j].Z })
		targets = append(targets, h.Cup)
		ball := Tee(h)
		var shots []uint32
		for _, target := range targets {
			for attempt := 0; attempt < 4 && !ball.Holed; attempt++ {
				best := math.Inf(1)
				var chosen uint32
				var next Result
				base := int(math.Round(math.Atan2(float64(target.Z-ball.Pos.Z), float64(target.X-ball.Pos.X)) * 4096 / (2 * math.Pi)))
				phases := []uint32{0}
				if len(h.Gates) > 0 {
					phases = []uint32{0, 120, 240}
				}
				for _, phase := range phases {
					for offset := -160; offset <= 160; offset += 16 {
						for power := uint32(40); power <= 1000; power += 20 {
							shot := ShotAt(uint32((base+offset+8192)%4096), power, phase)
							r, _ := Simulate(h, ball, shot)
							if r.Penalty {
								continue
							}
							dx, dz := float64(r.Ball.Pos.X-target.X), float64(r.Ball.Pos.Z-target.Z)
							score := dx*dx + dz*dz
							if r.Ball.Holed {
								score = -1
							}
							if score < best {
								best = score
								chosen = shot
								next = r
							}
						}
					}
				}
				if chosen == 0 {
					t.Errorf("%s no safe shot towards %+v from %+v", h.ID, target, ball.Pos)
					break
				}
				shots = append(shots, chosen)
				ball = next.Ball
				t.Logf("%s shot %d: yaw=%d power=%d phase=%d -> %.2f,%.2f,%.2f target %.1f,%.1f", h.ID, len(shots), (chosen>>10)&4095, chosen&1023, Phase(chosen), float64(ball.Pos.X)/10000, float64(ball.Pos.Y)/10000, float64(ball.Pos.Z)/10000, float64(target.X)/10000, float64(target.Z)/10000)
				if target != h.Cup && best < 4*float64(course.Unit*course.Unit) {
					break
				}
			}
		}
		t.Logf("ROUTE %s holed=%v strokes=%d: %#v", h.ID, ball.Holed, ball.Strokes, shots)
		if !ball.Holed || ball.Strokes > 12 {
			t.Errorf("%s not playable within cap", h.ID)
		}
	}
}
