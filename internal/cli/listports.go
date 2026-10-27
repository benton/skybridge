package cli

import (
	"context"
	"fmt"
	"runtime"
	"text/tabwriter"

	"git.h.usky.io/huskynet/skybridge/internal/discovery"
	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

// listPorts enumerates serial ports; tests replace it.
var listPorts = serial.List

func init() {
	attach("list-ports", runListPorts)
}

func runListPorts(ctx context.Context, s *stdio, args []string) int {
	fs := newFlagSet("list-ports", s.Err)
	asJSON := fs.Bool("json", false, "print as JSON")
	if ok, code := parseFlags(fs, s, args); !ok {
		return code
	}
	ports, err := listPorts(ctx)
	if err != nil {
		fmt.Fprintf(s.Err, "skybridge: %v\n", err)
		return ExitUsage
	}
	ranked := discovery.Rank(runtime.GOOS, ports)
	if *asJSON {
		if ranked == nil {
			ranked = []discovery.Candidate{}
		}
		return printJSON(s, ranked)
	}
	if len(ranked) == 0 {
		fmt.Fprintln(s.Err, "no serial ports found (is the mount on and the cable plugged in?)")
		return ExitOK
	}
	tw := tabwriter.NewWriter(s.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tADAPTER\tVID:PID\tPRODUCT")
	for _, c := range ranked {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Path, dash(c.Adapter), vidPID(c.VID, c.PID), dash(c.Product))
	}
	tw.Flush()
	return ExitOK
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func vidPID(vid, pid uint16) string {
	if vid == 0 {
		return "-"
	}
	return fmt.Sprintf("%04x:%04x", vid, pid)
}
