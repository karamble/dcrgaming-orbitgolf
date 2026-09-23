// orbit-backend owns gameplay and the bridge; stdout is a versioned JSON-lines
// IPC channel. Diagnostics must only go to stderr. It opens no listening port.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/karamble/dcrgaming-orbitgolf/internal/bridgeconn"
	"github.com/karamble/dcrgaming-orbitgolf/internal/match"
	"github.com/karamble/dcrgaming-orbitgolf/internal/session"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/course"
	"github.com/karamble/dcrgaming-orbitgolf/pkg/sim"
	"github.com/karamble/dcrgaming-sdk/pkg/identity"
	sdk "github.com/karamble/dcrgaming-sdk/pkg/runtime"
)

type request struct {
	ID                int    `json:"id"`
	Method            string `json:"method"`
	Hole              int    `json:"hole"`
	Yaw, Power, Phase uint32
	Table             string
	Config            *bridgeconn.Config
}
type app struct {
	dir         string
	local       *match.Match
	practice    int
	ball        sim.Ball
	game        *session.Game
	rt          *sdk.Runtime
	stop        context.CancelFunc
	done        chan struct{}
	sid         string
	status      string
	online      bool
	fundPending bool
	jobs        chan error
	runCtx      context.Context
	runErr      chan error
	fundDone    chan struct{}
}

func main() {
	dir := flag.String("appdata", "", "profile directory (no credentials on command line)")
	flag.Parse()
	if *dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		*dir = filepath.Join(base, "dcrminigolf")
	}
	a := &app{dir: *dir, practice: -1, jobs: make(chan error, 1)}
	defer a.close()
	encoder := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 256*1024)
	for scan.Scan() {
		var q request
		err := json.Unmarshal(scan.Bytes(), &q)
		var data any
		if err == nil {
			data, err = a.call(q)
		}
		out := map[string]any{"id": q.ID, "version": 1, "ok": err == nil, "data": data}
		if err != nil {
			out["error"] = err.Error()
		}
		if encoder.Encode(out) != nil {
			return
		}
		if q.Method == "quit" {
			return
		}
	}
	if err := scan.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "IPC:", err)
	}
}
func (a *app) close() {
	if a.stop != nil {
		a.stop()
		if a.fundDone != nil {
			<-a.fundDone
		}
		if a.done != nil {
			<-a.done
		}
	}
	if a.rt != nil {
		a.rt.Close()
	}
	if a.game != nil {
		a.game.Close()
	}
	a.stop = nil
	a.rt = nil
	a.game = nil
	a.online = false
	a.fundPending = false
	a.fundDone = nil
	a.sid = ""
	a.jobs = make(chan error, 1)
}
func (a *app) state() map[string]any {
	if a.runErr != nil {
		select {
		case err := <-a.runErr:
			a.online = false
			a.status = "Bridge connection ended; disconnect before reconnecting."
			if err != nil {
				a.status += " " + err.Error()
			}
		default:
		}
	}
	result := map[string]any{"mode": "lobby", "status": a.status, "connected": a.online, "fund_pending": a.fundPending, "course_hash": course.Hash()}
	if a.game != nil {
		result["tables"] = a.game.Tables()
		if v, ok := a.game.View(a.sid); ok {
			result["mode"] = "bridge"
			result["view"] = v
			result["course"] = course.All()[v.Hole]
			result["table"] = a.sid
			play, _ := a.game.Playable(a.sid)
			fund, reason := a.game.CanFund(a.sid)
			result["can_play"] = play && a.online
			result["can_fund"], result["fund_reason"] = fund && a.online, reason
			if lobby, ok := a.game.Lobby(a.sid, 0, !a.online); ok {
				result["seating"] = lobby
			}
		}
	}
	if a.local != nil {
		winner, won, done := a.local.Outcome()
		result["mode"] = "local"
		result["course"] = course.All()[a.local.Index()]
		result["view"] = map[string]any{"Grid": a.local.Hole(), "Turn": a.local.Turn(), "Hole": a.local.Index(), "Score": a.local.Score(), "Done": done, "Won": won, "Winner": winner, "Results": a.local.Results()}
	}
	if a.practice >= 0 {
		result["mode"] = "practice"
		result["course"] = course.All()[a.practice]
		result["view"] = map[string]any{"Grid": match.HoleState{Balls: [2]sim.Ball{a.ball, a.ball}}, "Turn": 0, "Hole": a.practice, "Score": [2]int{}, "Done": a.ball.Holed, "Won": a.ball.Holed, "Winner": 0}
	}
	return result
}
func (a *app) call(q request) (any, error) {
	switch q.Method {
	case "hello":
		return map[string]any{"brand": "ORBIT GOLF", "courses": course.All(), "simulation": sim.Version, "course_hash": course.Hash()}, nil
	case "state":
		return a.state(), nil
	case "practice":
		if q.Hole < 0 || q.Hole >= 9 {
			return nil, fmt.Errorf("invalid hole")
		}
		a.local = nil
		a.practice = q.Hole
		a.ball = sim.Tee(course.All()[q.Hole])
		return a.state(), nil
	case "local_match":
		a.practice = -1
		a.local, _ = match.New(0)
		return a.state(), nil
	case "lobby":
		a.local = nil
		a.practice = -1
		return a.state(), nil
	case "preview":
		// Presentation-only: simulate a copy, never advance a match, sign a
		// message, journal an intent, or call the bridge.
		if q.Yaw >= 4096 || q.Power < 1 || q.Power > 1000 || q.Phase >= course.Cycle || q.Phase%course.PhaseStep != 0 {
			return nil, fmt.Errorf("invalid shot")
		}
		var hole int
		var ball sim.Ball
		switch {
		case a.practice >= 0:
			hole, ball = a.practice, a.ball
		case a.local != nil:
			if a.local.Done() {
				return nil, fmt.Errorf("match finished")
			}
			hole, ball = a.local.Index(), a.local.Hole().Balls[a.local.Turn()]
		case a.game != nil && a.online:
			v, ok := a.game.View(a.sid)
			if !ok || v.Done || v.Seat != v.Turn {
				return nil, fmt.Errorf("not your turn")
			}
			if ok, why := a.game.Playable(a.sid); !ok {
				return nil, fmt.Errorf("table not ready: %s", why)
			}
			hole, ball = v.Hole, v.Grid.Balls[v.Seat]
		default:
			return nil, fmt.Errorf("start practice or join a table")
		}
		r, err := sim.Simulate(course.All()[hole], ball, sim.ShotAt(q.Yaw, q.Power, q.Phase))
		if err != nil {
			return nil, err
		}
		return map[string]any{"preview": r, "yaw": q.Yaw, "power": q.Power, "phase": q.Phase}, nil
	case "shot":
		if q.Yaw >= 4096 || q.Power < 1 || q.Power > 1000 || q.Phase >= course.Cycle || q.Phase%course.PhaseStep != 0 {
			return nil, fmt.Errorf("invalid shot")
		}
		shot := sim.ShotAt(q.Yaw, q.Power, q.Phase)
		var r sim.Result
		var err error
		if a.practice >= 0 {
			r, err = sim.Simulate(course.All()[a.practice], a.ball, shot)
			if err == nil {
				a.ball = r.Ball
			}
		} else if a.local != nil {
			err = a.local.Play(match.Move{Hole: uint8(a.local.Index()), Seat: a.local.Turn(), Shot: shot})
			r = a.local.Last()
		} else if a.game != nil && a.online {
			v, ok := a.game.View(a.sid)
			if !ok {
				return nil, fmt.Errorf("no selected table")
			}
			if v.Turn != v.Seat {
				return nil, fmt.Errorf("opponent's turn")
			}
			r, err = sim.Simulate(course.All()[v.Hole], v.Grid.Balls[v.Seat], shot)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err = a.game.Play(ctx, a.sid, shot)
			}
		} else {
			err = fmt.Errorf("start practice or join a table")
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": r, "state": a.state()}, nil
	case "config":
		c, err := bridgeconn.Load(filepath.Join(a.dir, "bridge.json"))
		return map[string]any{"host": c.Host, "port": c.Port, "network": c.Network, "configured": c.Complete()}, err
	case "save_config":
		if a.online {
			return nil, fmt.Errorf("disconnect before editing credentials")
		}
		if q.Config == nil {
			return nil, fmt.Errorf("missing config")
		}
		return nil, bridgeconn.Save(filepath.Join(a.dir, "bridge.json"), *q.Config)
	case "connect":
		if a.rt != nil {
			return a.state(), nil
		}
		c, err := bridgeconn.Load(filepath.Join(a.dir, "bridge.json"))
		if err != nil {
			return nil, err
		}
		if c.Network == "mainnet" {
			return nil, fmt.Errorf("mainnet is disabled in this development build; use simnet or testnet3")
		}
		g, err := session.New(a.dir)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b, err := bridgeconn.Connect(ctx, c, g.Identity())
		if err != nil {
			g.Close()
			return nil, err
		}
		seed, err := identity.Load(a.dir)
		if err != nil {
			b.Close()
			g.Close()
			return nil, err
		}
		rt, err := sdk.Open(sdk.Config{Rules: g, Bridge: b, Identity: seed, Dir: a.dir, SeatTags: session.SeatTags})
		if err != nil {
			b.Close()
			g.Close()
			return nil, err
		}
		g.Bind(rt)
		for _, sid := range rt.Resumed().Restored {
			if _, seated := rt.Seat(sid); seated {
				if err := g.Ensure(sid); err != nil {
					rt.Close()
					g.Close()
					return nil, fmt.Errorf("restore table %s: %w", sid, err)
				}
			}
		}
		run, stop := context.WithCancel(context.Background())
		a.game = g
		a.rt = rt
		a.stop = stop
		a.done = make(chan struct{})
		a.runCtx = run
		a.runErr = make(chan error, 1)
		a.online = true
		done, runErr := a.done, a.runErr
		go func() { defer close(done); runErr <- rt.Run(run) }()
		a.status = "Connected. Create or accept a table in dcrpulse."
		return a.state(), nil
	case "disconnect":
		a.close()
		a.status = "Disconnected; recovery remains available in dcrpulse."
		return a.state(), nil
	case "poll":
		select {
		case err := <-a.jobs:
			a.fundPending = false
			if err != nil {
				a.status = err.Error()
			} else {
				a.status = "Stake request completed"
			}
		default:
		}
		if a.game != nil && a.online {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			for _, sid := range a.game.Tables() {
				if err := a.game.Prepare(ctx, sid); err != nil {
					a.status = err.Error()
					continue
				}
				_ = a.game.Start(ctx, sid)
			}
			if a.sid == "" {
				t := a.game.Tables()
				if len(t) > 0 {
					a.sid = t[0]
				}
			}
		}
		return a.state(), nil
	case "select_table":
		if a.game == nil {
			return nil, fmt.Errorf("not connected")
		}
		if _, ok := a.game.View(q.Table); !ok {
			return nil, fmt.Errorf("unknown table")
		}
		a.sid = q.Table
		a.local = nil
		a.practice = -1
		return a.state(), nil
	case "fund":
		if a.game == nil || !a.online || a.sid == "" {
			return nil, fmt.Errorf("no table")
		}
		if a.fundPending {
			return nil, fmt.Errorf("funding already pending")
		}
		a.fundPending = true
		a.fundDone = make(chan struct{})
		g, sid, jobs, done, ctx := a.game, a.sid, a.jobs, a.fundDone, a.runCtx
		go func() { defer close(done); jobs <- g.Fund(ctx, sid) }()
		return a.state(), nil
	case "settle":
		if a.game == nil {
			return nil, fmt.Errorf("not connected")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := a.game.Settle(ctx, a.sid)
		return a.state(), err
	case "receipt":
		if a.game == nil {
			return nil, fmt.Errorf("not connected")
		}
		r, err := a.game.Receipt(a.sid)
		if err != nil {
			return nil, err
		}
		return r.Export(filepath.Join(a.dir, "exports"))
	case "quit":
		return nil, nil
	default:
		return nil, errors.New("unknown IPC method")
	}
}
