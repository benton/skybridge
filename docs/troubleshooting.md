# Troubleshooting

skybridge logs one line per change of state to standard error. Find the
last line it printed and look it up below. `--log-level debug` adds a line
for every SkySafari connection, which is a lot, but shows whether SkySafari
is reaching skybridge at all.

## skybridge log lines

### `waiting for serial port`

skybridge could not find a serial port that looks like a mount.

- Is the hand controller plugged into the computer, and the mount powered
  on? Many controllers only enumerate on USB once they have power from the
  mount.
- Run `skybridge list-ports`. If the port is listed but not recognised
  (no adapter name), pass it explicitly: `skybridge serve --port <path>`.
  If nothing is listed, the OS does not see the adapter. Confirm that
  before suspecting skybridge or a driver:
  - macOS: `ioreg -p IOUSB -w0` should list a USB-Serial Controller (or
    your adapter's name) under one of the host controllers.
  - Linux: `lsusb` should list the adapter's vendor, for example Prolific.
  - Windows: Device Manager should show it under **Ports (COM & LPT)** or
    **Other devices**.
  If the adapter is missing there too, the fault is physical: reseat both
  ends of the cable, in particular the small connector on the hand
  controller, and try without hubs or adapters. A marginal connector can
  work once and then vanish on the next replug. Only once the OS lists the
  device is a driver (Windows, macOS Prolific) or the `dialout` group
  (Linux) worth looking at.
- If `--port` was given and the file does not exist yet, this line is
  normal; skybridge keeps checking every two seconds and opens the port
  when it appears.

### `could not open serial port`

The port exists but could not be opened. The message includes the OS
error.

- `permission denied` on Linux: add yourself to the `dialout` group and
  log in again (see [install.md](install.md)).
- `resource busy` or `access denied`: another program has the port open.
  Close other astronomy software, an older bridge script, a terminal
  emulator, or stop ModemManager (Linux).
- `device not configured` on macOS right after plugging in: wait a second;
  skybridge retries on its own.

### `probe failed` (only with `--verify`)

The port opened but the mount did not answer the identification command
for the selected profile. skybridge closes the port and tries again.

- Wrong `--mount` for this hardware? `skybridge probe` without `--mount`
  tries every profile and tells you which one answers.
- Wrong speed? Try `--baud` with the other rate your mount supports.
- Mount still booting: the hand controller ignores commands until it has
  finished its start-up screens. Give it a few seconds.

### `serial port lost`

A read or write failed, which almost always means the cable came out or
the mount was powered off. skybridge goes back to waiting and reconnects
when the port returns. SkySafari will say the telescope is not responding
in the meantime; reconnect from SkySafari once skybridge logs
`serial port opened` again.

### `listen 127.0.0.1:4030: …address already in use`

Something is already listening on the port, most likely another skybridge
or the old Python bridge. Stop it, or choose another port with `--listen`
and change the port in SkySafari to match. skybridge exits with code 2 in
this case.

### `rejected client that speaks HTTP`

Something connected to skybridge's port and sent an HTTP request instead
of telescope commands. skybridge closes such connections without
forwarding anything, because a web page could otherwise be tricked into
sending commands to your mount through your own browser. If you see this
line while SkySafari is working normally, something else on the machine
(a port scanner, a browser tab, a monitoring tool) is poking the port and
you can ignore it. If you see it on a `--listen 0.0.0.0` address, consider
whether the network it is exposed to is one you trust.

### `unknown mount profile`

Check the spelling against `skybridge mounts`.

### `unsupported baud rate`

Only 4800, 9600, 19200, 38400, 57600 and 115200 are accepted.

## SkySafari messages

### "Telescope not responding" or "Could not connect"

SkySafari reached skybridge, but skybridge had no serial port open, or the
mount is not answering the protocol SkySafari is speaking.

1. Look at the skybridge log. If it says `waiting for serial port`, fix
   that first (above).
2. If it says `serial port opened`, run `skybridge probe` to confirm the
   mount answers. If it does, check SkySafari's **Scope Type**: a NexStar
   controller configured as a Meade in SkySafari produces exactly this
   message.
3. If `probe` does not get an answer either, check the mount: powered on,
   hand controller plugged into the mount, controller past its start-up
   screens.

### "Connection refused" or "Connection failed"

SkySafari could not reach skybridge at all.

- Is skybridge running? Its first log line is `listening addr=…`.
- Is the IP address right? `127.0.0.1` only works when SkySafari runs on
  the same computer. From a phone or tablet you need
  `skybridge serve --listen 0.0.0.0:4030` and the computer's LAN address.
- Is the port `4030`, matching skybridge's `--listen`?
- Is **Auto-Detect SkyFi** turned off in SkySafari?
- Firewall: on Windows the first run with `--listen 0.0.0.0` triggers a
  firewall prompt; allow it on private networks.

### SkySafari connects but the scope position jumps or commands lag

- Check for a second program using the same port. Only one may own it.
- On Linux make sure ModemManager is not probing the port
  (see [install.md](install.md)).
- Try `--log-level debug` and watch for `client write failed` lines, which
  indicate SkySafari dropping connections mid-poll.

## What to include in a bug report

```
skybridge version
skybridge list-ports
skybridge probe
```

plus the skybridge log from startup to the problem (run with
`--log-level debug` if the issue is intermittent), your operating system
and version, the mount and hand-controller model, and the cable or adapter
in use.
