# Changelog

All notable changes to skybridge are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `gui` command: serves a one-page control panel on `127.0.0.1:4031` and
  opens it in the browser. It shows the SkySafari, bridge and mount chain
  live, streams the log, starts and stops the bridge, and edits the
  settings, which it saves in the user's config directory. A red-light mode
  keeps night vision. See `docs/gui.md`.
- `bridge.Config.Events` callbacks for port, client and traffic changes.
- A project logo in `assets/logo.svg`.

## [0.1.0] - 2026-10-27

First release. Verified against a Celestron NexStar+ hand controller on a
Prolific PL2303G cable with SkySafari 7 Pro on macOS, including cable
loss and reattach while connected.

### Added

- Rewrite of the original Python `bridge.py` as a single static Go binary
  for Windows, macOS and Linux (amd64, arm64, 32-bit ARM).
- `serve` relays one serial port to one TCP client at a time with the
  connection semantics SkySafari expects, reattaches automatically when
  the cable or mount comes and goes, and shuts down cleanly on Ctrl-C.
- Mount profiles for Celestron NexStar, SkyWatcher SynScan, Losmandy
  Gemini, Meade LX200/Autostar, iOptron (9600 and 115200 baud) and a
  generic profile with `--baud`.
- Automatic port discovery that recognises Prolific, FTDI, Silicon Labs
  and WCH adapters by USB vendor ID, with `--port` to override.
- `list-ports`, `probe`, `mounts` and `version` commands, each with
  `--json` output.
- `--verify` to probe the mount after every port open and keep retrying
  until it answers.
- Environment-variable twins for every flag (`SKYBRIDGE_*`).
- Documentation for installation, SkySafari setup, hardware and
  troubleshooting.
- Release pipelines on GitLab CI and GitHub Actions producing archives and
  checksums for every platform.
