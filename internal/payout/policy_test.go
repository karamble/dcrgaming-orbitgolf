package payout

import "testing"

func TestAFrozenPolicyDividesExactlyWhatTheTableHolds(t *testing.T) {
	p, err := Freeze([]int64{100_000, 100_000})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if p.Pot() != 200_000 {
		t.Fatalf("pot is %d", p.Pot())
	}
	shares, err := p.Winner(1)
	if err != nil {
		t.Fatalf("winner: %v", err)
	}
	var total int64
	for _, a := range shares {
		total += a
	}
	if total != p.Pot() {
		t.Fatalf("the shares total %d and the table holds %d", total, p.Pot())
	}
	if shares[1] != p.Pot() || shares[0] != 0 {
		t.Fatalf("shares are %v", shares)
	}
	// Every seat is named, including the one paid nothing.
	if _, ok := shares[0]; !ok {
		t.Fatal("a seat paid nothing was left out of the allocation")
	}
}

// The observed stakes, not the buy-in: a seat may pay more than it owed.
func TestAnOverpaidSeatIsStillDividedExactly(t *testing.T) {
	p, err := Freeze([]int64{100_000, 150_000})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	shares, err := p.Winner(0)
	if err != nil {
		t.Fatalf("winner: %v", err)
	}
	if shares[0] != 250_000 {
		t.Fatalf("the winner takes %d of a %d pot", shares[0], p.Pot())
	}
}

func TestAPolicyRefusesWhatCannotBePaid(t *testing.T) {
	for name, stakes := range map[string][]int64{
		"one seat":     {100_000},
		"a zero stake": {100_000, 0},
		"a negative":   {100_000, -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Freeze(stakes); err == nil {
				t.Fatalf("%s was frozen", name)
			}
		})
	}
	p, _ := Freeze([]int64{100_000, 100_000})
	if _, err := p.Winner(5); err == nil {
		t.Fatal("a seat that is not at the table was paid")
	}
}

// The whole point: the numbers must not move between proposals.
func TestAChangedTableIsNotTheSamePolicy(t *testing.T) {
	p, _ := Freeze([]int64{100_000, 100_000})
	if !p.Same([]int64{100_000, 100_000}) {
		t.Fatal("an unchanged table did not match its own policy")
	}
	for _, other := range [][]int64{
		{100_000, 100_001},
		{100_000},
		{100_000, 100_000, 100_000},
	} {
		if p.Same(other) {
			t.Fatalf("%v matched a policy of %v", other, p.Stakes)
		}
	}
	a, _ := p.ID()
	q, _ := Freeze([]int64{100_000, 100_001})
	b, _ := q.ID()
	if a == b {
		t.Fatal("two different allocations share an identifier")
	}
}
