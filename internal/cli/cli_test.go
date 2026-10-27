package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestVersionCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Main([]string{"version"}, &out, &errb, noEnv); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.HasPrefix(out.String(), "skybridge dev") {
		t.Fatal(out.String())
	}
}

func TestVersionJSON(t *testing.T) {
	var out bytes.Buffer
	if code := Main([]string{"version", "--json"}, &out, io.Discard, noEnv); code != 0 {
		t.Fatal(code)
	}
	var v map[string]string
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v["version"] != "dev" {
		t.Fatal(out.String(), err)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	var errb bytes.Buffer
	if code := Main([]string{"bogus"}, io.Discard, &errb, noEnv); code != ExitUsage {
		t.Fatal(code)
	}
	if !strings.Contains(errb.String(), `unknown command "bogus"`) {
		t.Fatal(errb.String())
	}
}

func TestHelpListsCommands(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}} {
		var out bytes.Buffer
		if code := Main(args, &out, io.Discard, noEnv); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
		for _, name := range []string{"serve", "list-ports", "probe", "mounts", "version"} {
			if !strings.Contains(out.String(), name) {
				t.Fatalf("%v: missing %q in %s", args, name, out.String())
			}
		}
	}
}
