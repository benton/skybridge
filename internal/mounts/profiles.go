// Package mounts holds the table of known mount families: the serial settings
// each needs and a short probe that identifies it.
package mounts

import (
	"strings"
	"time"
)

// DefaultName is the profile used when none is given.
const DefaultName = "celestron-nexstar"

// Profile describes one mount family.
type Profile struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	// Baud is the serial speed; 0 means the user must supply one (generic).
	Baud   int    `json:"baud"`
	Covers string `json:"covers"`
	Notes  string `json:"notes,omitempty"`
	// Probe identifies the mount; nil when the family has no safe probe.
	Probe *Probe `json:"-"`
}

const probeTimeout = time.Second

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var nexstarProbe = Probe{Send: "Kx", Terminator: '#', Timeout: probeTimeout, Match: func(r string) bool { return r == "x#" }}
var ioptronProbe = Probe{Send: ":MountInfo#", Length: 4, Timeout: probeTimeout, Match: allDigits}

// profiles is the table in identification order: harmless echo-style probes first.
var profiles = []Profile{
	{
		Name: "celestron-nexstar", Aliases: []string{"nexstar", "celestron"}, Baud: 9600,
		Covers: "NexStar+, NexStar SE/SLT/Evolution, CGEM, CGX and AVX hand controllers; StarSense hand controller",
		Probe:  &nexstarProbe,
	},
	{
		Name: "skywatcher-synscan", Aliases: []string{"synscan", "skywatcher"}, Baud: 9600,
		Covers: "SynScan V3/V4/V5 hand controllers; EQ6, HEQ5 and AZ-GTi over cable",
		Notes:  "SynScan speaks the NexStar dialect, so 'probe' reports it as celestron-nexstar; both profiles use the same settings.",
		Probe:  &nexstarProbe,
	},
	{
		Name: "losmandy-gemini", Aliases: []string{"gemini", "losmandy"}, Baud: 9600,
		Covers: "Gemini 1 and Gemini 2",
		Probe: &Probe{Send: ":GVP#", Terminator: '#', Timeout: probeTimeout,
			Match: func(r string) bool { return strings.Contains(r, "Gemini") }},
	},
	{
		Name: "meade-lx200", Aliases: []string{"lx200", "meade", "autostar"}, Baud: 9600,
		Covers: "LX200 Classic and GPS, Autostar, Autostar II, ETX",
		Probe: &Probe{Send: ":GVP#", Terminator: '#', Timeout: probeTimeout,
			Match: func(r string) bool { return len(r) > 1 }},
	},
	{
		Name: "ioptron", Aliases: []string{"ioptron-9600"}, Baud: 9600,
		Covers: "CEM, GEM, iEQ and SmartEQ mounts with 9600 baud firmware",
		Probe:  &ioptronProbe,
	},
	{
		Name: "ioptron-115200", Baud: 115200,
		Covers: "CEM40/70/120, GEM45 and other iOptron mounts with 115200 baud firmware",
		Probe:  &ioptronProbe,
	},
	{
		Name: "generic", Baud: 0,
		Covers: "Any other serial mount; pass --baud",
		Notes:  "No probe; 'serve --verify' and 'probe --mount generic' are not available.",
	},
}

// All returns every profile in table order.
func All() []Profile {
	out := make([]Profile, len(profiles))
	copy(out, profiles)
	return out
}

// Lookup finds a profile by name or alias, ignoring case.
func Lookup(name string) (Profile, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, p := range profiles {
		if p.Name == name {
			return p, true
		}
		for _, a := range p.Aliases {
			if a == name {
				return p, true
			}
		}
	}
	return Profile{}, false
}
