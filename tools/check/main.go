// Command check runs the project's quality gates and cross builds.
//
//	go run ./tools/check [fmt|vet|static|vuln|tidy|test|cross|all]...
//
// With no argument it runs everything. It exists so developers and both CI
// pipelines run exactly the same commands.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Pinned dev-time tools; bump deliberately.
const (
	staticcheckPkg = "honnef.co/go/tools/cmd/staticcheck@v0.8.2"
	govulncheckPkg = "golang.org/x/vuln/cmd/govulncheck@v1.9.0"
)

// target is one cross-compilation target.
type target struct{ os, arch, arm string }

var targets = []target{
	{"linux", "amd64", ""}, {"linux", "arm64", ""}, {"linux", "arm", "6"},
	{"darwin", "amd64", ""}, {"darwin", "arm64", ""},
	{"windows", "amd64", ""}, {"windows", "arm64", ""},
}

var steps = []struct {
	name string
	run  func() error
}{
	{"fmt", checkFmt},
	{"vet", func() error { return run("go", "vet", "./...") }},
	{"static", func() error { return run("go", "run", staticcheckPkg, "./...") }},
	{"vuln", func() error { return run("go", "run", govulncheckPkg, "./...") }},
	{"tidy", checkTidy},
	{"test", func() error { return run("go", "test", "-race", "./...") }},
	{"cross", checkCross},
}

func main() {
	want := os.Args[1:]
	if len(want) == 0 || (len(want) == 1 && want[0] == "all") {
		want = nil
		for _, s := range steps {
			want = append(want, s.name)
		}
	}
	for _, name := range want {
		found := false
		for _, s := range steps {
			if s.name != name {
				continue
			}
			found = true
			fmt.Printf("==> %s\n", name)
			if err := s.run(); err != nil {
				fmt.Fprintf(os.Stderr, "check: %s failed: %v\n", name, err)
				os.Exit(1)
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "check: unknown step %q\n", name)
			os.Exit(1)
		}
	}
	fmt.Println("all checks passed")
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// checkFmt runs gofmt -l over the module's own package directories only, so
// a module cache or build cache kept inside the checkout (as CI does) is
// never walked.
func checkFmt() error {
	dirs, err := exec.Command("go", "list", "-f", "{{.Dir}}", "./...").Output()
	if err != nil {
		return fmt.Errorf("go list: %w", err)
	}
	args := []string{"-l"}
	for _, d := range strings.Fields(string(dirs)) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				args = append(args, filepath.Join(d, e.Name()))
			}
		}
	}
	cmd := exec.Command("gofmt", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gofmt: %w\n%s", err, stderr.String())
	}
	if files := strings.TrimSpace(stdout.String()); files != "" {
		return fmt.Errorf("files need gofmt:\n%s", files)
	}
	return nil
}

// checkTidy runs go mod tidy and fails if it changed go.mod or go.sum.
func checkTidy() error {
	before, err := readModFiles()
	if err != nil {
		return err
	}
	if err := run("go", "mod", "tidy"); err != nil {
		return err
	}
	after, err := readModFiles()
	if err != nil {
		return err
	}
	for name := range before {
		if !bytes.Equal(before[name], after[name]) {
			return fmt.Errorf("go mod tidy changed %s; commit the result", name)
		}
	}
	return nil
}

func readModFiles() (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range []string{"go.mod", "go.sum"} {
		b, err := os.ReadFile(name)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		out[name] = b
	}
	return out, nil
}

func checkCross() error {
	for _, t := range targets {
		dir := filepath.Join("dist", "check", t.os+"-"+t.arch)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		bin := filepath.Join(dir, "skybridge")
		if t.os == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/skybridge")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.os, "GOARCH="+t.arch)
		if t.arm != "" {
			cmd.Env = append(cmd.Env, "GOARM="+t.arm)
		}
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		fmt.Printf("    %s/%s\n", t.os, t.arch)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s/%s: %w", t.os, t.arch, err)
		}
	}
	return nil
}
