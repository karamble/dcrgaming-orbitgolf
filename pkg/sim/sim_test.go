package sim

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
)

func flat() course.Hole {
	return course.Hole{Gravity: 7, Cup: course.V(30, 0, 0), Platforms: []course.Platform{{W: 100 * course.Unit, D: 100 * course.Unit}}}
}

func TestShotValidation(t *testing.T) {
	for _, shot := range []uint32{0, Shot(0, 1001), Shot(4096, 1), ^uint32(0)} {
		if Validate(shot) == nil {
			t.Fatalf("accepted %d", shot)
		}
	}
	for _, shot := range []uint32{Shot(0, 1), Shot(4095, 1000)} {
		if err := Validate(shot); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGroundRollStopsAndPowerIncreasesDistance(t *testing.T) {
	h := flat()
	previous := int64(0)
	for _, power := range []uint32{1, 100, 300, 500} {
		r, err := Simulate(h, Tee(h), Shot(0, power))
		if err != nil || r.Penalty || r.Ball.Holed || r.Ball.Pos.Y != Radius || r.Ball.Pos.X <= previous || r.Ticks >= MaxTicks {
			t.Fatalf("power %d: %+v, %v", power, r.Ball, err)
		}
		previous = r.Ball.Pos.X
	}
}

func TestCupCapture(t *testing.T) {
	h := flat()
	h.Cup = course.V(1, 0, 0)
	r, err := Simulate(h, Tee(h), Shot(0, 110))
	if err != nil || !r.Ball.Holed || r.Ball.Strokes != 1 {
		t.Fatalf("not holed: %+v %v", r.Ball, err)
	}
	if _, err = Simulate(h, r.Ball, Shot(0, 1)); err == nil {
		t.Fatal("holed ball could play")
	}
}

func TestRailBounceAndVoidPenalty(t *testing.T) {
	h := flat()
	h.Walls = []course.Wall{{X: course.Unit, H: course.Unit, D: 10 * course.Unit}}
	r, err := Simulate(h, Tee(h), Shot(0, 300))
	if err != nil || r.Bounces == 0 || r.Penalty {
		t.Fatalf("rail not hit: %+v %v", r.Ball, err)
	}
	h.Walls = nil
	h.Platforms[0].W = course.Unit
	start := Tee(h)
	r, err = Simulate(h, start, Shot(0, 300))
	if err != nil || !r.Penalty || r.Ball.Pos != start.Pos || r.Ball.Strokes != 2 {
		t.Fatalf("bad penalty: %+v %v", r.Ball, err)
	}
}

func TestRampCanReachLanding(t *testing.T) {
	for _, gravity := range []int64{7, 3} {
		h := course.Hole{Name: "launch fixture", Gravity: gravity, Tee: course.V(0, 0, 10), Cup: course.V(0, 0, -10), Platforms: []course.Platform{
			{Z: 8 * course.Unit, W: 12 * course.Unit, D: 10 * course.Unit},
			{Z: course.Unit, W: 5 * course.Unit, D: 4 * course.Unit, Y: 2 * course.Unit, Rise: -2 * course.Unit},
			{Z: -9 * course.Unit, W: 12 * course.Unit, D: 8 * course.Unit},
		}}
		landed := false
		for p := uint32(300); p <= 1000; p += 10 {
			r, err := Simulate(h, Tee(h), Shot(3072, p))
			if err != nil {
				t.Fatal(err)
			}
			peak := int64(0)
			for _, point := range r.Path {
				peak = max(peak, point.Y)
			}
			if !r.Penalty && r.Ball.Pos.Z < -5*course.Unit && peak > 2*course.Unit {
				t.Logf("%s lands at power %d, peak %.2fm", h.Name, p, float64(peak)/float64(course.Unit))
				landed = true
				break
			}
		}
		if !landed {
			t.Errorf("no usable launch for %s", h.Name)
		}
	}
}

func TestAllCoursesDeterministicAndBounded(t *testing.T) {
	var corpus []Result
	for _, h := range course.All() {
		for yaw := uint32(0); yaw < 4096; yaw += 256 {
			for _, power := range []uint32{1, 200, 600, 1000} {
				a, err := Simulate(h, Tee(h), Shot(yaw, power))
				b, _ := Simulate(h, Tee(h), Shot(yaw, power))
				if err != nil || !reflect.DeepEqual(a, b) || a.Ticks > MaxTicks || len(a.Path) > MaxTicks/2+2 {
					t.Fatalf("non deterministic or unbounded: %s", h.ID)
				}
				corpus = append(corpus, a)
			}
		}
	}
	encoded, _ := json.Marshal(corpus)
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	// Simulation v2: expedition courses, continuous slopes, signed gate timing.
	const golden = "4b512032fef356fe013248a53b48a06ab545cf09df9a54954b9251b7a598b1cb"
	if hash != golden {
		t.Fatal("simulation changed: review and version rules before updating corpus hash", hash)
	}
	t.Log("simulation corpus SHA256:", hash)
}
