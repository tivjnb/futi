// Command futi runs console utilities described in YAML modules.
package main

import (
	"os"

	"github.com/tivjnb/futi/internal/cli"
)

// version is set at release build time: -ldflags "-X main.version=<v>".
var version string

func main() {
	os.Exit(cli.Main(os.Args, os.Stdin, os.Stdout, os.Stderr, version))
}
