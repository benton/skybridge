#!/usr/bin/env python3
"""Serial-to-TCP bridge for the Celestron NexStar+ hand controller.

Opens the USB serial port once and listens on 127.0.0.1:4030 so SkySafari
can connect to it as a SkyFi. Only one TCP client is served at a time; a new
connection replaces the previous one. SkySafari opens a fresh connection for
every position poll, several times a second, which this handles.

If the hand controller disappears (mount powered off, cable unplugged), the
bridge keeps running and reattaches when the port comes back.
"""
import glob, os, select, socket, sys, termios, time

HOST, PORT = "127.0.0.1", 4030
PATTERN = "/dev/cu.PL2303G-USBtoUART*"

def log(msg):
    print(time.strftime("%H:%M:%S"), msg, file=sys.stderr, flush=True)

def find_port(explicit):
    if explicit:
        return explicit if os.path.exists(explicit) else None
    return next(iter(glob.glob(PATTERN)), None)

def open_serial(path):
    fd = os.open(path, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
    a = termios.tcgetattr(fd)
    a[0] = 0; a[1] = 0
    a[2] = termios.CS8 | termios.CREAD | termios.CLOCAL
    a[3] = 0
    a[4] = a[5] = termios.B9600
    termios.tcsetattr(fd, termios.TCSANOW, a)
    termios.tcflush(fd, termios.TCIOFLUSH)
    return fd

def serve(srv, ser):
    """Relay between the serial fd and TCP clients until the serial port fails."""
    client = None
    try:
        while True:
            rl = [srv, ser] + ([client] if client else [])
            ready, _, _ = select.select(rl, [], [], 1.0)
            for r in ready:
                if r is srv:
                    c, addr = srv.accept()
                    c.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
                    if client:
                        try: client.close()
                        except OSError: pass
                    client = c
                    termios.tcflush(ser, termios.TCIOFLUSH)
                elif r is ser:
                    try: data = os.read(ser, 256)
                    except BlockingIOError: continue
                    if not data:
                        raise OSError("serial port returned EOF")
                    if client:
                        try: client.sendall(data)
                        except OSError:
                            client.close(); client = None
                elif r is client:
                    try: data = client.recv(256)
                    except OSError: data = b""
                    if not data:
                        client.close(); client = None
                    else:
                        os.write(ser, data)
    finally:
        if client:
            try: client.close()
            except OSError: pass

def main():
    explicit = sys.argv[1] if len(sys.argv) > 1 else None
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind((HOST, PORT)); srv.listen(5)
    log(f"listening on tcp {HOST}:{PORT}  (Ctrl-C to stop)")
    while True:
        path = find_port(explicit)
        if not path:
            log("waiting for the hand controller's serial port (is the mount on and plugged in?)")
            while not (path := find_port(explicit)):
                # refuse connections while the scope is absent so SkySafari reports it cleanly
                r, _, _ = select.select([srv], [], [], 2.0)
                if r:
                    c, _ = srv.accept(); c.close()
        try:
            ser = open_serial(path)
        except OSError as e:
            log(f"could not open {path}: {e}"); time.sleep(2); continue
        log(f"connected to hand controller on {path}")
        try:
            serve(srv, ser)
        except OSError as e:
            log(f"lost the hand controller: {e}")
        finally:
            try: os.close(ser)
            except OSError: pass
        time.sleep(1)

if __name__ == "__main__":
    try: main()
    except KeyboardInterrupt: log("stopped")
