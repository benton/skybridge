// Command skybridge bridges a telescope hand controller's serial port to TCP for SkySafari.
package main

import (
	"fmt"

	"git.h.usky.io/huskynet/skybridge/internal/version"
)

func main() {
	fmt.Println(version.Get().String())
}
