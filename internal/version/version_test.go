package version

import (
	"runtime"
	"testing"
)

func TestGetDefaults(t *testing.T) {
	i := Get()
	if i.Version != "dev" || i.Commit != "none" || i.Date != "unknown" {
		t.Fatalf("defaults: %+v", i)
	}
	if i.Go != runtime.Version() || i.OS != runtime.GOOS || i.Arch != runtime.GOARCH {
		t.Fatalf("runtime: %+v", i)
	}
}

func TestString(t *testing.T) {
	s := Info{Version: "v1.2.3", Commit: "abc1234", Date: "2026-10-27", Go: "go1.26.6", OS: "linux", Arch: "arm64"}.String()
	if s != "skybridge v1.2.3 (abc1234, 2026-10-27) go1.26.6 linux/arm64" {
		t.Fatal(s)
	}
}
