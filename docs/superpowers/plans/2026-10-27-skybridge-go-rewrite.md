# skybridge Go Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `bridge.py` with `skybridge`, a tested, documented, cross-platform Go binary that relays a telescope hand controller's serial port to TCP for SkySafari, with release pipelines on GitLab and GitHub.

**Architecture:** A pure relay core (`internal/bridge`) that knows nothing about hardware takes an `Opener` callback and a listener. The CLI wires that core to a mount-profile table (`internal/mounts`), a USB-adapter ranking (`internal/discovery`), and the only OS-specific package (`internal/serial`). Every package except `serial` is tested with in-memory pipes; `serial` is tested on a Linux pseudo-terminal in CI.

**Tech Stack:** Go 1.26, standard library, `golang.org/x/sys` (the only module dependency). Dev-time tools: `staticcheck`, `govulncheck`, GoReleaser v2. CI: GitHub Actions and GitLab CI.

**Spec:** `docs/superpowers/specs/2026-10-26-go-bridge-design.md`

## Global Constraints

- Module path `git.h.usky.io/huskynet/skybridge`; remote `git@git.h.usky.io:huskynet/skybridge.git`. Go `1.26` in `go.mod`.
- The shipped binary links exactly one third-party module: `golang.org/x/sys`. No cobra, testify, or serial libraries.
- `CGO_ENABLED=0` for every build. Targets: `linux/amd64`, `linux/arm64`, `linux/arm` (GOARM=6), `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.
- Commit messages use Conventional Commits. Commit messages and PR text never mention Claude or Anthropic.
- Default listen address `127.0.0.1:4030`. Default mount profile `celestron-nexstar`. Retry delay 2 s.
- Exit codes: 0 clean shutdown, 1 usage error, 2 could not bind listener, 3 `probe` found no responding mount. `serve` only exits 0, 1 or 2.
- Every `serve`/`probe` flag has an environment twin `SKYBRIDGE_<FLAG>` (dashes to underscores, upper case). Flag beats env beats default.
- Serial framing is always 8 data bits, no parity, 1 stop bit, no flow control. Supported baud rates: 4800, 9600, 19200, 38400, 57600, 115200.
- Logs go to stderr via `log/slog`; one line per state change, never per poll. Nothing is written to stdout except command output.
- The old `bridge.py` and `scope-bridge.sh` stay in the tree until Task 17 removes them after hardware verification.
- No file named `Makefile` or shell build script. Dev checks run through `go run ./tools/check`.

## Review Focus

1. SkySafari reconnects several times a second. Two hundred connect/replace cycles must not leak goroutines or file descriptors, and the last client must still relay. Pinned in Task 3.
2. Serial bytes that arrive while no client is connected must be discarded, not block the serial reader and not be delivered stale to the next client. Pinned in Task 3.
3. A port path that disappears between discovery and open (unplug race) must be treated as "absent, retry", never as fatal. Pinned in Task 11.
4. `--baud abc` or `--baud 1200` must exit 1 with a message naming the supported rates, before touching any hardware. Pinned in Task 11.
5. `--listen` on an address already in use must exit 2 with the OS error in the message, not panic or hang. Pinned in Task 11.

---

### Task 1: Repository bootstrap and version package

**Files:**
- Create: `go.mod`, `.gitignore`, `LICENSE`, `README.md` (stub), `cmd/skybridge/main.go`, `internal/version/version.go`
- Test: `internal/version/version_test.go`

**Interfaces:**
- Produces: `package version` with `var Version, Commit, Date string` (defaults `"dev"`, `"none"`, `"unknown"`), `type Info struct { Version, Commit, Date, Go, OS, Arch string }` with json tags in lower case, `func Get() Info`, `func (i Info) String() string` formatted as `skybridge <Version> (<Commit>, <Date>) <Go> <OS>/<Arch>`.

- [ ] **Step 1: Initialise the repository**

```bash
cd /Users/benton/code/skysafari
git init -b main
git remote add origin git@git.h.usky.io:huskynet/skybridge.git
go mod init git.h.usky.io/huskynet/skybridge
```

`.gitignore` contains: `/dist/`, `/skybridge`, `/skybridge.exe`, `*.pyc`, `__pycache__/`, `.DS_Store`, `/tmp/`.
`LICENSE` is the MIT text with `Copyright (c) 2026 Benton Roberts`.
`README.md` is one heading and one sentence for now; Task 14 writes the real one.

- [ ] **Step 2: Write the failing test**

```go
func TestGetDefaults(t *testing.T) {
	i := Get()
	if i.Version != "dev" || i.Commit != "none" || i.Date != "unknown" { t.Fatalf("defaults: %+v", i) }
	if i.Go != runtime.Version() || i.OS != runtime.GOOS || i.Arch != runtime.GOARCH { t.Fatalf("runtime: %+v", i) }
}

func TestString(t *testing.T) {
	s := Info{Version: "v1.2.3", Commit: "abc1234", Date: "2026-10-27", Go: "go1.26.6", OS: "linux", Arch: "arm64"}.String()
	if s != "skybridge v1.2.3 (abc1234, 2026-10-27) go1.26.6 linux/arm64" { t.Fatal(s) }
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/version/`
Expected: FAIL, `undefined: Get`

- [ ] **Step 4: Implement `internal/version/version.go`**

`cmd/skybridge/main.go` for now prints `version.Get().String()` and exits 0; Task 2 replaces it.

- [ ] **Step 5: Run tests and build**

Run: `go test ./... && go build ./cmd/skybridge && ./skybridge`
Expected: PASS, prints `skybridge dev (none, unknown) go1.26.6 darwin/arm64`

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore LICENSE README.md cmd internal docs bridge.py scope-bridge.sh .claude
git commit -m "chore: bootstrap skybridge module with version package"
```

---

### Task 2: CLI skeleton with `version` and `help`

**Files:**
- Create: `internal/cli/cli.go`, `internal/cli/flags.go`, `internal/cli/version.go`
- Modify: `cmd/skybridge/main.go`
- Test: `internal/cli/cli_test.go`, `internal/cli/flags_test.go`

**Interfaces:**
- Consumes: `version.Get()`.
- Produces: `type Env func(key string) string`; `func Main(args []string, stdout, stderr io.Writer, env Env) int`; constants `ExitOK = 0`, `ExitUsage = 1`, `ExitListen = 2`, `ExitProbe = 3`; internal `type command struct { Name, Summary string; Run func(ctx context.Context, s *stdio, args []string) int }` and `type stdio struct { Out, Err io.Writer; Env Env }`; `func applyEnv(fs *flag.FlagSet, env Env)` that, after `fs.Parse`, sets every flag the user did not pass from `SKYBRIDGE_<NAME>` when that variable is non-empty; `func newLogger(w io.Writer, level, format string) (*slog.Logger, error)` accepting levels `debug|info|warn|error` and formats `text|json`.

- [ ] **Step 1: Write the failing tests**

```go
func TestVersionCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Main([]string{"version"}, &out, &errb, func(string) string { return "" }); code != 0 { t.Fatal(code) }
	if !strings.HasPrefix(out.String(), "skybridge dev") { t.Fatal(out.String()) }
}

func TestVersionJSON(t *testing.T) {
	var out bytes.Buffer
	Main([]string{"version", "--json"}, &out, io.Discard, noEnv)
	var v map[string]string
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v["version"] != "dev" { t.Fatal(out.String(), err) }
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var errb bytes.Buffer
	if code := Main([]string{"bogus"}, io.Discard, &errb, noEnv); code != ExitUsage { t.Fatal(code) }
	if !strings.Contains(errb.String(), "unknown command \"bogus\"") { t.Fatal(errb.String()) }
}

func TestHelpListsCommands(t *testing.T) // "help" and "--help" exit 0 and list serve, list-ports, probe, mounts, version

func TestApplyEnvPrecedence(t *testing.T) {
	// flag --listen given: env ignored; flag absent: env used; both absent: default
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:4030", "")
	fs.Parse([]string{"--listen", "0.0.0.0:1"})
	applyEnv(fs, func(k string) string { if k == "SKYBRIDGE_LISTEN" { return "9.9.9.9:9" }; return "" })
	if *listen != "0.0.0.0:1" { t.Fatal(*listen) }
	// second FlagSet with no args → "9.9.9.9:9"; third with empty env → default
}

func TestNewLoggerRejectsBadLevel(t *testing.T) // level "loud" → error mentioning "loud"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/`
Expected: FAIL, `undefined: Main`

- [ ] **Step 3: Implement the dispatcher**

`Main` maps `args[0]` to a command; empty args means `serve` (registered in Task 11; until then `Main` with no args prints help and returns `ExitUsage`). `version` prints `Info.String()` or, with `--json`, `json.MarshalIndent(Info)`. `help` output format: `usage: skybridge <command> [flags]` then one aligned line per command. Root context comes from `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`. `main.go` becomes `os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))`.

- [ ] **Step 4: Run tests**

Run: `go test -race ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd internal/cli
git commit -m "feat(cli): add command dispatcher with version and help"
```

---

### Task 3: Relay core

**Files:**
- Create: `internal/bridge/bridge.go`
- Test: `internal/bridge/bridge_test.go`

**Interfaces:**
- Produces: `type Opener func(ctx context.Context) (io.ReadWriteCloser, error)` (blocks until a port is open, or returns `ctx.Err()`); `type Flusher interface { Flush() error }`; `type Config struct { Listener net.Listener; Open Opener; Logger *slog.Logger; RetryDelay time.Duration }`; `func Run(ctx context.Context, cfg Config) error`.
- Contract of `Run`: accepts on `Listener` for its whole life and closes it before returning. Returns `nil` when `ctx` is done, or the error from a non-temporary `Accept` failure. While `Open` is in progress, every accepted connection is closed immediately. While a port is open, exactly one client is served; a new connection closes the previous one and calls `Flush` on the port if it implements `Flusher`. Bytes copy verbatim both ways. Serial bytes with no client are read and dropped. A client error drops only the client. A serial read or write error closes the port, logs one `WARN serial port lost`, waits `RetryDelay`, and calls `Open` again. `TCP_NODELAY` is set on every `*net.TCPConn`.

- [ ] **Step 1: Write the failing tests**

Test helpers: `fakePort` wraps one end of `net.Pipe()` and counts `Flush` calls; `startRun(t, opener) (addr string, done <-chan error, cancel func())` listens on `127.0.0.1:0`, runs `Run` in a goroutine with `RetryDelay: 10*time.Millisecond` and `slog.New(slog.NewTextHandler(io.Discard, nil))`.

```go
func TestRefusesWhileAbsent(t *testing.T) {
	// Opener blocks on ctx. Dial; Read must return io.EOF within 1s.
}
func TestRelaysBothDirections(t *testing.T) {
	// client writes "Kx"; mount end reads "Kx", writes "x#"; client reads "x#".
}
func TestNewClientReplacesOld(t *testing.T) {
	// c1 connects and exchanges a byte; c2 connects; c1.Read returns error within 1s;
	// fakePort.flushes == 1 after c2 (first connect also flushes → expect 2 total; assert exact).
	// c2 still relays.
}
func TestClientDropKeepsPort(t *testing.T) {
	// c1 closes; Opener call count stays 1; c2 connects and relays.
}
func TestSerialLossReopens(t *testing.T) {
	// mount end closes its pipe; Opener is called a second time (count reaches 2 within 1s);
	// new client relays through the second port.
}
func TestSerialDataWithoutClientIsDropped(t *testing.T) {
	// mount writes "stale" with no client (must not block: write completes within 1s);
	// client connects, mount writes "fresh"; client reads exactly "fresh".
}
func TestCancelStopsRunAndClosesListener(t *testing.T) {
	// cancel; done receives nil within 1s; Dial(addr) fails.
}
func TestManyReconnectsDoNotLeak(t *testing.T) {
	// 200 sequential dial+write+close cycles; afterwards runtime.NumGoroutine() <= baseline+4
	// (after 200ms settle); a final client still relays.
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/bridge/`
Expected: FAIL, `undefined: Run`

- [ ] **Step 3: Implement `Run`**

One accept goroutine feeds a `chan net.Conn`. The main loop alternates between `awaitPort` (drains and closes conns while `Open` runs in a goroutine) and `relay(ctx, port, conns)`. `relay` runs one goroutine reading the port into a `chan []byte` and one per current client reading into a `chan []byte`; the select loop owns the "current client" variable so there is no locking. Read buffers 4096 bytes.

- [ ] **Step 4: Run tests**

Run: `go test -race -count=3 ./internal/bridge/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bridge
git commit -m "feat(bridge): add serial-to-tcp relay core"
```

---

### Task 4: Mount profiles and probes

**Files:**
- Create: `internal/mounts/profiles.go`, `internal/mounts/probe.go`
- Test: `internal/mounts/profiles_test.go`, `internal/mounts/probe_test.go`

**Interfaces:**
- Produces: `const DefaultName = "celestron-nexstar"`; `type Probe struct { Send string; Terminator byte; Length int; Timeout time.Duration; Match func(reply string) bool }`; `type Profile struct { Name string; Aliases []string; Baud int; Covers string; Notes string; Probe *Probe }` (json tags: name, aliases, baud, covers, notes; Probe is `json:"-"`); `func All() []Profile` in table order; `func Lookup(name string) (Profile, bool)` case-insensitive on name and aliases; `var ErrNoReply, ErrUnexpectedReply, ErrUnidentified error`; `func (p *Probe) Run(ctx context.Context, rw io.ReadWriter) (string, error)`; `type Opener func(ctx context.Context, baud int) (io.ReadWriteCloser, error)`; `func Identify(ctx context.Context, open Opener, log *slog.Logger) (Profile, string, error)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestTableOrderAndDefaults(t *testing.T) {
	names := []string{"celestron-nexstar", "skywatcher-synscan", "losmandy-gemini", "meade-lx200", "ioptron", "ioptron-115200", "generic"}
	// All() names equal this slice; Lookup(DefaultName) ok; every non-generic Baud > 0; generic Baud == 0 and Probe == nil.
}
func TestLookupAliasesCaseInsensitive(t *testing.T) {
	// "NexStar"→celestron-nexstar, "synscan"→skywatcher-synscan, "LX200"→meade-lx200,
	// "autostar"→meade-lx200, "gemini"→losmandy-gemini, "nope"→!ok
}
func TestProbeRunMatches(t *testing.T) {
	// nexstar probe over net.Pipe: responder reads "Kx", writes "x#"; Run returns "x#", nil.
}
func TestProbeRunFixedLength(t *testing.T) {
	// ioptron probe: responder replies "0030" with no terminator; Run returns "0030".
}
func TestProbeRunNoReply(t *testing.T) {
	// responder never answers; Probe.Timeout = 50ms; errors.Is(err, ErrNoReply).
}
func TestProbeRunMismatch(t *testing.T) {
	// nexstar probe gets "zz#"; errors.Is(err, ErrUnexpectedReply); reply still returned.
}
func TestIdentifyStopsAtFirstMatch(t *testing.T) {
	// fake opener records bauds; responder answers only ":GVP#" with "Losmandy Gemini#";
	// Identify returns losmandy-gemini, reply "Losmandy Gemini#"; opener saw bauds [9600,9600,9600] (nexstar, synscan, gemini).
}
func TestIdentifyNothingAnswers(t *testing.T) // errors.Is(err, ErrUnidentified); generic never probed
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mounts/`
Expected: FAIL, `undefined: All`

- [ ] **Step 3: Implement the table and probes**

Table values (Timeout 1 s for every probe):

| Name | Aliases | Baud | Probe |
|---|---|---|---|
| celestron-nexstar | nexstar, celestron | 9600 | Send `Kx`, Terminator `#`, Match reply == `x#` |
| skywatcher-synscan | synscan, skywatcher | 9600 | same as nexstar |
| losmandy-gemini | gemini, losmandy | 9600 | Send `:GVP#`, Terminator `#`, Match contains `Gemini` |
| meade-lx200 | lx200, meade, autostar | 9600 | Send `:GVP#`, Terminator `#`, Match len(reply) > 1 |
| ioptron | ioptron-9600 | 9600 | Send `:MountInfo#`, Length 4, Match four ASCII digits |
| ioptron-115200 | | 115200 | same as ioptron |
| generic | | 0 | nil |

`Covers` text comes from the spec's profile table; `Notes` for synscan says SynScan speaks the NexStar dialect so `probe` reports it as celestron-nexstar. `Probe.Run` writes `Send`, then reads in a goroutine until `Terminator` is seen or `Length` bytes arrived, racing a `time.After(Timeout)`; the reader goroutine is abandoned on timeout, so callers must close the port afterwards (document on the method). `Identify` iterates `All()`, skips nil probes, opens at each profile's baud, runs, closes, and returns on the first nil error; logs one `DEBUG` line per attempt.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/mounts/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mounts
git commit -m "feat(mounts): add mount profile table and identification probes"
```

---

### Task 5: Serial port on macOS and Linux

**Files:**
- Create: `internal/serial/serial.go`, `internal/serial/port_unix.go` (`//go:build darwin || linux`), `internal/serial/speed_linux.go`, `internal/serial/speed_darwin.go`
- Test: `internal/serial/serial_test.go`, `internal/serial/port_unix_test.go`

**Interfaces:**
- Produces: `type Config struct { Baud int }`; `type Port interface { io.ReadWriteCloser; Flush() error }`; `var ErrNotFound, ErrClosed, ErrUnsupportedBaud error`; `var SupportedBauds = []int{4800, 9600, 19200, 38400, 57600, 115200}`; `func Open(path string, cfg Config) (Port, error)`; unexported `func configure(fd int, baud int) error` and `func speedOf(t *unix.Termios) int` in `port_unix.go` for the tests.
- Contract: `Open` returns an error wrapping `ErrNotFound` when the path does not exist and `ErrUnsupportedBaud` before touching the device for a baud outside `SupportedBauds`. `Read` blocks until at least one byte or an error. After `Close`, any blocked `Read`/`Write` returns within 500 ms with an error wrapping `ErrClosed`, and so does every later call. Device removal surfaces as a non-nil read error. `Flush` discards both OS buffers.

- [ ] **Step 1: Write the failing tests**

```go
// serial_test.go (all platforms)
func TestOpenMissingPath(t *testing.T) // Open("/nonexistent/tty", Config{Baud: 9600}) → errors.Is(err, ErrNotFound)
func TestOpenUnsupportedBaud(t *testing.T) // Open("/nonexistent/tty", Config{Baud: 1200}) → errors.Is(err, ErrUnsupportedBaud) (checked before the path)

// port_unix_test.go (//go:build linux) — skips unless `socat` is on PATH
func TestPtyRoundTrip(t *testing.T) {
	// socat -d -d pty,raw,echo=0,link=$TMP/a pty,raw,echo=0,link=$TMP/b; wait for both links.
	// Open(a, 9600); os.OpenFile(b); write "Kx" to a → read "Kx" from b; write "x#" to b → Read from a returns "x#".
}
func TestPtyBaudApplied(t *testing.T) {
	// open a raw fd on link a; configure(fd, 19200); read termios back with unix.IoctlGetTermios; speedOf(&termios) == 19200
}
func TestCloseUnblocksRead(t *testing.T) // goroutine blocked in Read; Close; Read returns errors.Is(ErrClosed) within 500ms
func TestReadAfterCloseFails(t *testing.T)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/serial/`
Expected: FAIL, `undefined: Open`

- [ ] **Step 3: Implement the Unix port**

`port_unix.go`: `unix.Open(path, O_RDWR|O_NOCTTY|O_NONBLOCK|O_CLOEXEC, 0)`; `configure` sets raw mode (`Iflag`, `Oflag`, `Lflag` zero; `Cflag = CS8|CREAD|CLOCAL`; `Cc[VMIN]=1`, `Cc[VTIME]=0`) and calls the per-OS `setSpeed(*unix.Termios, int) error`; clear `O_NONBLOCK` with `fcntl` afterwards. `Read` loops on `unix.Poll` with a 250 ms timeout, checking a `closed` atomic between slices, then `unix.Read`. `Close` sets the flag, then `unix.Close`. `Flush` is `unix.IoctlSetInt(fd, TCFLSH/TIOCFLUSH, TCIOFLUSH)` per OS. `speed_linux.go` maps bauds to `unix.B9600` etc. into `Cflag` (`&^ unix.CBAUD`) and `Ispeed/Ospeed`; `speed_darwin.go` sets `Ispeed/Ospeed` to the number directly. Ioctl request constants: Linux `TCGETS/TCSETS`, Darwin `TIOCGETA/TIOCSETA`.

- [ ] **Step 4: Run tests on macOS, then on Linux**

Run: `go test -race ./internal/serial/` (macOS: pty tests skip with message `socat not found` or build-tag exclusion)
Run on Linux (CI or a container with socat): same command.
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/serial go.mod go.sum
git commit -m "feat(serial): add unix serial port with termios configuration"
```

---

### Task 6: Serial port on Windows

**Files:**
- Create: `internal/serial/port_windows.go`
- Test: `internal/serial/port_windows_test.go`

**Interfaces:**
- Consumes/Produces: the same `Open`, `Port`, errors as Task 5, on `windows`.

- [ ] **Step 1: Write the failing test**

```go
//go:build windows
func TestOpenMissingCOM(t *testing.T) // Open("COM255", Config{Baud: 9600}) → errors.Is(err, ErrNotFound)
func TestDevicePath(t *testing.T) // devicePath("COM3") == `\\.\COM3`; devicePath(`\\.\COM3`) unchanged
```

- [ ] **Step 2: Verify it does not compile on Windows**

Run: `GOOS=windows go vet ./internal/serial/`
Expected: `undefined: Open` for windows

- [ ] **Step 3: Implement the Windows port**

`windows.CreateFile(devicePath, GENERIC_READ|GENERIC_WRITE, 0, nil, OPEN_EXISTING, 0, 0)`; map `ERROR_FILE_NOT_FOUND` to `ErrNotFound`. `windows.GetCommState` → set `BaudRate`, `ByteSize=8`, `Parity=NOPARITY`, `StopBits=ONESTOPBIT`, flow-control bits off → `SetCommState`. `SetCommTimeouts` with `ReadIntervalTimeout=MAXDWORD`, `ReadTotalTimeoutMultiplier=MAXDWORD`, `ReadTotalTimeoutConstant=250`, write timeouts 0, so `ReadFile` returns every 250 ms with zero bytes when idle and the loop can check the closed flag. `Flush` is `PurgeComm(PURGE_RXCLEAR|PURGE_TXCLEAR)`. `Close` sets the flag then `CloseHandle`.

- [ ] **Step 4: Verify cross-compile and vet**

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./... && GOOS=windows go vet ./internal/serial/`
Expected: no output. The two tests run for real in Task 16's Windows CI job.

- [ ] **Step 5: Commit**

```bash
git add internal/serial/port_windows.go internal/serial/port_windows_test.go
git commit -m "feat(serial): add windows serial port"
```

---

### Task 7: Serial port enumeration

**Files:**
- Create: `internal/serial/list.go`, `internal/serial/usbid.go`, `internal/serial/list_linux.go`, `internal/serial/list_darwin.go`, `internal/serial/list_windows.go`, `internal/serial/testdata/ioreg-nexstar.txt`
- Test: `internal/serial/usbid_test.go`, `internal/serial/list_linux_test.go`, `internal/serial/list_darwin_test.go`

**Interfaces:**
- Produces: `type PortInfo struct { Path string; VID, PID uint16; Product string }` (json: path, vid, pid, product with omitempty on product); `func List(ctx context.Context) ([]PortInfo, error)` sorted by Path; unexported `parseUSBInstanceID(s string) (vid, pid uint16, ok bool)` in `usbid.go` (no build tag) parsing `USB\VID_067B&PID_23A3\...` case-insensitively; unexported `parseIOReg(r io.Reader) map[string]PortInfo` in `list_darwin.go` keyed by callout path; unexported `listLinux(devDir, sysClassTTY string) ([]PortInfo, error)`.

- [ ] **Step 1: Capture the macOS fixture**

With the hand controller plugged in, run `ioreg -c IOSerialBSDClient -l -w0 > internal/serial/testdata/ioreg-nexstar.txt`. If the controller is not available, the implementer leaves a note in the task and the owner supplies the file before the test is enabled.

- [ ] **Step 2: Write the failing tests**

```go
func TestParseUSBInstanceID(t *testing.T) // table: `USB\VID_067B&PID_23A3\6&1a2b` → 0x067B,0x23A3,true; `usb\vid_0403&pid_6001\x` ok; `ACPI\PNP0501\1` → !ok
func TestParseIORegFixture(t *testing.T) // map contains "/dev/cu.PL2303G-USBtoUART110" (adjust to the fixture's actual path) with VID 0x067B
func TestListLinuxFakeSysfs(t *testing.T) {
	// TempDir with dev/ttyUSB0, dev/ttyACM0, dev/ttyS0 (ignored) and sys/class/tty/ttyUSB0/device/../idVendor="067b", idProduct="23a3", product="USB-Serial Controller";
	// listLinux returns two entries; ttyUSB0 has VID 0x067B, Product set; ttyACM0 has VID 0.
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/serial/`
Expected: FAIL, `undefined: parseUSBInstanceID`

- [ ] **Step 4: Implement enumeration**

Linux: glob `ttyUSB*` and `ttyACM*` under `devDir`; for each, resolve `sysClassTTY/<name>/device` and walk up at most four parent directories looking for `idVendor`; read `idVendor`, `idProduct`, `product`. Darwin: glob `/dev/cu.*`, drop names containing `Bluetooth`; run `ioreg -c IOSerialBSDClient -l -w0` with `exec.CommandContext` (the caller's ctx, 1 s), feed `parseIOReg`, which tracks tree depth from the leading `|`/space prefix and the `+-o` marker, keeps the most recent `"idVendor"`/`"idProduct"`/`"USB Product Name"` seen at each depth, and assigns the nearest ancestor's values when it meets `"IOCalloutDevice"`. A failed `ioreg` still returns the paths with zero VID/PID. Windows: `SetupDiGetClassDevsEx(&GUID_DEVINTERFACE_COMPORT, DIGCF_PRESENT|DIGCF_DEVICEINTERFACE)`, enumerate with `SetupDiEnumDeviceInfo`, read `PortName` via `SetupDiOpenDevRegKey`, instance ID via `SetupDiGetDeviceInstanceId` → `parseUSBInstanceID`, friendly name via `SetupDiGetDeviceRegistryProperty(SPDRP_FRIENDLYNAME)`.

- [ ] **Step 5: Run tests and cross-compile**

Run: `go test -race ./internal/serial/ && for os in linux windows; do GOOS=$os CGO_ENABLED=0 go build ./...; done`
Expected: PASS, no build output

- [ ] **Step 6: Commit**

```bash
git add internal/serial
git commit -m "feat(serial): enumerate serial ports with usb ids on all platforms"
```

---

### Task 8: Adapter discovery and ranking

**Files:**
- Create: `internal/discovery/discovery.go`
- Test: `internal/discovery/discovery_test.go`

**Interfaces:**
- Consumes: `serial.PortInfo`, `serial.List`.
- Produces: `type Adapter struct { Name string; VID uint16 }`; `var KnownAdapters = []Adapter{{"Prolific PL2303", 0x067B}, {"FTDI", 0x0403}, {"Silicon Labs CP210x", 0x10C4}, {"WCH CH340/CH341", 0x1A86}}`; `type Candidate struct { serial.PortInfo; Adapter string; Score int }` (json: adapter omitempty, score); `func Rank(goos string, ports []serial.PortInfo) []Candidate` returning every port scored, sorted by Score descending then Path; `func Find(ctx context.Context) ([]Candidate, error)` = `serial.List` + `Rank(runtime.GOOS)` filtered to Score > 0, with a 1 s timeout applied to ctx.
- Scores: VID in `KnownAdapters` → 100 and `Adapter` set. Otherwise name pattern: darwin `/dev/cu.` prefix containing one of `usbserial`, `PL2303`, `SLAB_USBtoUART`, `wchusbserial`, `usbmodem` → 50; linux `/dev/ttyUSB` → 50, `/dev/ttyACM` → 40; windows any `COM` → 50. Everything else 0.

- [ ] **Step 1: Write the failing tests**

```go
func TestRankVIDBeatsName(t *testing.T) // darwin: {cu.usbserial-1 VID 0} vs {cu.PL2303G VID 0x067B} → PL2303 first, Score 100, Adapter "Prolific PL2303"
func TestRankDarwinPatterns(t *testing.T) // cu.Bluetooth-Incoming-Port → 0; cu.SLAB_USBtoUART → 50; cu.usbmodem1101 → 50
func TestRankLinuxPatterns(t *testing.T) // ttyUSB0 → 50, ttyACM0 → 40, ttyS0 → 0; order ttyUSB0, ttyACM0, ttyS0
func TestRankWindows(t *testing.T) // COM3 VID 0x10C4 → 100 "Silicon Labs CP210x"; COM1 → 50
func TestRankStableOrder(t *testing.T) // equal scores sort by Path
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/discovery/`
Expected: FAIL, `undefined: Rank`

- [ ] **Step 3: Implement `discovery.go`**

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/discovery/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/discovery
git commit -m "feat(discovery): rank serial ports by known usb adapter and name"
```

---

### Task 9: `list-ports` and `mounts` commands

**Files:**
- Create: `internal/cli/listports.go`, `internal/cli/mounts.go`
- Modify: `internal/cli/cli.go` (register commands)
- Test: `internal/cli/listports_test.go`, `internal/cli/mounts_test.go`

**Interfaces:**
- Consumes: `discovery.Rank`, `serial.List`, `mounts.All`.
- Produces: package var `listPorts = serial.List` (test hook); `list-ports` text output is a table with header `PATH  ADAPTER  VID:PID  PRODUCT` using `text/tabwriter`, VID:PID as `067b:23a3` or `-` when zero, every port listed, ranked first; `--json` prints `[]discovery.Candidate`. `mounts` prints `NAME  BAUD  PROBE  COVERS` (PROBE is `yes`/`no`; generic shows `--baud` in BAUD) or `--json` with fields name, aliases, baud, probe (bool), covers, notes.

- [ ] **Step 1: Write the failing tests**

```go
func TestListPortsTable(t *testing.T) {
	listPorts = func(context.Context) ([]serial.PortInfo, error) { return []serial.PortInfo{{Path: "/dev/cu.Bluetooth-Incoming-Port"}, {Path: "/dev/cu.PL2303G-USBtoUART110", VID: 0x067B, PID: 0x23A3, Product: "USB-Serial Controller"}}, nil }
	// output line 2 starts with "/dev/cu.PL2303G" and contains "Prolific PL2303" and "067b:23a3"; Bluetooth line comes last with "-"
}
func TestListPortsJSON(t *testing.T) // unmarshal into []discovery.Candidate; len 2; [0].Score == 100
func TestListPortsNone(t *testing.T) // empty list → stderr "no serial ports found", exit 0
func TestMountsTable(t *testing.T) // first data row starts with "celestron-nexstar", contains "9600" and "yes"; generic row contains "--baud" and "no"
func TestMountsJSON(t *testing.T) // 7 entries; [0].probe == true; [6].probe == false
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/`
Expected: FAIL, `undefined: listPorts`

- [ ] **Step 3: Implement both commands and register them**

- [ ] **Step 4: Run tests and try it live**

Run: `go test -race ./internal/cli/ && go run ./cmd/skybridge list-ports && go run ./cmd/skybridge mounts`
Expected: PASS; the live table shows the Prolific port when the controller is plugged in.

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): add list-ports and mounts commands"
```

---

### Task 10: `probe` command

**Files:**
- Create: `internal/cli/probe.go`
- Modify: `internal/cli/cli.go`
- Test: `internal/cli/probe_test.go`

**Interfaces:**
- Consumes: `mounts.Lookup`, `mounts.Identify`, `(*mounts.Probe).Run`, `discovery.Find`, `serial.Open`.
- Produces: package vars `findPorts = discovery.Find` and `openPort = serial.Open` (test hooks, shared with Task 11; test fakes returned by `openPort` must implement `serial.Port`, so wrap the pipe end in a struct with a no-op `Flush`); package var `rootCtx context.Context` which `Main` uses as the root when non-nil instead of the signal context; `func parseBaud(s string) (int, error)` returning an error that lists `serial.SupportedBauds` when the value is not one of them; flags `--port`, `--mount`, `--baud`, `--json`, `--log-level`, `--log-format`. Text output: `celestron-nexstar at /dev/cu.X (9600 baud): reply "x#"`. JSON: `{"profile","path","baud","reply"}`. Exit 1 for unknown `--mount`, bad `--baud`, or explicit `--port` that does not exist; exit 3 when no candidate port exists or nothing answers.

- [ ] **Step 1: Write the failing tests**

```go
func TestProbeWithMount(t *testing.T) {
	// findPorts → one candidate "/dev/fake"; openPort → net.Pipe end with responder answering "Kx" with "x#";
	// Main({"probe","--mount","nexstar"}) exit 0; stdout == `celestron-nexstar at /dev/fake (9600 baud): reply "x#"` + "\n"
}
func TestProbeIdentifies(t *testing.T) // no --mount; responder answers only ":GVP#" with "Losmandy Gemini#"; stdout starts "losmandy-gemini at"
func TestProbeJSON(t *testing.T)
func TestProbeNoAnswerExit3(t *testing.T) // silent responder, --mount nexstar; exit ExitProbe; stderr contains "no reply"
func TestProbeNoPortsExit3(t *testing.T)
func TestProbeUnknownMountExit1(t *testing.T) // stderr contains `unknown mount profile "zzz"` and "run 'skybridge mounts'"
func TestProbeBadBaudExit1(t *testing.T) // --baud 1200 → stderr contains "4800, 9600, 19200, 38400, 57600, 115200"; openPort never called
func TestProbeExplicitMissingPortExit1(t *testing.T) // --port /nope with openPort returning serial.ErrNotFound → exit 1
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/`
Expected: FAIL, `undefined: findPorts`

- [ ] **Step 3: Implement `probe`**

`--baud` overrides the profile's baud. With `--mount` the command opens once and runs that probe. Without, it builds a `mounts.Opener` from `openPort` and calls `Identify`. Always close the port after probing.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): add probe command with mount identification"
```

---

### Task 11: `serve` command and port opener

**Files:**
- Create: `internal/cli/serve.go`, `internal/cli/opener.go`
- Modify: `internal/cli/cli.go` (register; bare invocation runs `serve`)
- Test: `internal/cli/serve_test.go`, `internal/cli/opener_test.go`

**Interfaces:**
- Consumes: `bridge.Run`, `discovery.Find`, `serial.Open`, `mounts.Lookup`, `findPorts`/`openPort` hooks from Task 10.
- Produces: `type serveOptions struct { Port string; Profile mounts.Profile; Baud int; Verify bool; Retry time.Duration }`; `func newOpener(o serveOptions, log *slog.Logger) bridge.Opener`; package var `listen = net.Listen` (test hook); flags `--port`, `--mount` (default `celestron-nexstar`), `--baud`, `--listen` (default `127.0.0.1:4030`), `--verify`, `--log-level` (default `info`), `--log-format` (default `text`), all with env twins.
- Opener contract: loop until ctx done. Explicit `Port` → try it; otherwise `findPorts` and take the first candidate, logging the others once at `INFO` as `other candidates`. Log `waiting for serial port` once when entering the absent state, not every retry. On `serial.ErrNotFound` or any open error: sleep `Retry`, continue. If `Verify`: run `Profile.Probe.Run`; on failure log `WARN probe failed`, close, sleep, continue. On success log `INFO serial port opened path= baud=` and return. The generic profile with `Baud == 0` and no `--baud` is a usage error at flag time.

- [ ] **Step 1: Write the failing tests**

```go
// opener_test.go
func TestOpenerWaitsForPort(t *testing.T) // findPorts returns nothing twice then one candidate; openPort succeeds; Opener returns after ~2 retries (Retry 5ms)
func TestOpenerNotFoundRaceRetries(t *testing.T) // openPort returns serial.ErrNotFound first, then a pipe; Opener returns the pipe; no error
func TestOpenerVerifyRetriesOnProbeFailure(t *testing.T) // first port never answers, second answers "x#"; Opener returns second; first was closed
func TestOpenerReturnsOnCancel(t *testing.T) // findPorts always empty; cancel → errors.Is(err, context.Canceled)
func TestOpenerExplicitPortSkipsDiscovery(t *testing.T) // findPorts would t.Fatal if called

// serve_test.go
func TestServeEndToEnd(t *testing.T) {
	// listen hook captures the listener; openPort returns a fake serial.Port (pipe end plus no-op Flush) with a NexStar responder on the far end;
	// set the package var rootCtx to a cancellable context; run Main({"serve","--listen","127.0.0.1:0"}) in a goroutine;
	// dial the captured Addr; write "Kx"; read "x#"; cancel rootCtx; Main returns 0 within 1s.
}
func TestServeBadBaudExit1(t *testing.T) // --baud abc → exit 1, message lists supported rates, listen never called
func TestServeListenInUseExit2(t *testing.T) // pre-bind a port; --listen that addr → exit 2; stderr contains "address already in use" (or the OS equivalent string, assert on "listen")
func TestServeEnvListen(t *testing.T) // env SKYBRIDGE_LISTEN=127.0.0.1:0 and no flag → listen hook sees that address
func TestBareInvocationIsServe(t *testing.T) // Main(nil, ...) with listen hook → hook called with "127.0.0.1:4030"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/`
Expected: FAIL, `undefined: newOpener`

- [ ] **Step 3: Implement `serve` and the opener**

`serve` builds the logger, parses and validates flags, calls `listen("tcp", addr)` (exit 2 with `listen: <err>` on failure), logs `INFO listening addr=`, and calls `bridge.Run` with `RetryDelay: 2 * time.Second`. Return 0 when `Run` returns nil; log and return 2 otherwise.

- [ ] **Step 4: Run tests, then run it for real against the hand controller**

Run: `go test -race ./... && go run ./cmd/skybridge serve --log-level debug`
Expected: PASS; log shows `listening`, then `serial port opened`; connecting SkySafari to 127.0.0.1:4030 moves the scope. Unplug the cable: `serial port lost`; replug: `serial port opened`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): add serve command with hot-plug port opener"
```

---

### Task 12: `tools/check` developer gate

**Files:**
- Create: `tools/check/main.go`
- Test: manual run

**Interfaces:**
- Produces: `go run ./tools/check [fmt|vet|static|vuln|tidy|test|cross|all]` (default `all`). Each step prints `==> <name>` then runs: `gofmt -l .` (fails if any file listed), `go vet ./...`, `staticcheck ./...`, `govulncheck ./...`, `go mod tidy` followed by `git diff --exit-code go.mod go.sum`, `go test -race ./...`, and `cross` building every target from Global Constraints with `CGO_ENABLED=0` into `dist/check/<os>-<arch>`. `static` and `vuln` install their tool with `go install <path>@<pinned version>` into `GOBIN` under the module cache if missing. Exit 1 on the first failure, naming the step.

- [ ] **Step 1: Implement `tools/check/main.go`**

Pin `honnef.co/go/tools/cmd/staticcheck` and `golang.org/x/vuln/cmd/govulncheck` to the newest release at implementation time; record both versions in a `const` block.

- [ ] **Step 2: Run it**

Run: `go run ./tools/check`
Expected: every step prints and the run ends with `all checks passed`. Fix any staticcheck or vet finding in the packages it reports.

- [ ] **Step 3: Commit**

```bash
git add tools
git commit -m "build: add go-based check tool for format, vet, static analysis and cross builds"
```

---

### Task 13: Release configuration

**Files:**
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Write `.goreleaser.yaml` (v2)**

Contents it must fix: `builds` entry `main: ./cmd/skybridge`, `binary: skybridge`, `env: [CGO_ENABLED=0]`, the full `goos`/`goarch` matrix with `goarm: ["6"]` and `ignore` for `windows/arm` and `darwin/arm`; `ldflags: -s -w -X git.h.usky.io/huskynet/skybridge/internal/version.Version={{.Version}} -X ...Commit={{.ShortCommit}} -X ...Date={{.Date}}`; `archives` with `name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`, `format: tar.gz`, `format_overrides` zip for windows, including `LICENSE`, `README.md`, `docs/*.md`; `checksum: { name_template: checksums.txt }`; `changelog` grouped by conventional-commit type (`feat`, `fix`, others) excluding `docs`, `test`, `chore`; `gitlab_urls: { api: https://git.h.usky.io/api/v4/, download: https://git.h.usky.io }`. GoReleaser selects GitHub or GitLab from whichever token is present, so one file serves both pipelines. Assumption recorded in `docs/development.md`: `git.h.usky.io` is a GitLab instance; if it is Gitea, replace `gitlab_urls` with `gitea_urls`.

- [ ] **Step 2: Verify locally**

Run: `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish`
Expected: `dist/` contains seven archives and `checksums.txt`; `tar tzf dist/skybridge_*_darwin_arm64.tar.gz` lists `skybridge`, `LICENSE`, `README.md`. `dist/skybridge_darwin_arm64*/skybridge version` prints the snapshot version.

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "build: add goreleaser configuration for all platforms"
```

---

### Task 14: User documentation

**Files:**
- Create: `README.md` (replace stub), `docs/install.md`, `docs/skysafari.md`, `docs/hardware.md`, `docs/troubleshooting.md`, `CHANGELOG.md`

- [ ] **Step 1: Write the documents**

Each document's required content:

- `README.md`: one-paragraph description; "Install" (download the archive for your OS from the releases page, unpack, run `skybridge`); "Connect SkySafari" in four steps (Settings → Telescope → Setup; Scope Type matching your mount; Connect via Wi-Fi; IP `127.0.0.1` when SkySafari runs on the same machine or the computer's LAN address with `--listen 0.0.0.0:4030` otherwise; port `4030`); a table of the five commands; links to the other docs; MIT notice.
- `docs/install.md`: per OS. macOS: `xattr -d com.apple.quarantine skybridge` for the Gatekeeper warning, note that Prolific and CP210x adapters need no driver on macOS 11+. Linux: `sudo usermod -aG dialout $USER` then log out and in, `/dev/ttyUSB0` naming, ModemManager grabbing the port and how to blacklist it. Windows: driver pages for Prolific, FTDI, CP210x and CH340, that the port is `COM<n>` in Device Manager, Windows Defender SmartScreen "Run anyway". A "run at login" section with a launchd plist, a systemd user unit, and a Windows Task Scheduler note, each using `SKYBRIDGE_*` environment variables rather than flags.
- `docs/skysafari.md`: SkySafari 6 and 7 settings side by side, with the exact Scope Type names for each profile (Celestron NexStar/Advanced GT, Celestron AVX/CGEM/CGX, SkyWatcher SynScan, Meade LX200 Classic/GPS, Meade Autostar, iOptron CEM/GEM, Losmandy Gemini), Mount Type (Alt-Az GoTo or Equatorial GoTo), and "Set Time & Location" guidance. Paraphrase; never quote the manuals.
- `docs/hardware.md`: supported mounts table copied from the profile table with a "Tested" column (only celestron-nexstar is `yes`, others `untested, please report`); supported adapters by chipset with VID; NexStar+ hand controller USB-B port has a built-in Prolific chip, older controllers need Celestron's RS-232 cable plus an adapter; cable pinout for the 4P4C PC port; `skybridge list-ports` and `skybridge probe` as the first thing to run.
- `docs/troubleshooting.md`: symptom → cause → fix, keyed on the exact log lines `waiting for serial port`, `could not open serial port`, `probe failed`, `serial port lost`, `listen:`; plus SkySafari-side messages "Telescope not responding" and "Connection refused"; how to run with `--log-level debug` and what to include in a bug report (`skybridge version`, `list-ports`, the log).
- `CHANGELOG.md`: keep-a-changelog header and an `Unreleased` section listing the rewrite.

- [ ] **Step 2: Verify**

Run: `go run ./tools/check fmt` (markdown is not checked; this confirms nothing else broke) and read every relative link target exists: `ls docs/*.md`.

- [ ] **Step 3: Commit**

```bash
git add README.md docs CHANGELOG.md
git commit -m "docs: add user guide, hardware, skysafari setup and troubleshooting"
```

---

### Task 15: Developer documentation

**Files:**
- Create: `docs/development.md`

- [ ] **Step 1: Write it**

Sections: layout (one line per package from the spec's section 6, including `tools/check`); running `go run ./tools/check` and the Linux pty test (`apt install socat` or a Docker one-liner); adding a mount profile (edit the table in `internal/mounts/profiles.go`, add a `Lookup` alias test and a `Probe.Run` test with a canned reply, add a row to `docs/hardware.md`); adding an adapter (one line in `discovery.KnownAdapters` plus a `Rank` test); release procedure (update `CHANGELOG.md`, `git tag -a vX.Y.Z`, `git push --tags`, both pipelines publish); the GitLab/Gitea assumption from Task 13; enabling macOS/Windows SaaS runners on GitLab if ever wanted.

- [ ] **Step 2: Commit**

```bash
git add docs/development.md
git commit -m "docs: add development guide"
```

---

### Task 16: GitHub Actions and GitLab CI

**Files:**
- Create: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.gitlab-ci.yml`

- [ ] **Step 1: Write `ci.yml`**

Triggers: push to `main`, pull requests. Jobs: `check` on `ubuntu-latest` running `go run ./tools/check fmt vet static vuln tidy`; `test` matrix on `ubuntu-latest`, `macos-latest`, `windows-latest` running `go test -race -coverprofile=cover.out ./...` followed by `go tool cover -func=cover.out | tail -1` so coverage is printed but never gated (the Ubuntu job first runs `sudo apt-get install -y socat`); `cross` on `ubuntu-latest` running `go run ./tools/check cross`. All use `actions/checkout@v4` and `actions/setup-go@v5` with `go-version-file: go.mod`.

- [ ] **Step 2: Write `release.yml`**

Trigger: tags `v*`. Job: checkout with `fetch-depth: 0`, setup-go, `goreleaser/goreleaser-action@v6` with `version: "~> v2"` and `args: release --clean`, `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`, `permissions: contents: write`.

- [ ] **Step 3: Write `.gitlab-ci.yml`**

`image: golang:1.26`; stages `check`, `test`, `build`, `release`; cache `GOMODCACHE` keyed on `go.sum`. Jobs: `check` → `go run ./tools/check fmt vet static vuln tidy`; `test` → `apt-get update && apt-get install -y socat && go test -race -coverprofile=cover.out ./... && go tool cover -func=cover.out | tail -1`, with `coverage: '/total:\s+\(statements\)\s+(\d+\.\d+)%/'` so GitLab shows the number; `cross` → `go run ./tools/check cross`; `release` (`rules: if: $CI_COMMIT_TAG =~ /^v/`) uses `image: goreleaser/goreleaser:v2` pinned to a specific tag, `script: goreleaser release --clean`, needs `GITLAB_TOKEN` as a masked CI variable (documented in `docs/development.md`).

- [ ] **Step 4: Push and verify both pipelines**

```bash
git add .github .gitlab-ci.yml
git commit -m "ci: add github actions and gitlab pipelines with release jobs"
git push -u origin main
```

Expected on GitLab: `check`, `test`, `cross` green. If a GitHub mirror exists, `check`, `test` (3 jobs), `cross` green. Fix and recommit anything red; the Windows serial tests from Task 6 run for the first time here.

---

### Task 17: Hardware verification, legacy removal, first release

**Files:**
- Delete: `bridge.py`, `scope-bridge.sh`
- Modify: `CHANGELOG.md`, `docs/hardware.md` (confirm the Prolific PID observed)

- [ ] **Step 1: Verify on the NexStar+ hand controller (owner runs these)**

```bash
go run ./cmd/skybridge list-ports
go run ./cmd/skybridge probe
go run ./cmd/skybridge serve
```

Expected: `list-ports` shows the Prolific port with VID `067b`; `probe` prints `celestron-nexstar at /dev/cu.PL2303... (9600 baud): reply "x#"`; with `serve` running, SkySafari connects, reports the scope position, and slews on command; power-cycling the mount produces `serial port lost` then `serial port opened` and SkySafari reconnects without restarting anything.

- [ ] **Step 2: Record and remove**

Add the observed VID:PID to `docs/hardware.md`. Move the `Unreleased` changelog entries under `0.1.0`.

```bash
git rm bridge.py scope-bridge.sh
git commit -m "chore: remove legacy python bridge after hardware verification"
```

- [ ] **Step 3: Tag and release**

```bash
git tag -a v0.1.0 -m "skybridge 0.1.0"
git push origin main --tags
```

Expected: the release job publishes seven archives and `checksums.txt`; downloading the macOS arm64 archive and running `skybridge version` prints `skybridge v0.1.0 (<commit>, <date>) ...`.
