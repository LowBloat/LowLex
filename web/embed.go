// Package web embeds the built web UI (web/build, produced by `bun run build`).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// FS returns the UI files, or nil when the binary was built without them.
func FS() fs.FS {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
