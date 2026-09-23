package fixed

import "testing"

func TestGoldenArithmetic(t *testing.T) {
	if F(-1).Int() != -1 || (-Half).Int() != -1 || Half.Int() != 0 {
		t.Fatal("pixel conversion must floor")
	}
	if F(2147483647).Round() != 32768 {
		t.Fatal("round overflowed")
	}
	if Mul(Ratio(-3, 2), Ratio(5, 2)) != F(-245760) {
		t.Fatal("multiply vector")
	}
	if Div(FromInt(-7), FromInt(3)) != F(-152917) {
		t.Fatal("divide vector")
	}
	if Mul(F(-1), Half) != F(-1) {
		t.Fatal("negative multiplication must floor")
	}
	if (Vec{FromInt(3), FromInt(4)}).Len() != FromInt(5) {
		t.Fatal("integer vector length")
	}
}
func TestTrigAxes(t *testing.T) {
	for _, v := range []struct {
		a    Angle
		s, c F
	}{{0, 0, One}, {16384, One, 0}, {32768, 0, -One}, {49152, -One, 0}, {8192, 46341, 46341}} {
		if Sin(v.a) != v.s || Cos(v.a) != v.c {
			t.Fatalf("trig %d: %d %d", v.a, Sin(v.a), Cos(v.a))
		}
	}
	for _, v := range []struct {
		x, y F
		a    Angle
	}{{One, 0, 0}, {0, One, 16384}, {-One, 0, 32768}, {0, -One, 49152}, {One, One, 8192}, {-One, One, 24576}, {0, 0, 0}} {
		if got := Atan2(v.y, v.x); got != v.a {
			t.Fatalf("atan: %d want %d", got, v.a)
		}
	}
}
func TestSqrtEntireRange(t *testing.T) {
	for _, v := range []struct{ n, w uint64 }{{0, 0}, {1, 1}, {2, 1}, {8, 2}, {9, 3}, {18446744073709551615, 4294967295}} {
		if Sqrt(v.n) != v.w {
			t.Fatalf("sqrt %d", v.n)
		}
	}
}
func TestInvalidArithmeticPanics(t *testing.T) {
	for _, f := range []func(){func() { Div(One, 0) }, func() { Mul(FromInt(30000), FromInt(2)) }, func() { Abs(F(-2147483648)) }, func() { FromInt(32768) }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			f()
		}()
	}
}
