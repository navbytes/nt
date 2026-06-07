//go:build darwin && (production || dev)

package main

// Wails' macOS bridge (internal/frontend/desktop/darwin) references UTType from
// the UniformTypeIdentifiers framework but does not link it. On the macOS 15
// SDK that surfaces as an undefined-symbol link error
// (_OBJC_CLASS_$_UTType). Linking the framework here fixes the production/dev
// build without needing a CGO_LDFLAGS override on the command line.

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
