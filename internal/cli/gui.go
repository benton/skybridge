package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/bridge"
	"git.h.usky.io/huskynet/skybridge/internal/discovery"
	"git.h.usky.io/huskynet/skybridge/internal/ui"
	"git.h.usky.io/huskynet/skybridge/internal/version"
)

const defaultUI = "127.0.0.1:4031"

// Hooks tests replace: where the browser goes and where settings live.
var (
	openBrowser = launchBrowser
	configPath  = ui.ConfigPath
)

func init() {
	attach("gui", runGUI)
}

func runGUI(ctx context.Context, s *stdio, args []string) int {
	fs := newFlagSet("gui", s.Err)
	port := fs.String("port", "", "serial port path (default: auto-detect)")
	mount := fs.String("mount", "", "mount profile; see 'skybridge mounts'")
	baud := fs.String("baud", "", "baud rate override")
	addr := fs.String("listen", "", "TCP address SkySafari connects to (default "+defaultListen+")")
	verify := fs.Bool("verify", false, "probe the mount after opening the port and retry until it answers")
	uiAddr := fs.String("ui", defaultUI, "address to serve the control panel on; 0.0.0.0:4031 opens it to the LAN")
	noOpen := fs.Bool("no-open", false, "do not open the control panel in a browser")
	level, format := addLogFlags(fs)
	if ok, code := parseFlags(fs, s, args); !ok {
		return code
	}
	stderr, err := newLogger(s.Err, *level, *format)
	if err != nil {
		fmt.Fprintf(s.Err, "skybridge: %v\n", err)
		return ExitUsage
	}
	stderrHandler := stderr.Handler()
	lvl, _ := parseLevel(*level) // newLogger already validated it

	// Saved settings first, then any flag given on the command line or in the
	// environment on top.
	cfgPath, err := configPath()
	if err != nil {
		stderr.Warn("no config directory; settings will not be saved", "err", err)
	}
	settings := ui.DefaultSettings
	if cfgPath != "" {
		if settings, err = ui.Load(cfgPath); err != nil {
			stderr.Warn("could not read saved settings", "err", err)
		}
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, f := range []string{"port", "mount", "baud", "listen", "verify"} {
		if !set[f] && s.Env(envName(f)) != "" {
			set[f] = true
		}
	}
	if set["port"] {
		settings.Port = *port
	}
	if set["mount"] {
		settings.Mount = *mount
	}
	if set["listen"] {
		settings.Listen = *addr
	}
	if set["verify"] {
		settings.Verify = *verify
	}
	if set["baud"] {
		if settings.Baud, err = parseBaud(*baud); err != nil {
			fmt.Fprintf(s.Err, "skybridge: %v\n", err)
			return ExitUsage
		}
	}

	deps := ui.Deps{
		Open:    guiOpener,
		Ports:   guiPorts,
		Probe:   guiProbe,
		Listen:  listen,
		Retry:   retryDelay,
		Version: version.Version,
	}
	if cfgPath != "" {
		deps.Save = func(s ui.Settings) error { return ui.Save(cfgPath, s) }
	}
	ctl := ui.NewController(deps, settings, lvl, stderrHandler)
	log := ctl.Logger()

	uiLn, err := listen("tcp", *uiAddr)
	if err != nil {
		fmt.Fprintf(s.Err, "skybridge: listen %s: %v\n", *uiAddr, err)
		return ExitListen
	}
	url := "http://" + displayAddr(uiLn.Addr()) + "/"
	log.Info("control panel", "url", url)

	// A bridge that cannot start is still worth a panel: the user fixes the
	// settings there.
	_ = ctl.Start()

	srv := &http.Server{Handler: ui.Handler(ctl, *uiAddr), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(uiLn) }()
	if !*noOpen {
		if err := openBrowser(url); err != nil {
			log.Warn("could not open a browser; open the address yourself", "url", url, "err", err)
		}
	}

	code := ExitOK
	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("control panel stopped", "err", err)
			code = ExitListen
		}
	}

	// In order: stop accepting requests, end the event streams, let every
	// request in flight finish, then take the bridge down, which releases the
	// SkySafari port and the serial port. A probe can take a few seconds, so
	// the wait is generous; past it the bridge is stopped regardless.
	srv.RegisterOnShutdown(ctl.BeginShutdown)
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Warn("some requests did not finish in time", "err", err)
	}
	ctl.Stop()
	log.Info("control panel stopped")
	return code
}

// shutdownWait bounds how long shutdown waits for requests in flight.
const shutdownWait = 15 * time.Second

// displayAddr turns a bound address into one a browser can open.
func displayAddr(a net.Addr) string {
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func guiOpener(s ui.Settings, log *slog.Logger, opened func(path string, baud int)) (bridge.Opener, error) {
	opts, err := buildServeOptions(s.Port, s.Mount, s.Baud, s.Verify)
	if err != nil {
		return nil, err
	}
	opts.Opened = opened
	return newOpener(opts, log), nil
}

func guiPorts(ctx context.Context) ([]discovery.Candidate, error) {
	ports, err := listPorts(ctx)
	if err != nil {
		return nil, err
	}
	return discovery.Rank(runtime.GOOS, ports), nil
}

// guiProbe identifies whatever is on the configured port: the panel's
// "Identify mount" button tells the user what they have, so every profile
// is tried regardless of the one selected.
func guiProbe(ctx context.Context, s ui.Settings, log *slog.Logger) (ui.ProbeResult, error) {
	res, err := probeMount(ctx, s.Port, nil, s.Baud, log)
	if err != nil {
		return ui.ProbeResult{}, err
	}
	return ui.ProbeResult{Profile: res.Profile, Path: res.Path, Baud: res.Baud, Reply: res.Reply}, nil
}

func launchBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
