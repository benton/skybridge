# Supported hardware

skybridge is a transparent relay: whatever SkySafari sends goes to the
mount unchanged, and vice versa. That means it works with any mount
SkySafari can drive over a serial cable, as long as skybridge opens the
port at the right speed. The mount profiles below bundle the right speed
with a short identification probe used by `skybridge probe` and
`skybridge serve --verify`.

Only the Celestron NexStar+ hand controller has been tested on real
hardware by the author. Every other profile follows the manufacturer's
published serial settings and is marked untested; reports of success or
failure are welcome.

## Mount profiles

Select one with `--mount`. Names and aliases are case-insensitive.

| Profile | Aliases | Baud | Covers | Tested |
|---|---|---|---|---|
| `celestron-nexstar` (default) | `nexstar`, `celestron` | 9600 | NexStar+, NexStar SE/SLT/Evolution, CGEM, CGX and AVX hand controllers, StarSense hand controller | yes |
| `skywatcher-synscan` | `synscan`, `skywatcher` | 9600 | SynScan V3/V4/V5 hand controllers; EQ6, HEQ5, AZ-GTi over cable | untested, please report |
| `losmandy-gemini` | `gemini`, `losmandy` | 9600 | Gemini 1 and 2 | untested, please report |
| `meade-lx200` | `lx200`, `meade`, `autostar` | 9600 | LX200 Classic and GPS, Autostar, Autostar II, ETX | untested, please report |
| `ioptron` | `ioptron-9600` | 9600 | CEM, GEM, iEQ and SmartEQ mounts with 9600-baud firmware | untested, please report |
| `ioptron-115200` | | 115200 | CEM40/70/120, GEM45 and other iOptron mounts with 115200-baud firmware | untested, please report |
| `generic` | | `--baud` required | anything else | n/a |

Notes:

- SynScan hand controllers speak the NexStar dialect, so `skybridge probe`
  reports them as `celestron-nexstar`. The two profiles use identical
  settings, so it makes no difference to `serve`.
- Some mounts offer more than one speed. If a profile's speed does not
  work, `--baud` overrides it: `skybridge serve --mount ioptron --baud 115200`.
- Supported baud rates: 4800, 9600, 19200, 38400, 57600, 115200.

## USB-to-serial adapters

skybridge recognises these chips on the USB bus and ranks their ports
first when choosing automatically. Any other adapter still works; pass
`--port` if more than one serial port is present.

| Chip | USB vendor ID | Found in |
|---|---|---|
| Prolific PL2303 | `067b` | Celestron's USB hand controllers and Celestron's own USB cables |
| FTDI FT232 | `0403` | most well-made third-party cables |
| Silicon Labs CP210x | `10c4` | many hobbyist and iOptron cables |
| WCH CH340/CH341 | `1a86` | inexpensive cables |

`skybridge list-ports` prints the vendor and product IDs it sees, so you
can check which chip you have:

```
PATH                          ADAPTER          VID:PID    PRODUCT
/dev/cu.PL2303G-USBtoUART210  Prolific PL2303  067b:23a3  USB-Serial Controller
/dev/cu.Bluetooth-Incoming-Port  -             -          -
```

## Cables

### Celestron NexStar+ and StarSense hand controllers

Newer NexStar+ hand controllers ("NexStar+ USB") have a USB port on the
bottom edge with a Prolific PL2303 chip built in. A plain USB cable is all
you need; the controller shows up as a serial port directly.

Older NexStar+ and NexStar controllers instead have a small 4-pin
telephone-style jack labelled **PC**. That jack carries RS-232 at 9600
baud and needs Celestron's RS-232 cable (part 93920), which ends in a DB9
plug, plus any USB-to-serial adapter from the table above. Home-made cables
work but the jack's pinout differs between controller generations; check
Celestron's documentation for your controller before wiring one.

The hand controller must be plugged into the mount and the mount powered
on before the controller answers anything, even over USB.

### Other mounts

- **SkyWatcher SynScan**: the hand controller's RJ-12 "PC" jack with
  SkyWatcher's RS-232 cable, or the V5 controller's USB port.
- **Meade**: Meade's #505 cable for Autostar controllers, or the RS-232
  jack on LX200 base panels.
- **iOptron**: the controller's RS-232 jack with iOptron's cable, or the
  USB port that newer controllers provide.
- **Losmandy Gemini**: the Gemini unit's RS-232 port (Gemini 1) or its USB
  port (Gemini 2).

## The first thing to try

```
skybridge list-ports     # is the port there, and which chip is it?
skybridge probe          # does the mount answer, and what is it?
skybridge serve          # start relaying for SkySafari
```
