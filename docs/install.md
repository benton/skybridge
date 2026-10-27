# Installing skybridge

Every release ships one archive per operating system and CPU. Pick the one
that matches your machine, unpack it, and run the `skybridge` program
inside. There is nothing else to install: no runtime, no library, no
configuration file.

| Archive suffix | Machine |
|---|---|
| `darwin_arm64` | Mac with Apple silicon |
| `darwin_amd64` | Intel Mac |
| `linux_amd64` | 64-bit PC Linux |
| `linux_arm64` | Raspberry Pi 3/4/5 running 64-bit Raspberry Pi OS |
| `linux_arm` | Raspberry Pi running 32-bit Raspberry Pi OS, Pi Zero |
| `windows_amd64` | 64-bit Windows |
| `windows_arm64` | Windows on ARM |

The `checksums.txt` file next to the archives lets you verify a download:
`sha256sum -c checksums.txt --ignore-missing` on Linux and macOS,
`Get-FileHash` on Windows.

## macOS

1. Unpack the archive and move `skybridge` somewhere on your PATH, for
   example `/usr/local/bin`.
2. The first run may be blocked by Gatekeeper because the binary is not
   notarised. Clear the quarantine flag once:

   ```
   xattr -d com.apple.quarantine /usr/local/bin/skybridge
   ```

3. Plug in the hand controller and run `skybridge list-ports`. The port
   appears as `/dev/cu.usbserial-…` with Apple's built-in drivers, or as
   `/dev/cu.PL2303G-USBtoUART…`, `/dev/cu.SLAB_USBtoUART…` or
   `/dev/cu.wchusbserial…` when the chip vendor's own driver is installed.
   Recent macOS versions include drivers for FTDI, Prolific and Silicon
   Labs chips; WCH CH340 adapters may need the vendor driver on older
   releases.

## Linux

1. Unpack the archive and put `skybridge` on your PATH, for example
   `~/.local/bin` or `/usr/local/bin`.
2. Serial devices belong to the `dialout` group (`uucp` on Arch). Add
   yourself, then log out and back in:

   ```
   sudo usermod -aG dialout "$USER"
   ```

3. Plug in the hand controller. The port appears as `/dev/ttyUSB0` for
   most adapters or `/dev/ttyACM0` for a few. `skybridge list-ports` shows
   which chip is behind each one.

Two system services are known to grab USB serial ports on desktop
distributions:

- **ModemManager** probes every new serial port as if it were a modem,
  which can confuse the hand controller for a few seconds after plugging
  in. Tell it to ignore the adapter with a udev rule, for example for a
  Prolific chip:

  ```
  # /etc/udev/rules.d/90-skybridge.rules
  ATTRS{idVendor}=="067b", ENV{ID_MM_DEVICE_IGNORE}="1"
  ```

  Then `sudo udevadm control --reload`. Or disable it entirely if you do
  not use a modem: `sudo systemctl disable --now ModemManager`.
- **brltty** (a braille display daemon installed by default on some Ubuntu
  releases) claims CH340 adapters. `sudo apt remove brltty` if you do not
  need it.

## Windows

1. Unpack the zip and put `skybridge.exe` somewhere convenient, or add
   its folder to your PATH.
2. SmartScreen may warn that the publisher is unknown. Choose **More
   info → Run anyway**.
3. Install the driver for your cable's chip if Windows did not find one.
   Device Manager shows the port under **Ports (COM & LPT)** as `COM3`,
   `COM4` and so on; `skybridge list-ports` shows the same list with the
   chip identified.

   | Chip | Driver |
   |---|---|
   | Prolific PL2303 (Celestron USB hand controllers and cables) | prolific.com.tw, "PL2303 USB-to-Serial" driver |
   | FTDI FT232 | ftdichip.com, "VCP drivers" |
   | Silicon Labs CP210x | silabs.com, "CP210x USB to UART Bridge VCP Drivers" |
   | WCH CH340/CH341 | wch-ic.com, "CH341SER" |

   Note for Prolific: current Prolific drivers refuse to work with
   counterfeit PL2303 chips and show the device with a yellow warning and
   "This device cannot start (Code 10)". Genuine Celestron cables and
   controllers are fine; some cheap third-party cables are not.

## Running at login

skybridge is meant to be left running. Set every option through
environment variables so the service definition stays readable; flags on
the command line work too.

### macOS (launchd)

Save as `~/Library/LaunchAgents/io.usky.skybridge.plist`, then
`launchctl load ~/Library/LaunchAgents/io.usky.skybridge.plist`.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>io.usky.skybridge</string>
  <key>ProgramArguments</key>
  <array><string>/usr/local/bin/skybridge</string><string>serve</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>SKYBRIDGE_MOUNT</key><string>celestron-nexstar</string>
    <key>SKYBRIDGE_LOG_FORMAT</key><string>json</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>/tmp/skybridge.log</string>
</dict>
</plist>
```

### Linux (systemd user unit)

Save as `~/.config/systemd/user/skybridge.service`, then
`systemctl --user enable --now skybridge`. Logs go to
`journalctl --user -u skybridge`.

```ini
[Unit]
Description=skybridge serial-to-TCP bridge for SkySafari

[Service]
ExecStart=%h/.local/bin/skybridge serve
Environment=SKYBRIDGE_MOUNT=celestron-nexstar
Environment=SKYBRIDGE_LOG_FORMAT=json
Restart=on-failure

[Install]
WantedBy=default.target
```

Because skybridge waits for the port itself and never exits when the mount
is off, `Restart=on-failure` only matters if the TCP port is taken.

### Windows (Task Scheduler)

Create a basic task that runs `skybridge.exe serve` at log on, with
"Run whether user is logged on or not" unticked so it stays in your
session. Set environment variables for the task through the **Actions →
Start a program** dialog's "Start in" folder plus a small `.cmd` wrapper
if you need any; most people need none.
