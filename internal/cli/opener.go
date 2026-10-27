package cli

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/bridge"
	"git.h.usky.io/huskynet/skybridge/internal/mounts"
	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

// serveOptions is everything serve needs to find and open the port.
type serveOptions struct {
	Port    string         // explicit path; empty means discover
	Profile mounts.Profile // supplies baud and probe
	Baud    int            // overrides Profile.Baud when non-zero
	Verify  bool           // probe after every open
	Retry   time.Duration  // delay between attempts
	// Opened, when set, is told which port was opened and at what speed.
	Opened func(path string, baud int)

	probeTimeout time.Duration // tests shorten the probe; zero keeps the profile's
}

func (o serveOptions) baud() int {
	if o.Baud != 0 {
		return o.Baud
	}
	return o.Profile.Baud
}

// newOpener returns a bridge.Opener that keeps trying until a port is open
// (and, with Verify, answers its probe) or ctx is done. It logs state
// changes once, not every retry.
func newOpener(o serveOptions, log *slog.Logger) bridge.Opener {
	return func(ctx context.Context) (io.ReadWriteCloser, error) {
		waiting := false
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			path, err := pickPort(ctx, o.Port, log)
			if err != nil {
				log.Warn("port discovery failed", "err", err)
			}
			if path == "" {
				if !waiting {
					log.Info("waiting for serial port", "mount", o.Profile.Name)
					waiting = true
				}
				if !sleepCtx(ctx, o.Retry) {
					return nil, ctx.Err()
				}
				continue
			}

			port, err := openPort(path, serial.Config{Baud: o.baud()})
			if err != nil {
				if errors.Is(err, serial.ErrNotFound) {
					if !waiting {
						log.Info("waiting for serial port", "mount", o.Profile.Name, "path", path)
						waiting = true
					}
				} else {
					log.Warn("could not open serial port", "path", path, "err", err)
				}
				if !sleepCtx(ctx, o.Retry) {
					return nil, ctx.Err()
				}
				continue
			}

			if o.Verify && o.Profile.Probe != nil {
				probe := *o.Profile.Probe
				if o.probeTimeout > 0 {
					probe.Timeout = o.probeTimeout
				}
				reply, perr := probe.Run(ctx, port)
				if perr != nil {
					log.Warn("probe failed", "path", path, "profile", o.Profile.Name, "reply", reply, "err", perr)
					port.Close()
					if !sleepCtx(ctx, o.Retry) {
						return nil, ctx.Err()
					}
					continue
				}
				log.Debug("probe ok", "path", path, "reply", reply)
			}

			log.Info("serial port opened", "path", path, "baud", o.baud())
			if o.Opened != nil {
				o.Opened(path, o.baud())
			}
			return port, nil
		}
	}
}

// sleepCtx waits d or until ctx ends; it reports whether the full delay elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
