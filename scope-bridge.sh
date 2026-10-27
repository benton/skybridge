#!/bin/sh
# Bridges the NexStar+ hand controller's USB serial port to TCP 127.0.0.1:4030,
# so SkySafari can connect to it as a SkyFi. Optional argument: serial port path.
exec python3 "$(dirname "$0")/bridge.py" "$@"
