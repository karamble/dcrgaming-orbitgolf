// Package course is the canonical collision geometry. Godot builds playable
// surfaces from these same integers; decorative meshes never define the rules.
package course

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const Unit int64 = 10000

// Every obstacle period divides this cycle. A shot signs its chosen launch
// phase, never a wall-clock timestamp. The simulation then advances at 120 Hz.
const Cycle = 720
const PhaseStep = 12

type Vec struct{ X, Y, Z int64 }
type Platform struct {
	X, Z, W, D, Y int64
	// Rise is the height change from the negative-Z to positive-Z edge.
	Rise int64
}
type Wall struct {
	X, Y, Z, W, H, D int64
	Ceiling          bool
}
type Bumper struct{ X, Y, Z, Radius int64 }
type Field struct{ X, Z, Radius, Force int64 }
type Gate struct {
	X, Y, Z, W, H, D     int64
	Period, Open, Offset int
	Laser                bool
}
type Hole struct {
	ID, Name, World, Hint string
	Par                   int
	Tee, Cup              Vec
	Gravity               int64
	Platforms             []Platform
	Walls                 []Wall
	Bumpers               []Bumper
	Fields                []Field
	Gates                 []Gate
}

func V(x, y, z int64) Vec { return Vec{x * Unit, y * Unit, z * Unit} }
func platform(x, z, w, d, y, rise int64) Platform {
	return Platform{x * Unit, z * Unit, w * Unit, d * Unit, y * Unit, rise * Unit}
}
func wall(x, y, z, w, h, d int64) Wall {
	return Wall{X: x * Unit, Y: y * Unit, Z: z * Unit, W: w * Unit, H: h * Unit, D: d * Unit}
}
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// GateWall is also mirrored by the cosmetic client animation. Laser walls
// block/bounce, never destroy a ball. Sliding doors lift during the open window.
func GateWall(g Gate, tick int) (Wall, bool) {
	phase := (tick + g.Offset) % g.Period
	w := Wall{X: g.X, Y: g.Y, Z: g.Z, W: g.W, H: g.H, D: g.D}
	if g.Laser {
		return w, phase >= g.Open
	}
	if phase < g.Open {
		lift := min(24, min(phase, g.Open-phase))
		w.Y += int64(lift) * (g.H + Unit) / 24
	}
	return w, true
}

// A stage runs towards -Z. Heights match at joins; gaps omit the floor/rails.
type stage struct {
	length, width, endHeight int64
	gap                      bool
}

func route(id, name, world, hint string, par int, stages []stage) Hole {
	h := Hole{ID: id, Name: name, World: world, Hint: hint, Par: par, Gravity: 7}
	z, y := int64(56), int64(0)
	h.Tee = V(0, 0, z-4)
	for index, s := range stages {
		if !s.gap {
			p := platform(0, z-s.length/2, s.width, s.length, s.endHeight, y-s.endHeight)
			h.Platforms = append(h.Platforms, p)
			pieces := int64(1)
			if y != s.endHeight {
				pieces = s.length / 2
			}
			for j := int64(0); j < pieces; j++ {
				a, b := p.Y+p.Rise*j/pieces, p.Y+p.Rise*(j+1)/pieces
				for _, side := range []int64{-1, 1} {
					h.Walls = append(h.Walls, Wall{X: side * p.W / 2, Y: min(a, b), Z: p.Z - p.D/2 + (2*j+1)*p.D/(2*pieces), H: Unit + abs(a-b), D: p.D / pieces})
				}
			}
			if index == 0 {
				h.Walls = append(h.Walls, wall(0, y, z, s.width, 1, 0))
			}
			if index == len(stages)-1 {
				h.Walls = append(h.Walls, wall(0, s.endHeight, z-s.length, s.width, 1, 0))
				h.Cup = V(0, s.endHeight, z-s.length+4)
			}
		}
		z -= s.length
		y = s.endHeight
	}
	return h
}

// Staggered barriers leave a four-metre passage on alternating sides.
func gate(h *Hole, z, y, width, side int64) {
	h.Walls = append(h.Walls, wall(-side*2, y, z, width-4, 2, 1))
}
func tunnel(h *Hole, z, y, width, depth int64) {
	for _, side := range []int64{-1, 1} {
		h.Walls = append(h.Walls, wall(side*(width/2), y, z, 1, 3, depth))
	}
	roof := wall(0, y+3, z, width+1, 1, depth)
	roof.Ceiling = true
	h.Walls = append(h.Walls, roof)
}
func timed(h *Hole, z, y, width int64, laser bool, offset int) {
	h.Gates = append(h.Gates, Gate{Y: y * Unit, Z: z * Unit, W: width * Unit, H: 3 * Unit, D: Unit / 3, Period: 360, Open: 144, Offset: offset, Laser: laser})
}

// All returns fresh immutable-by-convention course values in tournament order.
func All() []Hole {
	a := route("shipyard-01", "First Contact", "SHIPYARD", "Climb the dock, take the right passage, then descend through the service tunnel.", 5, []stage{
		{16, 14, 0, false}, {12, 14, 3, false}, {20, 14, 3, false}, {12, 14, 0, false}, {16, 8, 0, false}, {12, 14, 2, false}, {20, 14, 2, false}, {12, 14, 0, false}, {16, 14, 0, false}})
	gate(&a, 18, 3, 14, 1)
	gate(&a, -40, 2, 14, -1)
	tunnel(&a, -12, 0, 8, 12)
	b := route("shipyard-02", "Cargo Run", "SHIPYARD", "Zigzag through freight decks. Time the loading door before the downhill green.", 6, []stage{
		{24, 18, 0, false}, {12, 14, 4, false}, {24, 18, 4, false}, {12, 14, 1, false}, {20, 8, 1, false}, {12, 14, 3, false}, {24, 18, 3, false}, {12, 14, 0, false}, {16, 14, 0, false}})
	gate(&b, 44, 0, 18, 1)
	gate(&b, 10, 4, 18, -1)
	gate(&b, -56, 3, 18, 1)
	tunnel(&b, -26, 1, 8, 14)
	timed(&b, -24, 1, 8, false, 0)
	c := route("shipyard-03", "Launch Bay", "SHIPYARD", "Stage at the launch lip, clear the docking gap, then climb to the observation green.", 6, []stage{
		{16, 14, 0, false}, {12, 10, 3, false}, {6, 10, 0, true}, {24, 18, 0, false}, {12, 14, 3, false}, {20, 8, 3, false}, {12, 14, 0, false}, {24, 18, 0, false}, {12, 14, 2, false}, {16, 14, 2, false}})
	gate(&c, 6, 0, 18, 1)
	tunnel(&c, -24, 3, 8, 14)
	gate(&c, -56, 0, 18, -1)
	d := route("reactor-01", "Containment", "REACTOR", "Bank around the cores; crest the cooling tower and time the containment laser.", 6, []stage{
		{24, 18, 0, false}, {12, 14, 4, false}, {24, 18, 4, false}, {16, 14, 0, false}, {20, 8, 0, false}, {12, 14, 3, false}, {24, 18, 3, false}, {12, 14, 0, false}, {16, 14, 0, false}})
	gate(&d, 8, 4, 18, 1)
	gate(&d, -64, 3, 18, -1)
	tunnel(&d, -30, 0, 8, 14)
	timed(&d, -30, 0, 8, true, 96)
	d.Bumpers = []Bumper{{0, 0, 44 * Unit, Unit}, {-3 * Unit, 4 * Unit, 12 * Unit, Unit}, {3 * Unit, 3 * Unit, -64 * Unit, Unit}}
	e := route("reactor-02", "Flux Channel", "REACTOR", "Magnetic bends feed a raised causeway. Read the laser cycle before the covered channel.", 6, []stage{
		{24, 18, 0, false}, {12, 12, 3, false}, {20, 14, 3, false}, {12, 12, 0, false}, {24, 18, 0, false}, {12, 10, 2, false}, {20, 8, 2, false}, {12, 12, 0, false}, {20, 16, 0, false}})
	e.Fields = []Field{{-3 * Unit, 42 * Unit, 6 * Unit, 3}, {3 * Unit, -24 * Unit, 6 * Unit, 3}}
	gate(&e, 8, 3, 14, -1)
	tunnel(&e, -58, 2, 8, 14)
	gate(&e, -86, 0, 16, 1)
	timed(&e, -58, 2, 8, true, 192)
	f := route("reactor-03", "Critical Angle", "REACTOR", "Four staggered gates, two ridgelines. Plan the laser crossing from the overview.", 7, []stage{
		{28, 20, 0, false}, {12, 14, 3, false}, {28, 20, 3, false}, {12, 14, 0, false}, {20, 8, 0, false}, {12, 14, 4, false}, {28, 20, 4, false}, {12, 14, 0, false}, {20, 16, 0, false}})
	gate(&f, 42, 0, 20, 1)
	gate(&f, 2, 3, 20, -1)
	gate(&f, -68, 4, 20, 1)
	gate(&f, -106, 0, 16, -1)
	tunnel(&f, -34, 0, 8, 14)
	timed(&f, -34, 0, 8, true, 0)
	g := route("moon-01", "Low Orbit", "SHATTERED MOON", "Two low-gravity launches. Set up each jump; braking on the far terrace matters.", 6, []stage{
		{16, 14, 0, false}, {12, 10, 3, false}, {8, 10, 0, true}, {24, 18, 0, false}, {12, 12, 3, false}, {20, 14, 3, false}, {12, 10, 5, false}, {8, 10, 0, true}, {24, 18, 0, false}, {12, 14, 2, false}, {16, 14, 2, false}})
	g.Gravity = 3
	gate(&g, 4, 0, 18, 1)
	gate(&g, -24, 3, 14, -1)
	gate(&g, -72, 0, 18, 1)
	gate(&g, -98, 2, 14, -1)
	h := route("moon-02", "Broken Causeway", "SHATTERED MOON", "Narrow rising fragments and exposed edges. Trade distance for a safe landing.", 6, []stage{
		{20, 14, 0, false}, {16, 6, 3, false}, {20, 14, 3, false}, {16, 6, 0, false}, {20, 14, 0, false}, {16, 6, 4, false}, {20, 14, 4, false}, {16, 6, 1, false}, {20, 14, 1, false}})
	h.Gravity = 3
	gate(&h, 10, 3, 14, 1)
	gate(&h, -24, 0, 14, -1)
	gate(&h, -60, 4, 14, 1)
	i := route("moon-03", "Event Horizon", "SHATTERED MOON", "An orbital finale: gravity wells, a ridge jump and a timed airlock before the final descent.", 7, []stage{
		{24, 18, 0, false}, {12, 12, 3, false}, {24, 18, 3, false}, {12, 10, 5, false}, {8, 10, 0, true}, {24, 18, 0, false}, {12, 12, 3, false}, {20, 8, 3, false}, {12, 12, 0, false}, {24, 18, 0, false}})
	i.Gravity = 3
	i.Fields = []Field{{-3 * Unit, 42 * Unit, 6 * Unit, 3}, {3 * Unit, -32 * Unit, 6 * Unit, 3}}
	gate(&i, 8, 3, 18, 1)
	gate(&i, -36, 0, 18, -1)
	tunnel(&i, -70, 3, 8, 14)
	gate(&i, -100, 0, 18, 1)
	timed(&i, -70, 3, 8, false, 180)
	return []Hole{a, b, c, d, e, f, g, h, i}
}

func Hash() string {
	b, _ := json.Marshal(All())
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
