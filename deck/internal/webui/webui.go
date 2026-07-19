package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist contains the static SvelteKit build. A small placeholder is committed so
// the Go packages remain buildable before the first frontend build.
//
//go:embed dist/*
var assets embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requested == "." || requested == "" {
			requested = "index.html"
		}
		if _, err := fs.Stat(dist, requested); err != nil {
			r.URL.Path = "/"
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
			return
		}
		if strings.Contains(requested, "/_app/") || strings.HasPrefix(requested, "_app/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
