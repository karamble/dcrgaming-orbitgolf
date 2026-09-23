package match

import (
	"fmt"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/sim"
)

const Holes = 9
const Seats = 2
const StrokeCap = 12

type Move struct {
	Hole uint8
	Seat uint32
	Shot uint32
}
type Result struct {
	Winner  uint32
	Decided bool
	Moves   int
	Strokes [2]int
}
type HoleState struct {
	Balls [2]sim.Ball
	Shots int
}

func (h *HoleState) Moves() int { return h.Shots }

type Match struct {
	opener  uint32
	index   int
	turn    uint32
	state   HoleState
	results []Result
	done    bool
	last    sim.Result
}

func New(opener uint32) (*Match, error) {
	if opener >= 2 {
		return nil, fmt.Errorf("invalid opener")
	}
	m := &Match{opener: opener}
	m.reset()
	return m, nil
}
func Other(s uint32) uint32 { return 1 - s }
func (m *Match) reset() {
	b := sim.Tee(course.All()[m.index])
	m.state = HoleState{Balls: [2]sim.Ball{b, b}}
	m.turn = (m.opener + uint32(m.index)) % 2
}
func (m *Match) Index() int        { return m.index }
func (m *Match) Turn() uint32      { return m.turn }
func (m *Match) Hole() *HoleState  { return &m.state }
func (m *Match) Done() bool        { return m.done }
func (m *Match) Results() []Result { return append([]Result(nil), m.results...) }
func (m *Match) Last() sim.Result {
	r := m.last
	r.Path = append([]course.Vec(nil), r.Path...)
	return r
}
func (m *Match) Score() [2]int {
	var s [2]int
	for _, r := range m.results {
		if r.Decided {
			s[r.Winner]++
		}
	}
	return s
}
func (m *Match) Outcome() (uint32, bool, bool) {
	if !m.done {
		return 0, false, false
	}
	s := m.Score()
	if s[0] == s[1] {
		return 0, false, true
	}
	if s[0] > s[1] {
		return 0, true, true
	}
	return 1, true, true
}
func (m *Match) CanPlay(v Move) error {
	if m.done {
		return fmt.Errorf("match finished")
	}
	if int(v.Hole) != m.index {
		return fmt.Errorf("wrong hole")
	}
	if v.Seat != m.turn {
		return fmt.Errorf("not your turn")
	}
	if m.state.Balls[v.Seat].Holed || m.state.Balls[v.Seat].Strokes >= StrokeCap {
		return fmt.Errorf("seat finished this hole")
	}
	return sim.Validate(v.Shot)
}
func (m *Match) Play(v Move) error {
	if err := m.CanPlay(v); err != nil {
		return err
	}
	r, err := sim.Simulate(course.All()[m.index], m.state.Balls[v.Seat], v.Shot)
	if err != nil {
		return err
	}
	m.last = r
	m.state.Balls[v.Seat] = r.Ball
	m.state.Shots++
	finished := func(s uint32) bool { b := m.state.Balls[s]; return b.Holed || b.Strokes >= StrokeCap }
	if finished(0) && finished(1) {
		result := Result{Moves: m.state.Shots}
		for s, b := range m.state.Balls {
			result.Strokes[s] = b.Strokes
			if !b.Holed {
				result.Strokes[s] = StrokeCap + 1
			}
		}
		if result.Strokes[0] != result.Strokes[1] {
			result.Decided = true
			if result.Strokes[1] < result.Strokes[0] {
				result.Winner = 1
			}
		}
		m.results = append(m.results, result)
		score := m.Score()
		remaining := Holes - len(m.results)
		if remaining == 0 || score[0] > score[1]+remaining || score[1] > score[0]+remaining {
			m.done = true
			return nil
		}
		m.index++
		m.reset()
		return nil
	}
	if !finished(Other(v.Seat)) {
		m.turn = Other(v.Seat)
	}
	return nil
}
func Replay(opener uint32, moves []Move) (*Match, error) {
	m, err := New(opener)
	if err != nil {
		return nil, err
	}
	for i, v := range moves {
		if err := m.Play(v); err != nil {
			return nil, fmt.Errorf("shot %d: %w", i, err)
		}
	}
	return m, nil
}
