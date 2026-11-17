# kb7screen lighting — layered effects streamed from the PC

Design for driving the KB7's 112 LEDs from `kb7screen`, so that Swarm II is no
longer needed for lighting either. Status: approved; implemented, hardware acceptance pending. Builds on
[`screen-app.md`](screen-app.md).

## Intent

The keyboard should light the way I want per profile, static or animated, and
react to what I do: a colour wave or a slow breathing glow as the base, a
ripple of colour running out from every key I press, and on the Star Citizen
profile lights that follow ship states (landing gear, quantum, shields). Some
keys, WASD first of all, must always stay lit in a fixed colour so they can be
found in the dark, whatever the effect does.

New effects must be easy to add. Everything has to be settable from the
configuration folder now and from a simple graphical program later, so every
effect describes its own settings.

## Goals

- Per profile, a stack of lighting layers, from bottom to top:
  1. **Base**: one colour, a colour wave, or breathing.
  2. **Ripple**: on each key press a ring of colour runs out from that key
     over the keyboard and fades; the rest of the keyboard shows the base.
  3. **States**: named on/off states (gear, quantum, shields) that light
     chosen keys in a colour while on.
  4. **Fixed keys**: chosen keys always in a fixed colour (WASD), on top of
     everything.
- `kb7screen run` streams the result to the keyboard and follows profile
  switches, the keyboard's brightness, and the RGB on/off button.
- A new effect is one Go file implementing a small interface plus one line
  registering it; its settings are declared, so the configuration check, the
  documentation and a later GUI know them without extra code.
- Windows first; Linux with the same code where the platform parts allow it.

## Non-goals for now

- The GUI itself, and the auto-update it will offer (opt-in, fetching releases
  from GitHub and checking them against the published SHA-256). Both come
  later; this design only keeps the configuration GUI-friendly.
- Game events the PC cannot see: damage, missile lock. Star Citizen offers no
  API and its `Game.log` does not contain them. Reading the game's memory or
  injecting into it breaks its terms of service and is excluded.
- Effects written as scripts outside the executable. The effect interface is
  kept narrow so that this could be added later without changing effects.
- Using the keyboard's own onboard effects (Fn + Left/Right). They did nothing
  visible in any capture.

## Anti-goals

- Anything that touches Star Citizen's process, memory or files beyond
  reading `Game.log` (already done for nothing here).
- A key logger: key presses are used for the ripple and the states in memory
  only, never written to disk or the log.
- Sending any lighting command not in `PROTOCOL.md`.
- Breaking the button features: taps, profile switching and apply must keep
  working exactly as now.
- Fighting Swarm II for the LEDs (the strobing seen in
  `13-replay-led-stream-green-1`): no stream while Swarm II runs.

## Constraints (measured unless marked)

From `PROTOCOL.md`, "Interface 1: the Swarm II state stream", "RGB lighting:
report `0x11`", "LED map" and "Device notifications":

| What | Fact |
|---|---|
| Live colours | `a1` frames on interface 1 (`0x0d` out, `0x85` acknowledgement in): six 64-byte frames `a1 <bank 1–6> <len>` + 60 bytes, length `68 01` (360) on bank 1 only. The 360-byte payload is seven 48-byte blocks of R[16] G[16] B[16] (112 LEDs) followed by 24 bytes; those were all zero in every Swarm II frame of `03-rgb-perkey-esc-1040c0-1` |
| Rate | Swarm II sends 20 Hz; our own 30 Hz stream rendered without lost frames. The keyboard shows the most recent frame |
| Stored colours | report `0x11`, 348 bytes, written as a feature report; with nobody streaming, a written table appears and stays. Header bytes 3 and 7 unknown, byte 6 brightness |
| Brightness | the host scales the colours it streams. Fn + Up/Down: the keyboard reports the new level as `03 00 0c <percent>` (only after `0x0e`, which `run` writes) in steps of ten from 20 to 100. The exact curve Swarm II uses is unknown; nine points are known |
| RGB on/off | the keyboard switches its LEDs off and on itself and reports `03 00 11 <off>` |
| Key positions | LED index of 94 keys and lights, rows of fourteen; Escape is 0, WASD are 30, 43, 44, 45, H and J are 48 and 49 |
| Key presses | the keyboard sends ordinary keyboard reports to the operating system; not read by our code so far |

Not measured, needed before relying on it:

1. **What the keyboard shows when the stream stops** (run ends or crashes):
   the last frame, or the stored `0x11` of the active profile.
2. **What a profile switch does to a running stream**: whether the stored
   table of the new profile flashes before our next frame.
3. ~~Reading key presses of the KB7 through Windows Raw Input while another
   program has the focus.~~ **Measured 2026-11-17** with `kb7screen keytest`:
   with Notepad focused, `wasd` arrived as four key-down/key-up pairs; with
   Star Citizen focused and in use, W A S D (including held, while moving),
   N, B, H, J, F, LShift and LAlt+N all arrived, in order. A held key repeats
   its key-down until it is released (LShift and A were seen four times in a
   row), so repeats must be ignored. During Alt+Tab only the Tab key-up
   arrived. Typing on a second keyboard in the same run produced nothing, so
   the device filter keeps other keyboards out. Still open: whether Fn-layer
   keys show up.
4. **The brightness level at start-up**: read from `0x11` byte 6 through the
   selector (`05 <profile> b0 00`), as Swarm II does at start. Reading this
   block from our own code has not been done; `b8`, `b9` and `ba` have.
5. **Streaming while an upload or a table write runs** on the same interface:
   `apply` reads acknowledgements on interface 1 and a concurrent stream would
   mix its own into them. Not tried, and not to be tried (see Run below).

## Approaches considered

| Approach | For | Against |
|---|---|---|
| **Effects in Go inside kb7screen** (chosen) | One executable, fast, testable; an effect is one file; settings declared per effect so a GUI can show them | A new kind of effect needs a new build |
| Effects as scripts next to the executable (e.g. Lua in a pure-Go interpreter) | New effects without building; shareable | More work now, an extra dependency, script errors only show at run time |
| Settings only, fixed building blocks | Simplest | No genuinely new effects, which is one of the wishes |

Chosen by the owner: everything in kb7screen, in Go, with a simple GUI on top
later.

## Design

### Where

A new package `internal/light` (frame building lives in `internal/proto`,
next to the existing frames). The stream runs inside `kb7screen run`, which
already owns the keyboard, follows profiles and polls for Swarm II.

### Configuration

A `[light]` part in each profile's layout file. Example for the SC profile:

```toml
[light]
fps = 30                         # optional, 10–30

[[light.layer]]                  # bottom first
effect = "wave"
colors = ["#0040FF", "#00C0FF", "#8000FF"]
speed = 0.5                      # cycles per second
direction = "right"

[[light.layer]]
effect = "ripple"
color = "#FFFFFF"                # or colors = [...] to cycle per press
speed = 12                       # keys per second
width = 1.5                      # keys

[[light.layer]]
effect = "state"
name = "gear"
toggle = "N"                     # the key that flips it
start = "on"                     # state when the game starts
keys = ["N"]
color = "#30E070"

[[light.layer]]
effect = "state"
name = "quantum"
toggle = "B"
keys = ["B"]
color = "#00AAFF"
blink = 0                        # Hz; 0 is steady

[[light.layer]]
effect = "fixed"
keys = ["W", "A", "S", "D"]
color = "#FFFFFF"
```

- Keys use the same names as button actions (`W`, `LShift`, `F1`, `Space`),
  plus names for the lights that are not keys: `PrevTrack`, `NextTrack`,
  `GameMode`, `ProfileCycle`, `MicOff`, `RGBButton`, `Wrist1`–`Wrist10`, and
  the groups `all`, `keys`, `wrist`.
- A layer may take `keys = [...]` to limit where it shows; the default is
  `all`. Unknown effects, unknown settings, wrong types and out-of-range
  values are refused when the file is loaded, with the setting's name.
- A profile without `[light]` gets no stream: the keyboard shows its stored
  table, as without Swarm II today.

### Effects

```go
type Effect interface {
    // Render adds this layer to the frame at time t (since start).
    Render(f *Frame, t time.Duration, in *Input)
}

type Spec struct {
    Name     string
    Settings []Setting          // name, kind, default, min/max, help text
    New      func(s Values) (Effect, error)
}
```

- `Frame` holds 112 colours with alpha per LED; layers are composed bottom to
  top with straight alpha, so an effect that only touches a few keys leaves
  the rest of the layer below visible.
- `Input` carries the key presses since the last frame (LED index and time)
  and the current states, so effects stay pure functions of time and input
  and can be tested without a keyboard.
- Setting kinds: colour, colour list, number with range, choice (e.g.
  direction), key list, duration. That list is what a GUI has to draw.
- Each LED has a position in key units (from the LED map's rows of fourteen,
  with the real offsets of the rows and the wider keys), used by the wave and
  the ripple so they move across the board as it looks, not along the index.
- First set: `static`, `wave`, `breathing`, `ripple`, `state`, `fixed`.

### States

- `toggle` is a chord in the action syntax (`N`, `LAlt+N`). It matches on
  key-down of its last key when exactly its modifiers are held: `N` does not
  match LAlt+N, which in Star Citizen is usually another binding. Key-up and
  how long a key is held play no part.
- A state flips when its toggle chord is pressed on the KB7, or when a screen
  button's action sends that chord.
- A screen button may name a state: `state = "gear"`. With an action it sends
  the action and flips the state; **without an action it flips only the
  state** and sends nothing to the game. That is the way to bring a state
  back in line with the game.
- **States belong to their profile.** Two profiles may use the same name
  without sharing it. Toggle presses count only while that profile is active.
- A state takes its `start` value (default off) when its profile is switched
  to because its program started, and when `run` starts and finds that
  program already running. Otherwise it keeps its value across switches back
  and forth.
- **They can drift from the game.** A key pressed on another keyboard, a
  binding that does more than one thing, or a death in the game all change
  the ship without us seeing it. There is no game data to correct it; the
  state-only button above is the remedy.

### Key presses

Windows Raw Input for keyboards, registered with "input sink" on a hidden
window so presses arrive whatever program has the focus, filtered on the
KB7's device handle so other keyboards and our own `SendInput` keystrokes
(which carry no device) are ignored. The scan code is turned into the LED
index through the existing key tables. Raw Input only listens: the key still
goes to the game unchanged. Modifier state is tracked from the same events.

This is measurement 3, and the build **starts with it** as a small hardware
check. If it fails, the ripple and key-toggled states wait for another source;
the base, fixed keys and button-driven states are built regardless.

Linux: the KB7's `/dev/input/event*` node. Read access to a keyboard's event
node lets any program of that user read its keys, so this gets its own udev
rule, separate from the hidraw one, documented as optional; without it the
ripple and key-toggled states are off on Linux.

Nothing about the keys is stored or logged.

### Run

- After `0x0e` and the first profile switch, start streaming the active
  profile's layers at its `fps`; on `03 00 01 <profile>` switch to that
  profile's layers at the next frame.
- Brightness: start from the active profile's `0x11` byte 6 (once measured,
  otherwise 100 %), follow `03 00 0c`, and scale every colour with
  `v x percent / 100`. Whether the level is per profile is not known; `run`
  reads byte 6 again after every profile switch (one selector read on the
  feature interface, not on the stream's) and falls back to the last level if
  that read fails.
- When the stream stops (Ctrl+C, closing the keyboard for Swarm II, a switch
  to a profile without `[light]`), `run` sends one last frame of the profile's
  layers at t = 0 with no ripple and states off, so no half-finished ripple
  or stale state stays frozen if the keyboard keeps the last frame. This goes
  after the held keys are released and before the keyboard is closed, and is
  skipped when the keyboard is already gone.
- RGB off (`03 00 11 01`): keep streaming, the keyboard keeps them dark.
- While Swarm II runs, `run` already closes the keyboard; the stream stops
  with it and resumes after.
- The stream writes one frame set per tick and reads its six
  acknowledgements; a missed tick is skipped, never queued, so the lighting
  never lags behind.
- `apply` and `restore` refuse to start while a `run` is active (a lock
  file in the configuration folder plus a check for the process), because
  their uploads and the stream share interface 1. (My call; the GUI later
  does both in one process and can pause the stream instead.)

### Stored table (static fallback)

`apply` also writes each lit profile's `0x11` with the layers frozen at
t = 0 (states off, no ripple), keeping the header it read, so the keyboard
looks close to right when `run` is not running. This depends on measurement 1
and on reading `0x11` (measurement 4); until both are done it is left out.

## Testing

- Unit tests: `a1` framing (six frames, length on bank 1, zero tail),
  brightness scaling, layer composition, every effect at fixed times with
  fixed input, setting validation, the key and LED name tables, LED positions.
- A preview command, `kb7screen light-preview <config> --profile N`, writes
  an animated GIF of a few seconds with simulated key presses, so effects can
  be checked without the keyboard.
- Hardware checklist: each effect, ripple while Star Citizen has the focus,
  states, brightness keys, RGB button, profile switch, Swarm II starting and
  stopping, unplugging during `run`.

## Left to the builder

Package layout inside `internal/light`, the exact LED positions, easing
curves, default values of settings, the GIF size and frame rate, log wording.
