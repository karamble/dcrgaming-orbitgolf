// Package sim implements the authoritative bounded integer 3D ball simulation.
// Coordinates are 1/10000 metre, velocity is units per 120 Hz tick. No floats,
// wall clock, GPU physics, random sources or map iteration affect an outcome.
package sim

import (
	"fmt"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/fixed"
)

const Version = 1
const Radius int64 = 1800
const MaxTicks = 3600
const Hz = 120

type Ball struct {
	Pos     course.Vec
	Holed   bool
	Strokes int
}
type Result struct {
	Ball    Ball
	Path    []course.Vec
	Penalty bool
	Ticks   int
	Bounces int
}

// Shot packs a 12-bit yaw (0 is +X, 1024 is +Z) and 10-bit power (1..1000).
func Shot(yaw, power uint32) uint32 { return yaw<<10 | power }
func Validate(shot uint32) error {
	if shot>>10 >= 4096 || shot&1023 < 1 || shot&1023 > 1000 {
		return fmt.Errorf("invalid aim or power")
	}
	return nil
}
func Tee(h course.Hole) Ball { p := h.Tee; p.Y = ground(h, p.X, p.Z) + Radius; return Ball{Pos: p} }
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
func length(x, z int64) int64 { return int64(fixed.Sqrt(uint64(x*x + z*z))) }
func ground(h course.Hole, x, z int64) int64 {
	best := int64(-100 * course.Unit)
	for _, p := range h.Platforms {
		if abs(x-p.X) <= p.W/2 && abs(z-p.Z) <= p.D/2 {
			y := p.Y + (z-(p.Z-p.D/2))*p.Rise/p.D
			if y > best {
				best = y
			}
		}
	}
	return best
}

func Simulate(h course.Hole, ball Ball, shot uint32) (Result, error) {
	if err := Validate(shot); err != nil {
		return Result{}, err
	}
	if ball.Holed {
		return Result{}, fmt.Errorf("ball already holed")
	}
	p := ball.Pos
	start := p
	angle := fixed.Angle((shot >> 10) * 16)
	speed := int64(shot&1023)*2 + 80
	v := course.Vec{X: int64(fixed.Cos(angle)) * speed / 65536, Z: int64(fixed.Sin(angle)) * speed / 65536}
	r := Result{Ball: ball, Path: []course.Vec{p}}
	r.Ball.Strokes++
	rest := 0
	grounded := true
	for tick := 0; tick < MaxTicks; tick++ {
		r.Ticks = tick + 1
		if grounded {
			for _, s := range h.Platforms {
				if abs(p.X-s.X) <= s.W/2 && abs(p.Z-s.Z) <= s.D/2 && s.Rise != 0 {
					v.Z -= h.Gravity * s.Rise / s.D
				}
			}
			n := length(v.X, v.Z)
			if n > 0 {
				friction := int64(4)
				if n <= friction {
					v.X = 0
					v.Z = 0
				} else {
					v.X = v.X * (n - friction) / n
					v.Z = v.Z * (n - friction) / n
				}
			}
		}
		for _, f := range h.Fields {
			dx, dz := f.X-p.X, f.Z-p.Z
			n := length(dx, dz)
			if n > Radius && n < f.Radius {
				v.X += dx * f.Force / n
				v.Z += dz * f.Force / n
			}
		}
		v.Y -= h.Gravity
		// Bound velocity and subdivide all movement to less than half a radius.
		v.X = max(-3000, min(3000, v.X))
		v.Z = max(-3000, min(3000, v.Z))
		v.Y = max(-3000, min(3000, v.Y))
		steps := max(int64(1), (max(abs(v.X), max(abs(v.Y), abs(v.Z)))+Radius/2-1)/(Radius/2))
		grounded = false
		for sub := int64(0); sub < steps; sub++ {
			old := p
			p.X += v.X / steps
			p.Z += v.Z / steps
			p.Y += v.Y / steps
			for _, w := range h.Walls {
				if p.Y-Radius >= w.Y+w.H || p.Y+Radius <= w.Y {
					continue
				}
				if abs(p.X-w.X) < w.W/2+Radius && abs(p.Z-w.Z) < w.D/2+Radius {
					if abs(old.X-w.X) >= w.W/2+Radius {
						p.X = old.X
						v.X = -v.X * 82 / 100
					} else {
						p.Z = old.Z
						v.Z = -v.Z * 82 / 100
					}
					r.Bounces++
				}
			}
			for _, b := range h.Bumpers {
				if p.Y > course.Unit+Radius {
					continue
				}
				dx, dz := p.X-b.X, p.Z-b.Z
				n := length(dx, dz)
				if n > 0 && n < b.Radius+Radius {
					dot := v.X*dx + v.Z*dz
					if dot < 0 {
						v.X -= 2 * dot * dx / (n * n)
						v.Z -= 2 * dot * dz / (n * n)
						r.Bounces++
					}
					p.X = b.X + dx*(b.Radius+Radius)/n
					p.Z = b.Z + dz*(b.Radius+Radius)/n
				}
			}
			floor := ground(h, p.X, p.Z)
			if floor > -50*course.Unit && p.Y <= floor+Radius && old.Y >= floor-Radius {
				p.Y = floor + Radius
				v.Y = 0
				grounded = true
				// A ramp's normal gives deterministic lift when its edge is crossed.
				for _, s := range h.Platforms {
					if s.Rise != 0 && abs(p.X-s.X) <= s.W/2 && abs(p.Z-s.Z) <= s.D/2 {
						v.Y = v.Z * s.Rise / s.D
					}
				}
			}
		}
		if tick%2 == 0 {
			r.Path = append(r.Path, p)
		}
		if p.Y < -5*course.Unit || abs(p.X) > 40*course.Unit || abs(p.Z) > 50*course.Unit {
			r.Penalty = true
			break
		}
		cupY := ground(h, h.Cup.X, h.Cup.Z) + Radius
		if length(p.X-h.Cup.X, p.Z-h.Cup.Z) < 3200 && abs(p.Y-cupY) < 2500 && length(v.X, v.Z) < 520 {
			r.Ball.Holed = true
			p = course.Vec{X: h.Cup.X, Y: cupY - 1200, Z: h.Cup.Z}
			break
		}
		if grounded && length(v.X, v.Z) < 10 && abs(v.Y) < 10 {
			rest++
		} else {
			rest = 0
		}
		if rest >= 30 {
			break
		}
		if tick == MaxTicks-1 {
			r.Penalty = true
		}
	}
	if r.Penalty {
		p = start
		r.Ball.Strokes++
	}
	r.Ball.Pos = p
	r.Path = append(r.Path, p)
	return r, nil
}
