package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var DistFS embed.FS

// SubFS returns the embedded frontend dist filesystem rooted at "dist".
func SubFS() fs.FS {
	sub, err := fs.Sub(DistFS, "dist")
	if err != nil {
		return DistFS
	}
	return sub
}
