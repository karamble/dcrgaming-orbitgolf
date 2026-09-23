# ORBIT GOLF

Full-3D orbital mini-golf, rendered with Godot and simulated deterministically
in Go. Two players, nine holes across Shipyard, Reactor and Shattered Moon.
The public brand is ORBIT GOLF; the repository and Go module are
`github.com/karamble/dcrgaming-orbitgolf`. The bridge game ID remains
`dcrminigolf`; existing profile locations and signing tags are unchanged.

Playable development prototype. **Mainnet is disabled.** Bridge lifecycle tests
use the SDK's local test bridge, not real dcrpulse wallets. This is not an
audited or release-ready wagering game.

## Play

From this directory:

```sh
make play
```

The portable Godot 4.6.3 Linux executable is installed locally under `.tools/`.
It is ignored by Git. A fresh checkout needs Godot 4.6.3 or a compatible version:
`make play GODOT=/path/to/godot`. Go 1.26+ and the sibling `../dcrgaming-sdk`
checkout are required. No dedicated GPU is required; the renderer uses OpenGL
Compatibility. This build was rendered on Intel Iris Plus integrated graphics.

Choose any of nine practice holes, or a local two-player match. Local modes do
not connect to a bridge and need no credentials or wallet.

- Click the course to aim, or hold left/right arrows (A/D) to turn smoothly.
  Up/down arrows also turn the aim; hold Shift for precision aiming.
- Hold Space to fill the power bar, then release to putt. Full power takes
  1.8 seconds and stays capped until release. The on-screen button also supports
  press-and-hold charging. Losing focus or leaving the hole cancels a charge.
- The UI animates the Go simulation's verified path after release.
- A textured, fading route shows the Go simulation's predicted banks, magnetic
  curves and ramp flight, with an endpoint ring. Orange warns of a penalty;
  gold indicates a predicted cup capture. Before charging, a 35% guide is shown;
  charging replaces it. Releasing a shot resets the power bar to zero and the
  next idle guide to 35%, never the previous shot's power. The preview power is shown
  beside the bar. Updates are throttled and can lag slightly while aim/power
  changes; they never sign a move or advance the match.
- The camera follows the active ball, including its elevation and shot replay.
  Right-drag or hold Q/E to orbit; wheel zooms. C recentres on the ball, not the
  course origin. V toggles a whole-course overview for planning long approaches.
  Tunnel roofs become transparent near the ball. Reduced motion removes camera
  smoothing, but does not hide the ball or freeze gameplay obstacles.
- R replays the last shot on the current hole; Escape returns to the lobby.

## Rules

Shipyard: First Contact, Cargo Run, Launch Bay.
Reactor: Containment, Flux Channel, Critical Angle.
Shattered Moon: Low Orbit, Broken Causeway, Event Horizon.

Players putt independent balls which never collide. The first player alternates
by hole. Players alternate shots unless one has already finished the hole.
Ramps launch balls; there is no separate chip control. Falling into
the void adds one penalty and returns the ball to its pre-shot position.

The expedition layouts run 128–164 metres from tee to cup, with par 5–7,
raised decks, uphill/downhill approaches, narrow causeways, covered tunnels and
selected jump gaps. Full power rolls roughly 31 metres on level turf. Ramps
have no static friction: insufficient uphill power rolls back down, including
in low gravity. Frozen regression routes finish all nine holes in 4–6 putts
without penalties; these are safe routes, not proven optimal scores.

Cargo Run and Event Horizon have moving airlocks; the reactor courses use
cycling laser barriers. Red blocks/bounces the ball, green opens the passage.
The HUD shows the cycle, and the fading guide predicts the crossing at the
current launch phase. Wait and release your charged putt at the right moment.
Preview requests are quantized to 100 ms and can lag while charging/aiming.

Timing is deterministic, not a network latency contest: each signed shot packs
a chosen phase in a six-second cycle alongside angle and power. The Go engine
advances obstacles at 120 Hz from that phase; peers replay the same result.
The client clock only selects the phase, never determines collisions. This is
simulation version 2 with a new course hash. Both peers must use matching rules;
finish any existing v1 match using the old build rather than replaying it here.

Lower strokes wins a hole; ties give neither player a point. A seat that has not
holed out at the 12-stroke cap scores 13 for that hole. An unbeatable lead ends
the match early; equal scores after nine holes draw. Par is informational.

## dcrpulse development connection

Register game ID `dcrminigolf`, version 1, in dcrpulse. Use simnet or testnet3.
Open Bridge settings, select the matching network, and paste the client
certificate, client private key and bridge certificate. The private key uses a
separate hidden clipboard-paste control to preserve PEM line breaks. These are
bridge credentials, **never wallet seed words or wallet private keys**.

Credentials are saved with owner-only permissions in the backend profile,
normally `$XDG_CONFIG_HOME/dcrminigolf` or `~/.config/dcrminigolf`. Nothing secret
is returned by the config-read command. To use independent development peers:

```sh
.tools/Godot_v4.6.3-stable_linux.x86_64 --path client -- --appdata /tmp/orbit-peer-a
```

Connect, create/accept a table in dcrpulse, then choose Open table. Both peers
must agree the rules/course hash and verify admission bonds before the stake
button is enabled. Its dialog discloses the stake, bond and refund lock. The SDK
owns admission payments, stake dispatch, durable transport and settlement.
Approve payment requests in dcrpulse. Do not retry an ambiguous payment or
delete a funded profile to clear an error. Recovery belongs in dcrpulse.

The inherited development stake policy permits 50,000–200,000 atoms per seat;
refund and bond locks are at least 288 blocks. Move deadline: 12 blocks; match
deadline: 144 blocks. Cooperative payouts require both signatures. The game
cannot force an uncooperative loser to sign a payout.

## Architecture and checks

`client/` is the Godot presentation. `cmd/orbit-backend` is a child process using
versioned JSON-lines over stdin/stdout; it opens no local listening port.
`pkg/course` supplies integer geometry to both simulation and renderer.
`pkg/sim` runs bounded 120 Hz integer physics: no GPU physics, floating point,
randomness or wall clock participates in a shot outcome. Visual interpolation
is deliberately separate. `internal/match` scores the holes.

Course visuals use procedural hex-weave surfaces, brushed panel metal, rough
asteroids and dimpled golf balls. Directional shadows and cool fill lighting
run in the Compatibility renderer. These cosmetic shaders do not change
collision geometry, simulation, or the agreed course hash. Reduced motion
disables the route's flowing dash animation without removing its texture/fade.
Perimeter walls use thick, chamfered armor with metallic caps, panel joints and
recessed blue light strips inspired by the loading artwork. Armor extends
outward from the original collision planes; interior obstacle footprints and
open ramp ends are preserved.
Planets rotate, orbital/reactor rings precess, asteroids drift and tumble, stars
twinkle, and occasional comets cross the background with fading tails. Reduced
motion freezes these decorative animations. None of them are collision objects
or simulation inputs.

The SDK session adapter, journal and signing safeguards were adapted from
FOUR2WIN; fixed-point helpers from dcrstakewars. Game ID, manifest and seat-key
tags are separate. Position-nonce signatures and pre-sign intent journaling
protect against contradictory moves; this is not a claim of complete security.

```sh
make check       # race tests, vet, Godot import, nine-course IPC smoke test
make preview     # real rendered PNGs in artifacts/
```

Tests cover all-course deterministic/bounded simulation with a frozen corpus
hash, ramps, rails, cup capture, penalties, turns, replay, early clinch, draws,
signed-log tampering/equivocation, and two real SDK runtimes connected through
the SDK test bridge. The latter exercise funding after rule agreement, a full
216-shot drawn match and settlement, and restart without duplicate payments.

GitHub Actions runs `.github/workflows/ci.yml` on pushes and pull requests:
formatting, `go mod tidy` cleanliness, module verification, escrow dependency
pins, backend build, race tests, `go vet`, and headless Godot import/smoke tests.
It also rejects tracked files covered by `.gitignore`, including artifacts and
binaries. Godot import and smoke logs fail the check on script errors even if
the engine exits successfully. CI does not publish screenshots or binaries.

CI checks out the public SDK beside the game at commit
`bd72465cbee7cd2491531cb2b9ffee3652b46d6c`, preserving the local `replace` in
`go.mod`. Use that SDK revision to reproduce CI locally; update the workflow
pin deliberately when upgrading the SDK. Actions are commit-pinned and the
Godot 4.6.3 download is checked against its official release SHA-512 checksum.

## Next release gates

- Human playtesting and course balancing; the current geometry and scenery are
  a prototype art pass, not the fidelity shown by the illustrated title art.
- Two actual dcrpulse instances on simnet: invitations, confirmation delays,
  disconnects, ambiguous payments, winning and drawn settlement, recovery.
- Richer admission/recovery UI, full scorecards, opponent-shot animation and
  spectator polish. Current remote state updates are polled snapshots.
- Cross-platform simulation corpus checks, packaged exports, performance
  profiling and independent review before considering real-value play.

Screenshots in `artifacts/` are local-only, ignored by Git, and must never be
committed. Regenerate them with `make preview`. The generated logo and title background
are under `client/assets/`; exact prompts and generation method are recorded in
[ARTWORK.md](client/assets/ARTWORK.md).
