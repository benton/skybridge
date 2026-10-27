// Package cli implements the skybridge command line: dispatch, flags and commands.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
)

// Exit codes, as fixed by the design document.
const (
	ExitOK     = 0
	ExitUsage  = 1
	ExitListen = 2
	ExitProbe  = 3
)

// Env looks up an environment variable; os.Getenv in production.
type Env func(key string) string

// stdio is what a command gets to talk to the world.
type stdio struct {
	Out io.Writer
	Err io.Writer
	Env Env
}

// command is one subcommand.
type command struct {
	Name    string
	Summary string
	Run     func(ctx context.Context, s *stdio, args []string) int
}

// rootCtx, when non-nil, replaces the signal-driven root context. Tests set it.
var rootCtx context.Context

// commands lists every subcommand in help order. Each command's Run is attached
// by its own file's init; a command without a Run is not yet available.
var commands = []*command{
	{Name: "serve", Summary: "relay the hand controller's serial port to TCP (default)"},
	{Name: "gui", Summary: "serve and open the control panel in a browser"},
	{Name: "list-ports", Summary: "list serial ports and the USB adapters behind them"},
	{Name: "probe", Summary: "talk to the mount and identify it"},
	{Name: "mounts", Summary: "list the known mount profiles"},
	{Name: "version", Summary: "print version and build information"},
}

// attach installs run as the implementation of the named command.
func attach(name string, run func(ctx context.Context, s *stdio, args []string) int) {
	for _, c := range commands {
		if c.Name == name {
			c.Run = run
			return
		}
	}
	panic("cli: attach to unknown command " + name)
}

func lookup(name string) *command {
	for _, c := range commands {
		if c.Name == name && c.Run != nil {
			return c
		}
	}
	return nil
}

// Main runs the command line and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer, env Env) int {
	s := &stdio{Out: stdout, Err: stderr, Env: env}
	name := "serve"
	if len(args) > 0 {
		name = args[0]
		args = args[1:]
	}
	switch name {
	case "help", "--help", "-h":
		printHelp(s.Out)
		return ExitOK
	}
	c := lookup(name)
	if c == nil {
		fmt.Fprintf(s.Err, "skybridge: unknown command %q\n\n", name)
		printHelp(s.Err)
		return ExitUsage
	}
	ctx := rootCtx
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	return c.Run(ctx, s, args)
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "usage: skybridge <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Running skybridge with no command is the same as 'skybridge serve'.")
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(tw, "  %s\t%s\n", c.Name, c.Summary)
	}
	fmt.Fprintf(tw, "  %s\t%s\n", "help", "show this help")
	tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'skybridge <command> --help' for that command's flags.")
}
