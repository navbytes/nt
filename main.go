// Command nt is a terminal task & note manager that stores data as plain files
// (todo.txt tasks + markdown notes) so editors, grep, git, and AI agents can
// read and write it directly. See SPEC.md for the design.
package main

import (
	"os"
	"runtime/debug"

	"github.com/navbytes/nt/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=..." (GoReleaser and
// `make install`). For `go install`, which doesn't apply ldflags, it falls back
// to the module version embedded by the Go toolchain.
var version = "dev"

func main() {
	cli.Version = resolveVersion()
	os.Exit(cli.Run(os.Args[1:]))
}

// resolveVersion prefers the ldflag-injected version; otherwise it reads the
// module version baked in by `go install` (e.g. "v0.1.2"), so that build is no
// longer reported as a bare "dev".
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}
