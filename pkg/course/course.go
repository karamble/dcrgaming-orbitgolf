// Package course is the canonical collision geometry. Godot builds playable
// surfaces from these same integers; decorative meshes never define the rules.
package course

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const Unit int64 = 10000

type Vec struct{ X, Y, Z int64 }
type Platform struct {
	X, Z, W, D, Y int64
	// Rise is the height change from the negative-Z to positive-Z edge.
	Rise int64
}
type Wall struct{ X, Y, Z, W, H, D int64 }
type Bumper struct{ X, Z, Radius int64 }
type Field struct{ X, Z, Radius, Force int64 }
type Hole struct {
	ID, Name, World, Hint string
	Par                   int
	Tee, Cup              Vec
	Gravity               int64
	Platforms             []Platform
	Walls                 []Wall
	Bumpers               []Bumper
	Fields                []Field
}

func V(x, y, z int64) Vec { return Vec{x * Unit, y * Unit, z * Unit} }
func platform(x, z, w, d, y, rise int64) Platform {
	return Platform{x * Unit, z * Unit, w * Unit, d * Unit, y * Unit, rise * Unit}
}
func wall(x, z, w, d int64) Wall { return Wall{x * Unit, 0, z * Unit, w * Unit, Unit, d * Unit} }
func bumper(x, z int64) Bumper   { return Bumper{x * Unit, z * Unit, Unit / 2} }

// All returns fresh immutable-by-convention course values in tournament order.
func All() []Hole {
	base := func(id, name, world, hint string, par int, tee, cup Vec) Hole {
		return Hole{ID: id, Name: name, World: world, Hint: hint, Par: par, Tee: tee, Cup: cup, Gravity: 7,
			Platforms: []Platform{platform(0, 0, 12, 26, 0, 0)},
			Walls:     []Wall{wall(-6, 0, 0, 26), wall(6, 0, 0, 26), wall(0, -13, 12, 0), wall(0, 13, 12, 0)}}
	}
	a := base("shipyard-01", "First Contact", "SHIPYARD", "Find the bank. Let the rails do the work.", 3, V(-3, 0, 10), V(3, 0, -10))
	a.Walls = append(a.Walls, wall(-2, 0, 8, 1))
	b := base("shipyard-02", "Cargo Run", "SHIPYARD", "Thread the cargo lanes. Power is not always speed.", 4, V(-4, 0, 10), V(4, 0, -10))
	b.Walls = append(b.Walls, wall(-2, 4, 8, 1), wall(2, -4, 8, 1))
	c := base("shipyard-03", "Launch Bay", "SHIPYARD", "Ride the launch ramp across the open docking gap.", 4, V(0, 0, 10), V(0, 0, -10))
	c.Platforms = []Platform{platform(0, 8, 12, 10, 0, 0), platform(0, 1, 5, 4, 2, -2), platform(0, -9, 12, 8, 0, 0)}
	c.Walls = []Wall{wall(-6, 8, 0, 10), wall(6, 8, 0, 10), wall(0, 13, 12, 0), wall(-6, -9, 0, 8), wall(6, -9, 0, 8), wall(0, -13, 12, 0)}
	d := base("reactor-01", "Containment", "REACTOR", "Bank around the core. Bumpers return your momentum.", 3, V(-4, 0, 10), V(4, 0, -10))
	d.Bumpers = []Bumper{bumper(0, 0), bumper(-3, -4), bumper(3, 4)}
	e := base("reactor-02", "Flux Channel", "REACTOR", "The magnetic field bends your path. Read its reach.", 4, V(0, 0, 10), V(0, 0, -10))
	e.Fields = []Field{{-2 * Unit, 0, 5 * Unit, 5}}
	e.Walls = append(e.Walls, wall(2, 0, 4, 1))
	f := base("reactor-03", "Critical Angle", "REACTOR", "Two banks, one narrow approach. Choose your line.", 4, V(-4, 0, 10), V(4, 0, -10))
	f.Walls = append(f.Walls, wall(-2, 5, 8, 1), wall(2, -2, 8, 1))
	f.Bumpers = []Bumper{bumper(0, -7)}
	g := base("moon-01", "Low Orbit", "SHATTERED MOON", "Less gravity. Longer flights. Commit to the landing.", 3, V(0, 0, 10), V(0, 0, -10))
	g.Gravity = 3
	g.Platforms = []Platform{platform(0, 8, 12, 10, 0, 0), platform(0, 1, 5, 4, 2, -2), platform(0, -9, 12, 8, 0, 0)}
	g.Walls = c.Walls
	h := base("moon-02", "Broken Causeway", "SHATTERED MOON", "Keep to the connected fragments, or risk the void.", 4, V(-3, 0, 10), V(3, 0, -10))
	h.Gravity = 3
	h.Platforms = []Platform{platform(-3, 7, 6, 12, 0, 0), platform(0, 0, 12, 4, 0, 0), platform(3, -7, 6, 12, 0, 0)}
	h.Walls = nil
	i := base("moon-03", "Event Horizon", "SHATTERED MOON", "The final field pulls inward. Aim beyond the obvious.", 5, V(-4, 0, 10), V(4, 0, -10))
	i.Gravity = 3
	i.Fields = []Field{{0, 0, 7 * Unit, 6}}
	i.Bumpers = []Bumper{bumper(0, 0), bumper(-3, -5), bumper(3, 5)}
	return []Hole{a, b, c, d, e, f, g, h, i}
}

func Hash() string {
	b, _ := json.Marshal(All())
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
