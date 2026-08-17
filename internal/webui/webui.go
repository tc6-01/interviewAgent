package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist
var assets embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return &spaHandler{fs: dist, files: http.FileServer(http.FS(dist))}
}

type spaHandler struct {
	fs    fs.FS
	files http.Handler
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	if _, err := fs.Stat(h.fs, name); err != nil {
		name = "index.html"
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	request := r.Clone(r.Context())
	if name == "index.html" {
		request.URL.Path = "/"
	} else {
		request.URL.Path = "/" + name
	}
	h.files.ServeHTTP(w, request)
}
