package manifest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/decred/dcrd/crypto/blake256"
)

const Version uint16 = 1
const OpenerAlternating = "alternating-holes/v1"
const PayoutWinnerTakesPot = "match-play-winner/v1"

type Manifest struct {
	Version                                 uint16
	GameVersion, SimulationVersion          int32
	CourseHash                              string
	Holes, StrokeCap                        uint8
	MoveDeadlineBlocks, MatchDeadlineBlocks uint32
	OpenerRule, PayoutRule                  string
}

func (m Manifest) Validate() error {
	if _, err := hex.DecodeString(m.CourseHash); err != nil {
		return fmt.Errorf("invalid course hash")
	}
	if m.Version != Version || m.GameVersion != 1 || m.SimulationVersion != 1 || len(m.CourseHash) != 64 || m.Holes != 9 || m.StrokeCap != 12 || m.MoveDeadlineBlocks == 0 || m.MatchDeadlineBlocks <= m.MoveDeadlineBlocks || m.OpenerRule != OpenerAlternating || m.PayoutRule != PayoutWinnerTakesPot {
		return fmt.Errorf("unsupported ORBIT GOLF rules")
	}
	return nil
}
func (m Manifest) Hash() ([32]byte, error) {
	if err := m.Validate(); err != nil {
		return [32]byte{}, err
	}
	b, _ := json.Marshal(m)
	return blake256.Sum256(append([]byte("dcrminigolf/manifest/v1:"), b...)), nil
}
func (m Manifest) Same(other Manifest) bool { return m == other }
