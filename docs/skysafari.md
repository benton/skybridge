# Connecting SkySafari

skybridge pretends to be a SkyFi, Simulation Curriculum's Wi-Fi-to-serial
box. So in SkySafari you configure a Wi-Fi connection, not a serial one,
and point it at the computer running skybridge. SkySafari then speaks the
mount's own protocol through skybridge exactly as it would through a
SkyFi.

Start `skybridge` first, confirm the log says `serial port opened`, then
set up SkySafari. Exact labels differ a little between SkySafari versions
and platforms; the settings you need are the same.

## Address and port

| SkySafari runs on | IP address | Port |
|---|---|---|
| The same computer as skybridge | `127.0.0.1` | `4030` |
| A phone, tablet or other computer | the LAN address of the computer running skybridge, with `skybridge --listen 0.0.0.0:4030` | `4030` |

Turn off any "auto-detect SkyFi" option; skybridge does not answer the
discovery broadcast, so SkySafari must be given the address.

## SkySafari 7

1. Open **Settings** (the gear) and choose **Telescope**.
2. Under **Presets**, tap **+** (or **Add Preset**) to create one, or
   edit the preset you already use.
3. For the connection choose **Wi-Fi** (sometimes shown as
   *Wi-Fi / Ethernet*). Disable **Auto-Detect SkyFi** and enter the IP
   address and port from the table above.
4. Choose the **Scope Type** for your mount (next section) and the
   **Mount Type**: *Alt-Az GoTo* for a NexStar SE/SLT/Evolution on its
   normal base, *Equatorial GoTo* for AVX, CGEM, CGX, EQ6 and other
   German equatorial mounts, *Alt-Az GoTo on Wedge* for a fork mount on a
   wedge.
5. Save the preset, then use **Connect** on the telescope toolbar.

## SkySafari 6

1. Open **Settings** and choose **Telescope → Setup**.
2. Under **Equipment Selection** set **Scope Type** and **Mount Type** as
   above.
3. Under **Communication Settings** choose **Connect via Wi-Fi**, turn
   off **Auto-Detect SkyFi**, and enter the IP address and port.
4. Leave **Set Time & Location** on if you want SkySafari to push the
   phone's clock and GPS position into the hand controller on connect;
   turn it off if you prefer to align the mount from the hand controller
   first.
5. Return to the chart and use **Connect** on the telescope toolbar.

## Scope Type per skybridge profile

| skybridge profile | Look for the SkySafari Scope Type named like… |
|---|---|
| `celestron-nexstar` | *Celestron NexStar / Advanced GT*, or the more specific *Celestron NexStar GPS/SLT/SE*, *Celestron AVX/CGEM/CGX* entries for those mounts |
| `skywatcher-synscan` | *SkyWatcher SynScan* |
| `meade-lx200` | *Meade LX200 Classic*, *Meade LX200 GPS* or *Meade Autostar/Audiostar*, matching your controller |
| `losmandy-gemini` | *Losmandy Gemini* (version 1 or 2) |
| `ioptron`, `ioptron-115200` | the *iOptron* entry that names your mount family |
| `generic` | whichever entry matches the mount's protocol; skybridge only sets the baud rate |

The Scope Type tells SkySafari which protocol to speak; skybridge does not
look at the protocol at all except in `skybridge probe`, so a wrong Scope
Type produces "telescope not responding" rather than a skybridge error.

## Checking the link without SkySafari

```
skybridge probe
```

sends one short identification command to the mount and prints the family
that answered, for example:

```
celestron-nexstar at /dev/cu.usbserial-110 (9600 baud): reply "x#"
```

If that works and SkySafari still cannot connect, the problem is in the
SkySafari settings, almost always the IP address, the port, or auto-detect
being left on. See [troubleshooting.md](troubleshooting.md).

## Time and location

Celestron and SkyWatcher hand controllers want the date, time and site
before they can align. SkySafari can send them on connect (the "Set Time &
Location" option); the hand controller's own alignment still has to be
done on the controller. skybridge does not touch any of this.
