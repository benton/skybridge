# Developing skybridge

skybridge is written in Go against the standard library. Its one module
dependency is `golang.org/x/sys`, used by the serial package for the
OS calls Go's standard library does not expose. Keep it that way: before
adding a dependency, read `docs/design/code-style.md` and write down why
a domain-specific implementation would be worse.

The design document this code implements is
`docs/superpowers/specs/2026-10-26-go-bridge-design.md`, and the plan it
was built from is under `docs/superpowers/plans/`.

## Layout

| Path | Responsibility |
|---|---|
| `cmd/skybridge/main.go` | Entry point. Calls `cli.Main` and exits with its code. |
| `internal/cli/` | Command dispatch, flag and environment handling, exit codes, and one file per command (`serve`, `list-ports`, `probe`, `mounts`, `version`). `opener.go` turns discovery, serial and probes into the `bridge.Opener` that `serve` uses. |
| `internal/bridge/` | The relay core. Knows nothing about hardware: takes a `net.Listener` and an `Opener` callback, serves one client at a time, refuses connections while the port is absent, reopens after serial failure. Tested entirely with `net.Pipe`. |
| `internal/serial/` | The only package with build tags and the only one importing `x/sys`. `Open` gives an 8N1 `Port`; `List` enumerates ports with USB IDs. Unix uses termios and `poll`, Windows uses `SetCommState`/`SetCommTimeouts` and SetupAPI. |
| `internal/discovery/` | Known USB adapters and the ranking that picks the most likely port. |
| `internal/mounts/` | The mount profile table and the probe runner / identification sequence. |
| `internal/version/` | Build information injected with `-ldflags -X`. |
| `tools/check/` | The developer and CI gate: format, vet, staticcheck, govulncheck, tidy, race tests, cross builds. |

Dependency direction: `cli` imports everything; nothing imports `cli`.
`bridge` imports only the standard library.

## Building and checking

```
go build ./cmd/skybridge       # a binary for this machine
go test -race ./...            # the tests
go run ./tools/check           # everything CI runs, in order
go run ./tools/check test cross   # or any subset of: fmt vet static vuln tidy test cross
```

`tools/check` pins the staticcheck and govulncheck versions it runs with
`go run pkg@version`; bump them in its `const` block. `cross` builds all
seven release targets into `dist/check/`.

### The serial tests

`internal/serial` has real round-trip tests that need a pseudo-terminal
pair created by `socat`. They run on Linux and macOS when `socat` is on
the PATH and skip with a message otherwise.

- Debian/Ubuntu: `sudo apt install socat`
- macOS: `brew install socat`
- Anywhere with Docker: `docker run --rm -v "$PWD":/src -w /src golang:1.26 sh -c 'apt-get update && apt-get install -y socat && go test -race ./internal/serial/'`

The Windows port has no equivalent test; it is compile-checked on every
platform and its small tests run in the Windows CI job.

### Trying it without a mount

`internal/cli` has a fake "responder" port used in tests that answers the
NexStar echo command. For manual testing, `socat` can stand in for a mount:

```
socat -d -d pty,raw,echo=0,link=/tmp/mount pty,raw,echo=0,link=/tmp/pc
skybridge serve --port /tmp/pc --log-level debug
# in another terminal, watch what SkySafari sends:
cat /tmp/mount
```

## Adding a mount profile

1. Add an entry to the `profiles` slice in `internal/mounts/profiles.go`
   with name, aliases, baud, the hardware it covers, and a `Probe` if the
   family has a harmless identification command. Order matters:
   `Identify` tries profiles top to bottom and stops at the first answer,
   so put more specific matchers before general ones (Gemini before
   LX200, for example).
2. In `internal/mounts/profiles_test.go`, extend `TestTableOrderAndDefaults`
   and `TestLookupAliasesCaseInsensitive`.
3. In `internal/mounts/probe_test.go`, add a `Probe.Run` test with the
   reply a real controller gives.
4. Add a row to the table in `docs/hardware.md`, marked
   "untested, please report" unless you have the hardware.

## Adding a USB adapter

Add one line to `KnownAdapters` in `internal/discovery/discovery.go` and a
case to `TestRankAllAdapters`. Vendor IDs are in the USB ID repository
(`/usr/share/hwdata/usb.ids` on most Linux systems).

## Commits

Use Conventional Commits: `feat(serial): …`, `fix(bridge): …`,
`docs: …`, `ci: …`, `build: …`, `chore: …`. The release changelog is
generated from these.

## Releasing

1. Move the `Unreleased` section of `CHANGELOG.md` under the new version
   and date.
2. Commit, tag and push:

   ```
   git tag -a v0.2.0 -m "skybridge 0.2.0"
   git push origin main --tags
   ```

3. Both pipelines run GoReleaser on the tag. It builds all targets,
   writes `checksums.txt`, and creates a release on the forge that ran it
   with the archives attached and release notes grouped by commit type.

GoReleaser decides whether it is talking to GitHub or GitLab from the
token in the environment: GitHub Actions passes `GITHUB_TOKEN`
automatically; on GitLab you must create a masked CI/CD variable named
`GITLAB_TOKEN` holding a project access token with `api` scope.

`.goreleaser.yaml` assumes `git.h.usky.io` is a GitLab instance (the
`gitlab_urls` block). If it turns out to be Gitea or Forgejo, replace that
block with:

```yaml
gitea_urls:
  api: https://git.h.usky.io/api/v1
  download: https://git.h.usky.io
```

and set `GITEA_TOKEN` instead of `GITLAB_TOKEN` in the release job.

## CI

- `.github/workflows/ci.yml`: `check` (gates), `test` on Linux, macOS and
  Windows, `cross` builds. `release.yml` runs GoReleaser on `v*` tags.
- `.gitlab-ci.yml`: the same stages. GitLab's shared runners are Linux
  only, so the test job runs there and the other platforms are covered by
  cross-compilation plus the GitHub matrix. If you ever want native macOS
  or Windows runs on GitLab, add jobs with the `saas-macos-medium-m1` and
  `saas-windows-medium-amd64` tags; both are paid SaaS runners.

Both pipelines call `go run ./tools/check` so there is one definition of
"passing", shared with your terminal.
