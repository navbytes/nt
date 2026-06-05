// Command nt is a terminal task & note manager that stores data as plain files
// (todo.txt tasks + markdown notes) so editors, grep, git, and AI agents can
// read and write it directly. See SPEC.md for the design.
package main

import (
	"os"

	"github.com/navbytes/nt/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cli.Version = version
	os.Exit(cli.Run(os.Args[1:]))
}
