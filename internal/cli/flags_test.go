package cli

import (
	"flag"
	"io"
	"strings"
	"testing"
)

func TestApplyEnvPrecedence(t *testing.T) {
	env := func(k string) string {
		if k == "SKYBRIDGE_LISTEN" {
			return "9.9.9.9:9"
		}
		return ""
	}
	cases := []struct {
		name string
		args []string
		env  Env
		want string
	}{
		{"flag beats env", []string{"--listen", "0.0.0.0:1"}, env, "0.0.0.0:1"},
		{"env beats default", nil, env, "9.9.9.9:9"},
		{"default", nil, noEnv, "127.0.0.1:4030"},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		listen := fs.String("listen", "127.0.0.1:4030", "")
		if err := fs.Parse(c.args); err != nil {
			t.Fatal(err)
		}
		applyEnv(fs, c.env)
		if *listen != c.want {
			t.Errorf("%s: got %q want %q", c.name, *listen, c.want)
		}
	}
}

func TestApplyEnvDashesBecomeUnderscores(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	level := fs.String("log-level", "info", "")
	_ = fs.Parse(nil)
	applyEnv(fs, func(k string) string {
		if k == "SKYBRIDGE_LOG_LEVEL" {
			return "debug"
		}
		return ""
	})
	if *level != "debug" {
		t.Fatal(*level)
	}
}

func TestNewLoggerRejectsBadLevel(t *testing.T) {
	if _, err := newLogger(io.Discard, "loud", "text"); err == nil || !strings.Contains(err.Error(), "loud") {
		t.Fatal(err)
	}
	if _, err := newLogger(io.Discard, "info", "xml"); err == nil || !strings.Contains(err.Error(), "xml") {
		t.Fatal(err)
	}
	if _, err := newLogger(io.Discard, "debug", "json"); err != nil {
		t.Fatal(err)
	}
}
