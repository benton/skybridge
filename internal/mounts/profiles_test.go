package mounts

import (
	"reflect"
	"testing"
)

func TestTableOrderAndDefaults(t *testing.T) {
	want := []string{"celestron-nexstar", "skywatcher-synscan", "losmandy-gemini", "meade-lx200", "ioptron", "ioptron-115200", "generic"}
	var got []string
	for _, p := range All() {
		got = append(got, p.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if _, ok := Lookup(DefaultName); !ok {
		t.Fatal("default profile missing")
	}
	for _, p := range All() {
		if p.Name == "generic" {
			if p.Baud != 0 || p.Probe != nil {
				t.Fatalf("generic: %+v", p)
			}
			continue
		}
		if p.Baud <= 0 || p.Probe == nil || p.Covers == "" {
			t.Fatalf("%s: baud %d probe %v covers %q", p.Name, p.Baud, p.Probe, p.Covers)
		}
	}
}

func TestLookupAliasesCaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"NexStar": "celestron-nexstar", "celestron": "celestron-nexstar",
		"synscan": "skywatcher-synscan", "SkyWatcher": "skywatcher-synscan",
		"LX200": "meade-lx200", "autostar": "meade-lx200", "meade": "meade-lx200",
		"gemini": "losmandy-gemini", "ioptron": "ioptron", "GENERIC": "generic",
	}
	for in, want := range cases {
		p, ok := Lookup(in)
		if !ok || p.Name != want {
			t.Errorf("Lookup(%q) = %q, %v; want %q", in, p.Name, ok, want)
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) should fail")
	}
}
