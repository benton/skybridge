package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"git.h.usky.io/huskynet/skybridge/internal/version"
)

func init() {
	attach("version", runVersion)
}

func runVersion(_ context.Context, s *stdio, args []string) int {
	fs := newFlagSet("version", s.Err)
	asJSON := fs.Bool("json", false, "print as JSON")
	if ok, code := parseFlags(fs, s, args); !ok {
		return code
	}
	info := version.Get()
	if *asJSON {
		return printJSON(s, info)
	}
	fmt.Fprintln(s.Out, info.String())
	return ExitOK
}

// printJSON writes v to stdout indented, returning the exit code.
func printJSON(s *stdio, v any) int {
	enc := json.NewEncoder(s.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(s.Err, "skybridge: %v\n", err)
		return ExitUsage
	}
	return ExitOK
}
