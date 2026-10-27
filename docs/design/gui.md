# The control panel

`skybridge gui` runs the bridge and serves a one-page control panel on
`127.0.0.1:4031`. This note records why it is a web page and the design
decisions behind the page, so later changes stay coherent.

## Why a page and not a window

Every native Go toolkit (Fyne, Gio, Wails, webview) needs cgo and a
platform library: OpenGL and X11 headers on Linux, WebKitGTK for the
webview ones, Cocoa on macOS. That breaks the project's promises in
`code-style.md` and the README: one static binary, `CGO_ENABLED=0`, seven
release targets cross-compiled from one CI job. It also cannot serve the
Raspberry Pi case at all, where the bridge runs headless on a shelf and the
only screen nearby is the phone running SkySafari.

An HTTP page costs nothing: `net/http` and `embed` are standard library,
the page is a few kilobytes, and the phone case works for free. The price
is that the panel lives in a browser tab, which the command opens itself.

## Who it is for

Someone at a telescope at night, probably with a laptop or phone, with
dark-adapted eyes. The page has one job: say whether SkySafari is talking to
the mount right now and, when it is not, which link is broken. Everything
else (choosing a port, a mount profile, a baud rate) is setup done once.

## Tokens

Colour. The page is dark because it is used in the dark; there is no light
theme. The palette is a night sky, not neutral grey.

| Token | Value | Role |
|---|---|---|
| sky | `#0e1322` | page background |
| field | `#161c2e` | panels, inputs |
| haze | `#8a93a8` | secondary text, idle links |
| starlight | `#e9ebf1` | primary text |
| ember | `#e9a23b` | a live link, the running state; the only accent |
| fault | `#e4574f` | an error |

Red-light mode remaps every token to reds on pure black, the astronomer's
convention for keeping night vision. It is a toggle in the header and is
remembered by the browser. This is the one deliberately bold choice; the
rest of the page is quiet.

Type. One system sans-serif stack, because the page must render in a field
with no internet. Headline 600, body 400, tabular numerals for counters.
The log is the only monospace text; its timestamps line up.

Layout. Left aligned. A header with the name, version, the Start/Stop
control and the red-light toggle. Then the chain, full width, as the hero:

```
SkySafari ········○ Sky Bridge ●━━━━━━━━━━ Mount
not connected       127.0.0.1:4030   /dev/cu.PL2303G  9600 baud
                    waiting            12.4 kB received
```

Below it a two-column grid, log on the left (wider) and setup on the
right, collapsing to one column on a phone.

## Principles

- The chain is the status. There are no status badges anywhere else.
- Every state is a sentence someone would say out loud: "Waiting for the
  cable", "SkySafari connected from 192.168.1.20".
- Nothing moves except a link while bytes flow through it, and that
  respects `prefers-reduced-motion`.
- Setup changes take effect with one button, Apply, which restarts the
  bridge if it is running and saves the settings for next time.

## API

The page talks to the same process over JSON. `GET /api/state` is the
whole picture; `GET /api/events` streams it as server-sent events along
with log lines. `POST /api/start`, `/api/stop`, `/api/settings` and
`/api/probe` change things and must carry `Content-Type: application/json`,
which a cross-site form cannot, so a hostile web page cannot drive the
bridge. `GET /api/ports` and `GET /api/mounts` are read-only lists.
