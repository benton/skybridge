# skybridge: Go rewrite of the NexStar serial-to-TCP bridge

Date: 2026-10-26
Status: approved 2026-10-27

## 1. Intent

Today `bridge.py` opens a Celestron NexStar+ hand controller's USB serial
port and listens on `127.0.0.1:4030` so SkySafari can talk to the mount as
if it were a SkyFi. It works, but it is a single macOS-only Python script
with a hard-coded Prolific device path, no tests, no packaging, and no
documentation beyond its docstring.

The rewrite turns it into a small, professional, cross-platform tool:

- one statically linked Go binary per platform, no runtime dependencies
- Windows, macOS and Linux (including Raspberry Pi)
- works with more mounts and more USB-serial adapters, not just one
  Prolific cable on one Celestron controller
- tested, linted, documented, and built and packaged automatically by
  both GitLab CI and GitHub Actions

The behaviour that makes the current script pleasant must survive: start
it once, leave it running, power the mount on and off, unplug and replug
the cable, and SkySafari keeps working without restarting anything.

## 2. Goals, non-goals, anti-goals

### Goals

1. Byte-faithful relay between one serial port and one TCP client at a
   time, with the same connection semantics SkySafari expects (section 5).
2. Hot-plug resilience: serial port absent at startup, lost mid-session,
   and reappearing later are all handled without operator action.
3. Mount profiles: a named table of serial settings and identification
   probes for the common mount families SkySafari supports over serial.
4. Adapter discovery: find the right serial port automatically on each OS
   from known USB vendor/product IDs and device-name patterns, with an
   explicit override.
5. First-class CLI: `serve`, `list-ports`, `probe`, `version`, with
   consistent flags, `--help`, structured logs, clean shutdown on
   Ctrl-C / SIGTERM.
6. Tests for every package that holds logic; CI gates on format, vet,
   static analysis, vulnerability scan, and race-enabled tests.
7. Documentation a non-programmer astronomer can follow: install, wire up,
   configure SkySafari, troubleshoot.
8. Release pipeline on both GitLab and GitHub producing per-platform
   archives with checksums on every tagged version.

### Non-goals (version 1)

- Implementing or interpreting any mount protocol beyond the short probe
  used to identify a mount. The bridge stays a transparent relay.
- SkyFi UDP auto-discovery (SkySafari's "Auto-Detect SkyFi"). Users enter
  the IP and port by hand. Can be added later; the design leaves room.
- Serving more than one client concurrently or multiplexing several apps
  onto one mount.
- INDI / ASCOM Alpaca drivers. SkySafari already supports those natively
  and they are different products.
- Bluetooth serial, network-attached mounts (those connect to SkySafari
  directly).
- A GUI or system tray app.
- Installing itself as a system service (launchd / systemd / Windows
  service). Documented manually instead; a future `service` subcommand is
  a natural extension.
- Configuration files. Flags and environment variables are enough for a
  tool with a handful of options.

### Anti-goals

- A dependency tree. If the finished binary pulls in more than a couple of
  modules, the design has failed its own code-style rule.
- Platform-conditional behaviour visible to the user. The same flags and
  the same log lines must mean the same thing on every OS.
- Silent fallbacks. If the bridge cannot find a port, cannot open it, or
  gets a mount that does not answer, it says so clearly and keeps trying;
  it never pretends to be connected.

## 3. Constraints

- Language: Go, standard library first, per `docs/design/code-style.md`.
  Single binary, CLI subcommands for every function.
- Minimum Go: the version current at implementation time (1.26 today),
  pinned in `go.mod` and CI.
- Targets: `linux/amd64`, `linux/arm64`, `linux/arm` (32-bit Raspberry
  Pi), `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.
  `CGO_ENABLED=0` everywhere so cross-compilation from a Linux runner
  works and binaries are static.
- Commit messages follow Conventional Commits.
- The working directory is not yet a git repository. Initialising it is
  the first task of the plan.

## 4. Dependencies: the decisions

### Serial port access

The standard library has no serial port support. Three options:

| Option | Pros | Cons |
|---|---|---|
| `go.bug.st/serial` | Mature, used by the Arduino CLI, handles Windows quirks, pure Go core | One third-party module plus `golang.org/x/sys`; its port enumerator uses cgo on macOS, which breaks static cross builds |
| `github.com/tarm/serial` | Tiny | Effectively unmaintained; no port enumeration |
| Own package on `golang.org/x/sys` | No third-party code; exactly the five operations we need (open, configure 8N1 at a baud rate, read, write, close) plus enumeration; static everywhere | We own the Windows `SetCommState` / `SetCommTimeouts` code and its bugs; roughly 300 lines across three OS files |

**My call: write our own `internal/serial` package on `golang.org/x/sys`.**
`x/sys` is maintained by the Go team and is the layer every serial library
uses anyway. Our needs are narrow enough that the domain-specific version is
smaller than learning another library's API surface, and it keeps
`go.sum` to a single module. The package exposes a small `Port` interface
so `go.bug.st/serial` could be dropped in behind it if the Windows code
proves troublesome.

### Everything else

- CLI parsing: `flag` from the standard library with a hand-rolled
  subcommand dispatcher. No cobra, no urfave/cli.
- Logging: `log/slog`, text handler to stderr by default, `--log-format
  json` for people running it under a service manager.
- Testing: `testing` only. No testify. Table-driven tests and
  `net.Pipe` / `io.Pipe` doubles for the relay core.
- Static analysis tools (dev-time only, never linked into the binary):
  `gofmt`, `go vet`, `staticcheck`, `govulncheck`. These are installed by
  CI with `go install`, not vendored.
- Release packaging: **GoReleaser**, run only in CI on tags. It is the
  ubiquitous Go release tool, produces the archive matrix, checksums,
  changelog, and GitHub / GitLab release pages from one YAML file, and
  touches no application code. The alternative is a Go program under
  `tools/` that shells out to `go build` and `tar`; that is more code to
  maintain for a worse result. Day-to-day builds remain plain `go build`.

## 5. Behaviour

### Relay semantics (unchanged from today, now specified)

- Listen on one TCP address, default `127.0.0.1:4030`.
- Exactly one client at a time. A new connection replaces the old one: the
  old socket is closed, the serial port's buffers are flushed, and the new
  client starts clean. SkySafari opens a fresh connection for every
  position poll, several times per second, so this path must be cheap.
- Bytes are copied verbatim in both directions. No framing, no parsing, no
  buffering beyond what the OS gives us. `TCP_NODELAY` on every client.
- When the serial port is not open (mount off, cable out), incoming TCP
  connections are accepted and immediately closed. SkySafari interprets
  that as "telescope not responding", which is the honest answer. The
  listener is never closed while the bridge runs, so SkySafari never sees
  "connection refused" and gives up.
- When the serial port returns EOF or an error, close it, log once, and
  re-enter discovery. When a client write fails, drop the client only.

### Lifecycle

```
start ─► listen TCP ─► discover port ─► open ─► relay ─┐
                           ▲                            │ serial error
                           └── wait, retry (2 s) ◄──────┘
```

Ctrl-C or SIGTERM cancels a root `context.Context`; every goroutine exits,
the serial port and listener are closed, and the process returns 0.

### Mount profiles

A profile is data, not code:

```
name        celestron-nexstar
aliases     nexstar, celestron
baud        9600
framing     8N1
probe       send "Kx", expect "x#"   (NexStar echo command)
notes       NexStar+, NexStar SE/SLT/Evolution hand controllers, StarSense HC
```

Initial table (my call, chosen to match what SkySafari lists as serial
mount types and what is common in the field):

| Profile | Baud | Probe | Covers |
|---|---|---|---|
| `celestron-nexstar` (default) | 9600 | `Kx` → `x#` | NexStar+, SE, SLT, Evolution, CGEM, CGX, AVX hand controllers |
| `skywatcher-synscan` | 9600 | `Kx` → `x#` (SynScan speaks the NexStar dialect) | SynScan V3/V4/V5 hand controllers, EQ6, HEQ5, AZ-GTi over cable |
| `meade-lx200` | 9600 | `:GVP#` → product name | LX200 Classic/GPS, Autostar, Autostar II, ETX |
| `ioptron` | 9600 or 115200 (two sub-profiles) | `:MountInfo#` → 4-digit code | CEM, GEM, iEQ, SmartEQ |
| `losmandy-gemini` | 9600 | `:GVP#` (LX200 dialect) | Gemini 1 and 2 |
| `generic` | user-specified | none | anything else: pass `--baud` |

The probe is used by the `probe` subcommand and, optionally, by
`serve --verify`. Default `serve` does not probe, because some mounts
dislike unexpected traffic and SkySafari will do its own handshake.

`serve --verify` (my call): the probe is one more step in the open path,
run every time the serial port is opened, including after a mid-session
loss. A failed probe is logged as a warning, the port is closed, and the
bridge retries after the usual delay, exactly as it does for an absent
port. `serve` therefore never exits because of a probe; a daemon under a
service manager must not crash-loop because the mount was slow to boot.

`probe` with `--mount` sends that profile's probe once and reports.
`probe` without `--mount` (my call) runs an identification sequence: each
profile in table order, reconfiguring the baud rate between profiles,
stopping at the first match. The order puts the harmless echo-style probes
first. The docs state plainly that this sends a few short commands from
other mount dialects to the controller, and that `--mount` avoids it.

If a probe for a family turns out to be wrong during implementation (I am
confident about NexStar and LX200, less so about iOptron), the builder
drops the probe from that profile rather than guessing.

### Port discovery

Discovery returns candidate ports in order of confidence:

1. `--port` given: use it, no search. For `serve` a missing path is
   treated like any absent port: wait and retry, so a service started
   before the cable is plugged in still works. For `probe` and
   `list-ports` a missing path is an error.
2. USB VID/PID match against a known-adapter table: Prolific PL2303
   (`067B`), FTDI (`0403`), Silicon Labs CP210x (`10C4`), WCH CH340/CH341
   (`1A86`), and Celestron's own USB-equipped controllers (`Celestron`
   vendor `1D4B`, to be confirmed during implementation).
3. Device-name patterns per OS: `/dev/cu.*usbserial*`, `/dev/cu.PL2303*`,
   `/dev/cu.SLAB_USBtoUART*`, `/dev/cu.wchusbserial*` on macOS;
   `/dev/serial/by-id/*` then `/dev/ttyUSB*`, `/dev/ttyACM*` on Linux;
   `COM*` on Windows, read from the registry's `SERIALCOMM` key and the
   USB enumeration tree for VID/PID.

If more than one candidate matches, `serve` picks the first and logs the
others; `list-ports` shows all with their VID/PID and friendly name so the
user can pass `--port`.

How VID/PID is read on each OS is left to the builder, within these
facts: Linux exposes it in sysfs next to each tty device; Windows exposes
it through SetupAPI, reachable from `x/sys/windows`; macOS exposes it only
through IOKit, which needs cgo. On macOS the builder may therefore settle
for name patterns alone, or shell out to `ioreg -p IOUSB -l` and parse its
output. Whatever the method, discovery must finish within one second and
must never require elevated privileges.

### CLI

```
skybridge serve      [--port PATH] [--mount PROFILE] [--baud N]
                       [--listen ADDR] [--verify] [--log-level L]
                       [--log-format text|json]
skybridge list-ports [--json]
skybridge probe      [--port PATH] [--mount PROFILE] [--baud N] [--json]
skybridge mounts     [--json]          list known profiles
skybridge version    [--json]
```

- Running `skybridge` with no subcommand runs `serve` with defaults,
  so double-clicking the binary or running it from a shortcut works the
  same way the shell script does today.
- Every flag has an environment-variable twin, `SKYBRIDGE_PORT` and so
  on, for service managers.
- `--listen` defaults to loopback. Users who want SkySafari on a tablet
  to reach the bridge on a laptop set `--listen 0.0.0.0:4030`; the docs
  explain that this exposes the mount to the whole LAN and that there is
  no authentication, because the SkyFi protocol has none.
- Exit codes: 0 clean shutdown, 1 usage error, 2 could not bind the
  listener, 3 `probe` found no responding mount. `serve` only ever exits
  0, 1 or 2.

### Logging

`slog` text to stderr. One line per state change, never per poll:

```
18:04:12 INFO listening addr=127.0.0.1:4030
18:04:12 INFO waiting for serial port mount=celestron-nexstar
18:04:40 INFO serial port opened path=/dev/cu.PL2303G-USBtoUART110 baud=9600
18:09:03 WARN serial port lost err="EOF"; retrying
```

`--log-level debug` adds one line per client connect/replace, which is
noisy by design and off by default.

## 6. Code layout

```
cmd/skybridge/main.go        entrypoint, wires subcommands
internal/cli/                  subcommand dispatch, flag/env handling, exit codes
internal/bridge/               relay core: Listener + io.ReadWriteCloser → runs loop
internal/serial/               Port interface; port_unix.go, port_darwin.go,
                               port_linux.go, port_windows.go; enumeration
internal/discovery/            adapter table, candidate ranking, per-OS listing
internal/mounts/               profile table, probe runner
internal/version/              ldflags-injected version, commit, date
docs/                          user and developer docs (section 8)
.goreleaser.yaml
.gitlab-ci.yml
.github/workflows/ci.yml, release.yml
```

Rules:

- `internal/bridge` depends only on `net`, `io`, `context`, `log/slog`. It
  takes a `func(context.Context) (io.ReadWriteCloser, error)` to open the
  serial side, so tests inject pipes and never touch hardware.
- `internal/serial` is the only package with build tags and the only one
  importing `golang.org/x/sys`.
- No package imports `internal/cli`. `cli` imports everything else.

## 7. Testing and quality

- **Unit tests**, race detector on, for `bridge` (client replacement,
  flush on connect, serial loss and recovery, refuse-while-absent,
  shutdown), `mounts` (profile lookup, probe matching against canned
  responses), `discovery` (ranking against fake device listings), `cli`
  (flag/env precedence, exit codes, `--json` output shape).
- **Serial integration test** on Linux CI only: `socat` creates a pty
  pair, the test opens one end through `internal/serial` and checks bytes
  and baud configuration round-trip. Skipped elsewhere with a clear
  `t.Skip` message.
- **End-to-end smoke test**: a fake NexStar responder on one `net.Pipe`
  end, real `bridge` in between, a client that sends `Kx` and expects
  `x#`. Runs everywhere.
- **Static gates**, all blocking in CI: `gofmt -l`, `go vet`,
  `staticcheck`, `govulncheck`, `go mod tidy` leaves no diff.
- Coverage is reported, not gated. A number is a number.

## 8. Documentation

- `README.md`: what it is, 30-second install, 30-second SkySafari setup,
  link to the rest.
- `docs/install.md`: per-OS install, including macOS Gatekeeper
  quarantine removal, Linux `dialout` group, Windows driver notes for
  each adapter chipset.
- `docs/skysafari.md`: step-by-step with the exact SkySafari settings
  (Scope Type, Mount Type, Connect via WiFi, IP, port 4030, "SkyFi"
  option), for SkySafari 6 and 7.
- `docs/hardware.md`: supported mounts and adapters, cable pinouts for the
  NexStar+ hand-controller port, which adapters are known-good.
- `docs/troubleshooting.md`: symptom → cause → fix, driven by the log lines
  the tool actually prints.
- `docs/development.md`: layout, how to run tests, how to add a mount
  profile or an adapter, release procedure.
- `CHANGELOG.md`, keep-a-changelog format, with the GoReleaser-generated
  notes appended on release.
- `LICENSE`: MIT.

The SkySafari manuals that used to sit in this directory are Simulation
Curriculum's copyrighted material. They now live outside the repo at
`~/h/resources/manuals/skysafari/` and are never committed.
`docs/skysafari.md` paraphrases them and never quotes them.

`bridge.py` and `scope-bridge.sh` (my call): kept, untouched, until the Go
binary has been verified against the real NexStar+ hand controller, then
removed in a single commit. `__pycache__/` is never committed.

## 9. CI and release

Both pipelines do the same three jobs so neither platform is second-class:

1. **check**: gofmt, vet, staticcheck, govulncheck, tidy diff.
2. **test**: `go test -race ./...` on Linux, macOS and Windows runners
   where the platform offers them. GitHub has all three. GitLab shared
   runners are Linux only; the GitLab pipeline runs the test job on Linux
   and relies on cross-compilation plus the Linux pty test for the rest,
   with a note in `docs/development.md` on enabling macOS/Windows SaaS
   runners if the project ever pays for them.
3. **build**: cross-compile the full target matrix with `CGO_ENABLED=0`
   on every push so a platform break is caught before release.

On a `v*` tag, a **release** job runs GoReleaser: archives (`.tar.gz` for
Unix, `.zip` for Windows), `checksums.txt`, and a release page on the
host that triggered the pipeline. Deb/rpm packages, Homebrew tap and
winget manifests are out of scope for version 1 but are one-line
additions in `.goreleaser.yaml` later.

Version information is injected with `-ldflags -X` into
`internal/version` so `skybridge version` reports tag, commit and date.

## 10. Left to the builder

- Exact Windows serial implementation details (overlapped vs. blocking
  I/O, timeout values), as long as the `Port` interface and tests hold.
- How to read USB VID/PID on macOS without cgo, or whether to settle for
  name patterns there.
- Internal buffer sizes and read timeouts in the relay.
- Whether `list-ports` output is a table or aligned columns.
- The exact set of staticcheck checks enabled.

## 11. Decisions from the owner (2026-10-27)

1. **Name:** `skybridge`. A web search found no astronomy or serial tool
   of that name; the only hits are unrelated companies. The Go module
   path is `git.h.usky.io/huskynet/skybridge` (remote
   `git@git.h.usky.io:huskynet/skybridge.git`).
2. **License:** MIT.
3. **Hardware for testing:** one Celestron NexStar+ hand controller over
   the Prolific cable. Every other profile ships marked "untested, please
   report" in `docs/hardware.md`.
4. **Serial package:** own package on `golang.org/x/sys`, as in section 4.
5. **Listen default:** loopback only. Binding to another interface is an
   explicit `--listen` choice.
6. **Manuals:** moved out of the tree to `~/h/resources/manuals/skysafari/`.
