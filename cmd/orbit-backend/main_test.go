package main

import (
	"encoding/json"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/sim"
	"reflect"
	"testing"
)

func TestPreviewIsReadOnlyAndMatchesShot(t *testing.T) {
	a := &app{dir: t.TempDir(), practice: -1, jobs: make(chan error, 1)}
	defer a.close()
	if _, err := a.call(request{Method: "preview", Power: 500}); err == nil {
		t.Fatal("preview outside game")
	}
	for _, mode := range []string{"practice", "local_match"} {
		for hole := 0; hole < 9; hole++ {
			if _, err := a.call(request{Method: mode, Hole: hole}); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(a.state())
			q := request{Method: "preview", Yaw: 3072, Power: 600, Phase: uint32(hole * 12)}
			first, err := a.call(q)
			if err != nil {
				t.Fatal(err)
			}
			second, err := a.call(q)
			if err != nil || !reflect.DeepEqual(first, second) {
				t.Fatal("preview is not repeatable", err)
			}
			after, _ := json.Marshal(a.state())
			if string(before) != string(after) {
				t.Fatal("preview changed game state")
			}
			q.Method = "shot"
			actual, err := a.call(q)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(first.(map[string]any)["preview"], actual.(map[string]any)["result"]) {
				t.Fatal("preview differs from shot")
			}
		}
	}
	for _, q := range []request{{Method: "preview", Power: 0}, {Method: "preview", Power: 1001}, {Method: "preview", Power: 500, Yaw: 4096}, {Method: "preview", Power: 500, Phase: 720}, {Method: "preview", Power: 500, Phase: 13}, {Method: "shot", Power: 500, Phase: 720}, {Method: "shot", Power: 500, Phase: 13}} {
		if _, err := a.call(q); err == nil {
			t.Fatal("invalid preview accepted")
		}
	}
}

func TestLocalIPC(t *testing.T) {
	a := &app{dir: t.TempDir(), practice: -1, jobs: make(chan error, 1)}
	defer a.close()
	if _, err := a.call(request{Method: "shot", Power: 100}); err == nil {
		t.Fatal("shot without match")
	}
	if _, err := a.call(request{Method: "practice", Hole: 9}); err == nil {
		t.Fatal("invalid hole")
	}
	for hole := 0; hole < 9; hole++ {
		if _, err := a.call(request{Method: "practice", Hole: hole}); err != nil {
			t.Fatal(err)
		}
		result, err := a.call(request{Method: "shot", Yaw: 3072, Power: 500})
		if err != nil {
			t.Fatal(err)
		}
		r := result.(map[string]any)["result"].(sim.Result)
		if r.Ball.Strokes < 1 || len(r.Path) < 2 {
			t.Fatal("missing shot result")
		}
	}
	if _, err := a.call(request{Method: "local_match"}); err != nil {
		t.Fatal(err)
	}
	if a.state()["mode"] != "local" {
		t.Fatal("local match mode")
	}
	if _, err := a.call(request{Method: "fund"}); err == nil {
		t.Fatal("funding without bridge")
	}
	if _, err := a.call(request{Method: "connect"}); err == nil {
		t.Fatal("default mainnet must refuse")
	}
}
