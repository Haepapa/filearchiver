package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var assets embed.FS

// SubFS returns a sub-filesystem rooted at the "web" directory so callers
// can serve it directly without the "web/" path prefix.
func SubFS() fs.FS {
	sub, err := fs.Sub(assets, "web")
	if err != nil {
		panic("webui: failed to create sub-filesystem: " + err.Error())
	}
	return sub
}
