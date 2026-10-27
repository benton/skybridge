package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMountsTable(t *testing.T) {
	var out bytes.Buffer
	if code := Main([]string{"mounts"}, &out, &out, noEnv); code != 0 {
		t.Fatal(out.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("want header + 7 rows, got %d: %s", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], "NAME") || !strings.Contains(lines[0], "PROBE") {
		t.Fatal(lines[0])
	}
	if !strings.HasPrefix(lines[1], "celestron-nexstar") || !strings.Contains(lines[1], "9600") || !strings.Contains(lines[1], "yes") {
		t.Fatal(lines[1])
	}
	last := lines[7]
	if !strings.HasPrefix(last, "generic") || !strings.Contains(last, "--baud") || !strings.Contains(last, "no") {
		t.Fatal(last)
	}
}

func TestMountsJSON(t *testing.T) {
	var out bytes.Buffer
	if code := Main([]string{"mounts", "--json"}, &out, &out, noEnv); code != 0 {
		t.Fatal(out.String())
	}
	var got []struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
		Baud    int      `json:"baud"`
		Probe   bool     `json:"probe"`
		Covers  string   `json:"covers"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err, out.String())
	}
	if len(got) != 7 || !got[0].Probe || got[6].Probe || got[0].Name != "celestron-nexstar" || got[0].Covers == "" {
		t.Fatalf("%+v", got)
	}
}
