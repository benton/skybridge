// Command skybridge bridges a telescope hand controller's serial port to TCP for SkySafari.
package main

import (
	"os"

	"git.h.usky.io/huskynet/skybridge/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}
