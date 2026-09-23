// Package fixed implements deterministic Q16.16 arithmetic. Overflow in an
// operation's result is a programming error; callers must keep world bounds
// within the representable range. Products and quotients use wide intermediates.
package fixed

type F int32
type Angle uint16

const (
	Shift   = 16
	One   F = 1 << Shift
	Half  F = One / 2
)

func FromInt(i int) F {
	if i < -32768 || i > 32767 {
		panic("fixed: integer out of range")
	}
	return F(i) << Shift
}

func Ratio(n, d int32) F { return checked((int64(n) << Shift) / int64(d)) }
func (f F) Int() int     { return int(f >> Shift) }
func (f F) Round() int   { return int((int64(f) + int64(Half)) >> Shift) }
func Mul(a, b F) F       { return checked((int64(a) * int64(b)) >> Shift) }
func Div(a, b F) F       { return checked((int64(a) << Shift) / int64(b)) }
func checked(v int64) F {
	if v < -2147483648 || v > 2147483647 {
		panic("fixed: overflow")
	}
	return F(v)
}
func Abs(a F) F {
	if a < 0 {
		return checked(-int64(a))
	}
	return a
}
func Min(a, b F) F {
	if a < b {
		return a
	}
	return b
}
func Max(a, b F) F {
	if a > b {
		return a
	}
	return b
}
func Clamp(v, lo, hi F) F { return Min(Max(v, lo), hi) }

// Sqrt returns floor(sqrt(n)), using integer arithmetic over the whole uint64
// domain. It is also used to normalize fixed-point vectors without overflow.
func Sqrt(n uint64) uint64 {
	var result uint64
	bit := uint64(1) << 62
	for bit > n {
		bit >>= 2
	}
	for bit != 0 {
		if n >= result+bit {
			n -= result + bit
			result = (result >> 1) + bit
		} else {
			result >>= 1
		}
		bit >>= 2
	}
	return result
}

type Vec struct{ X, Y F }

func (v Vec) Add(w Vec) Vec {
	return Vec{checked(int64(v.X) + int64(w.X)), checked(int64(v.Y) + int64(w.Y))}
}
func (v Vec) Sub(w Vec) Vec {
	return Vec{checked(int64(v.X) - int64(w.X)), checked(int64(v.Y) - int64(w.Y))}
}
func (v Vec) Scale(f F) Vec { return Vec{Mul(v.X, f), Mul(v.Y, f)} }
func (v Vec) Len() F {
	x, y := int64(v.X), int64(v.Y)
	return checked(int64(Sqrt(uint64(x*x) + uint64(y*y))))
}
func (v Vec) Unit() Vec {
	n := v.Len()
	if n == 0 {
		return Vec{}
	}
	return Vec{Div(v.X, n), Div(v.Y, n)}
}
func Dot(a, b Vec) F { return checked((int64(a.X)*int64(b.X) + int64(a.Y)*int64(b.Y)) >> Shift) }

func Sin(a Angle) F {
	q := uint16(a) >> 14
	o := uint16(a) & 16383
	if q&1 != 0 {
		o = 16384 - o
	}
	i, fraction := int(o>>4), int64(o&15)
	v := sineQuarter[i]
	if fraction != 0 {
		v += F((int64(sineQuarter[i+1]-v) * fraction) / 16)
	}
	if q >= 2 {
		return -v
	}
	return v
}
func Cos(a Angle) F { return Sin(a + 16384) }

// Atan2 uses an integer search against the committed sine table. Positive y
// points down in screen coordinates. (0,0) is defined as angle zero.
func Atan2(y, x F) Angle {
	if x == 0 && y == 0 {
		return 0
	}
	xx, yy := int64(x), int64(y)
	if xx < 0 {
		xx = -xx
	}
	if yy < 0 {
		yy = -yy
	}
	lo, hi := uint16(0), uint16(16384)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if yy*int64(Cos(Angle(mid))) > xx*int64(Sin(Angle(mid))) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	a := Angle(lo)
	if x < 0 {
		a = 32768 - a
	}
	if y < 0 {
		a = -a
	}
	return a
}
