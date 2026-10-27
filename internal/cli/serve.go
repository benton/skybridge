package cli

import (
	"context"
	"fmt"
	"net"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/bridge"
	"git.h.usky.io/huskynet/skybridge/internal/mounts"
)

const (
	defaultListen = "127.0.0.1:4030"
	retryDelay    = 2 * time.Second
)

// listen creates the TCP listener; tests replace it.
var listen = net.Listen

func init() {
	attach("serve", runServe)
}

func runServe(ctx context.Context, s *stdio, args []string) int {
	fs := newFlagSet("serve", s.Err)
	port := fs.String("port", "", "serial port path (default: auto-detect)")
	mount := fs.String("mount", mounts.DefaultName, "mount profile; see 'skybridge mounts'")
	baud := fs.String("baud", "", "baud rate override")
	addr := fs.String("listen", defaultListen, "TCP address to listen on; 0.0.0.0:4030 exposes the mount to the whole LAN")
	verify := fs.Bool("verify", false, "probe the mount after opening the port and retry until it answers")
	level, format := addLogFlags(fs)
	if ok, code := parseFlags(fs, s, args); !ok {
		return code
	}
	log, err := newLogger(s.Err, *level, *format)
	if err != nil {
		fmt.Fprintf(s.Err, "skybridge: %v\n", err)
		return ExitUsage
	}
	opts := serveOptions{Port: *port, Verify: *verify, Retry: retryDelay}
	if opts.Profile, err = resolveMount(*mount); err != nil {
		fmt.Fprintf(s.Err, "skybridge: %v\n", err)
		return ExitUsage
	}
	if *baud != "" {
		if opts.Baud, err = parseBaud(*baud); err != nil {
			fmt.Fprintf(s.Err, "skybridge: %v\n", err)
			return ExitUsage
		}
	}
	if opts.baud() == 0 {
		fmt.Fprintf(s.Err, "skybridge: profile %q has no baud rate; pass --baud\n", opts.Profile.Name)
		return ExitUsage
	}
	if opts.Verify && opts.Profile.Probe == nil {
		fmt.Fprintf(s.Err, "skybridge: profile %q has no probe; --verify is not available\n", opts.Profile.Name)
		return ExitUsage
	}

	ln, err := listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(s.Err, "skybridge: listen %s: %v\n", *addr, err)
		return ExitListen
	}
	log.Info("listening", "addr", ln.Addr().String(), "mount", opts.Profile.Name)

	err = bridge.Run(ctx, bridge.Config{
		Listener:   ln,
		Open:       newOpener(opts, log),
		Logger:     log,
		RetryDelay: retryDelay,
	})
	if err != nil {
		log.Error("bridge stopped", "err", err)
		return ExitListen
	}
	log.Info("stopped")
	return ExitOK
}
