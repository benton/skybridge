package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"git.h.usky.io/huskynet/skybridge/internal/discovery"
	"git.h.usky.io/huskynet/skybridge/internal/mounts"
	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

// Hardware hooks shared by probe and serve; tests replace them.
var (
	findPorts = discovery.Find
	openPort  = serial.Open
)

func init() {
	attach("probe", runProbe)
}

// parseBaud accepts a supported rate or explains which ones are.
func parseBaud(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid baud rate %q (supported: %s)", s, serial.SupportedBaudList())
	}
	for _, b := range serial.SupportedBauds {
		if b == n {
			return n, nil
		}
	}
	return 0, fmt.Errorf("unsupported baud rate %d (supported: %s)", n, serial.SupportedBaudList())
}

// resolveMount turns --mount into a profile, with a helpful error.
func resolveMount(name string) (mounts.Profile, error) {
	p, ok := mounts.Lookup(name)
	if !ok {
		return mounts.Profile{}, fmt.Errorf("unknown mount profile %q; run 'skybridge mounts' to list them", name)
	}
	return p, nil
}

// pickPort returns the explicit path or the best discovered candidate.
// It returns "" with a nil error when nothing plausible exists.
func pickPort(ctx context.Context, explicit string, log *slog.Logger) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	cands, err := findPorts(ctx)
	if err != nil {
		return "", err
	}
	if len(cands) == 0 {
		return "", nil
	}
	if len(cands) > 1 {
		others := make([]string, 0, len(cands)-1)
		for _, c := range cands[1:] {
			others = append(others, c.Path)
		}
		log.Info("other candidates", "paths", others)
	}
	return cands[0].Path, nil
}

type probeResult struct {
	Profile string `json:"profile"`
	Path    string `json:"path"`
	Baud    int    `json:"baud"`
	Reply   string `json:"reply"`
}

func runProbe(ctx context.Context, s *stdio, args []string) int {
	fs := newFlagSet("probe", s.Err)
	port := fs.String("port", "", "serial port path (default: auto-detect)")
	mount := fs.String("mount", "", "mount profile to probe (default: try every profile)")
	baud := fs.String("baud", "", "baud rate override")
	asJSON := fs.Bool("json", false, "print as JSON")
	level, format := addLogFlags(fs)
	if ok, code := parseFlags(fs, s, args); !ok {
		return code
	}
	log, err := newLogger(s.Err, *level, *format)
	if err != nil {
		fmt.Fprintf(s.Err, "skybridge: %v\n", err)
		return ExitUsage
	}

	var profile mounts.Profile
	if *mount != "" {
		if profile, err = resolveMount(*mount); err != nil {
			fmt.Fprintf(s.Err, "skybridge: %v\n", err)
			return ExitUsage
		}
		if profile.Probe == nil {
			fmt.Fprintf(s.Err, "skybridge: profile %q has no probe\n", profile.Name)
			return ExitUsage
		}
	}
	baudOverride := 0
	if *baud != "" {
		if baudOverride, err = parseBaud(*baud); err != nil {
			fmt.Fprintf(s.Err, "skybridge: %v\n", err)
			return ExitUsage
		}
	}

	path, err := pickPort(ctx, *port, log)
	if err != nil {
		fmt.Fprintf(s.Err, "skybridge: %v\n", err)
		return ExitProbe
	}
	if path == "" {
		fmt.Fprintln(s.Err, "skybridge: no serial port found (is the mount on and the cable plugged in?)")
		return ExitProbe
	}

	open := func(ctx context.Context, b int) (io.ReadWriteCloser, error) {
		if baudOverride != 0 {
			b = baudOverride
		}
		p, err := openPort(path, serial.Config{Baud: b})
		if err != nil {
			return nil, err
		}
		return p, nil
	}

	var res probeResult
	if *mount != "" {
		p, err := open(ctx, profile.Baud)
		if err != nil {
			return reportOpenError(s, err)
		}
		reply, perr := profile.Probe.Run(ctx, p)
		p.Close()
		if perr != nil {
			fmt.Fprintf(s.Err, "skybridge: %s at %s: %v\n", profile.Name, path, perr)
			return ExitProbe
		}
		res = probeResult{profile.Name, path, effectiveBaud(profile.Baud, baudOverride), reply}
	} else {
		p, reply, err := mounts.Identify(ctx, open, log)
		if err != nil {
			if errors.Is(err, mounts.ErrUnidentified) {
				fmt.Fprintf(s.Err, "skybridge: no known mount answered on %s\n", path)
				return ExitProbe
			}
			return reportOpenError(s, err)
		}
		res = probeResult{p.Name, path, effectiveBaud(p.Baud, baudOverride), reply}
	}

	if *asJSON {
		return printJSON(s, res)
	}
	fmt.Fprintf(s.Out, "%s at %s (%d baud): reply %q\n", res.Profile, res.Path, res.Baud, res.Reply)
	return ExitOK
}

func effectiveBaud(profileBaud, override int) int {
	if override != 0 {
		return override
	}
	return profileBaud
}

// reportOpenError maps a port-open failure to the right exit code: a path that
// does not exist is the user's mistake, anything else is the mount's.
func reportOpenError(s *stdio, err error) int {
	fmt.Fprintf(s.Err, "skybridge: %v\n", err)
	if errors.Is(err, serial.ErrNotFound) {
		return ExitUsage
	}
	return ExitProbe
}
