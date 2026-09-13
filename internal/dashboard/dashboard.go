// Package dashboard serves the SvelteKit build that `npm run build` in web/
// writes into this directory, embedded in the binary.
package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:build
var files embed.FS

// Handler serves the built dashboard.
func Handler() http.Handler {
	site, _ := fs.Sub(files, "build")
	return http.FileServerFS(site)
}
