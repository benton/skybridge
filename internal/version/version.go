// Package version reports build information injected at link time.
package version

import (
	"fmt"
	"runtime"
)

// Set with -ldflags "-X git.h.usky.io/huskynet/skybridge/internal/version.Version=..." at release time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Info describes the running binary.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// Get returns the build information for the running binary.
func Get() Info {
	return Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

// String formats the information on one line.
func (i Info) String() string {
	return fmt.Sprintf("skybridge %s (%s, %s) %s %s/%s", i.Version, i.Commit, i.Date, i.Go, i.OS, i.Arch)
}
