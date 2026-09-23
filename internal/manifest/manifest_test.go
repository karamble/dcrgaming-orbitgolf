package manifest_test

import (
	"github.com/karamble/dcrgaming-orbitgolf/internal/session"
	"testing"
)

func TestExpeditionRejectsOldSimulationRules(t *testing.T) {
	current := session.Ours()
	if err := current.Validate(); err != nil {
		t.Fatal(err)
	}
	if current.SimulationVersion != 2 {
		t.Fatal("expedition rules need simulation v2")
	}
	legacy := current
	legacy.SimulationVersion = 1
	if legacy.Validate() == nil {
		t.Fatal("legacy physics accepted")
	}
	if _, err := legacy.Hash(); err == nil {
		t.Fatal("legacy rules could be signed")
	}
}
