package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"git.h.usky.io/huskynet/skybridge/internal/mounts"
)

func init() {
	attach("mounts", runMounts)
}

// mountJSON is the wire form of a profile; the probe itself is not data.
type mountJSON struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Baud    int      `json:"baud"`
	Probe   bool     `json:"probe"`
	Covers  string   `json:"covers"`
	Notes   string   `json:"notes,omitempty"`
}

func runMounts(_ context.Context, s *stdio, args []string) int {
	fs := newFlagSet("mounts", s.Err)
	asJSON := fs.Bool("json", false, "print as JSON")
	if ok, code := parseFlags(fs, s, args); !ok {
		return code
	}
	all := mounts.All()
	if *asJSON {
		out := make([]mountJSON, 0, len(all))
		for _, p := range all {
			aliases := p.Aliases
			if aliases == nil {
				aliases = []string{}
			}
			out = append(out, mountJSON{p.Name, aliases, p.Baud, p.Probe != nil, p.Covers, p.Notes})
		}
		return printJSON(s, out)
	}
	tw := tabwriter.NewWriter(s.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tBAUD\tPROBE\tCOVERS")
	for _, p := range all {
		baud := fmt.Sprint(p.Baud)
		if p.Baud == 0 {
			baud = "--baud"
		}
		probe := "no"
		if p.Probe != nil {
			probe = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, baud, probe, p.Covers)
	}
	tw.Flush()
	return ExitOK
}
