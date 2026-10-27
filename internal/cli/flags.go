package cli

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// envName is the environment twin of a flag: SKYBRIDGE_LOG_LEVEL for --log-level.
func envName(flagName string) string {
	return "SKYBRIDGE_" + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// applyEnv sets every flag the user did not pass from its environment twin.
// Call after fs.Parse. A flag given on the command line always wins.
func applyEnv(fs *flag.FlagSet, env Env) {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	fs.VisitAll(func(f *flag.Flag) {
		if set[f.Name] {
			return
		}
		if v := env(envName(f.Name)); v != "" {
			_ = fs.Set(f.Name, v)
		}
	})
}

// newFlagSet returns a flag set that reports errors through the returned writer.
func newFlagSet(name string, errw io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("skybridge "+name, flag.ContinueOnError)
	fs.SetOutput(errw)
	return fs
}

// parseFlags parses args and applies environment twins. It returns false when
// parsing failed or --help was requested; the caller returns the given code.
func parseFlags(fs *flag.FlagSet, s *stdio, args []string) (ok bool, code int) {
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return false, ExitOK
		}
		return false, ExitUsage
	}
	applyEnv(fs, s.Env)
	return true, ExitOK
}

// parseLevel turns a --log-level value into a slog level.
func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", level)
	}
}

// newLogger builds the stderr logger for a command.
func newLogger(w io.Writer, level, format string) (*slog.Logger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: lvl}
	switch strings.ToLower(format) {
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("unknown log format %q (want text or json)", format)
	}
}

// addLogFlags registers --log-level and --log-format on fs.
func addLogFlags(fs *flag.FlagSet) (level, format *string) {
	level = fs.String("log-level", "info", "log level: debug, info, warn or error")
	format = fs.String("log-format", "text", "log format: text or json")
	return level, format
}
