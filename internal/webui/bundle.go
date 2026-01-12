package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

// dist embeds the built web UI assets.
//
//go:embed dist/*
var dist embed.FS

// Bundle exposes embedded web UI assets for serving.
type Bundle struct {
	DistFS    fs.FS           // Root dist filesystem.
	AssetsFS  http.FileSystem // Assets subdirectory filesystem.
	IndexHTML []byte          // Raw index HTML content.
}

// Load loads the embedded web UI bundle from the dist filesystem.
func Load() (Bundle, error) {
	distFS, errSub := fs.Sub(dist, "dist")
	if errSub != nil {
		return Bundle{}, errSub
	}
	assetsFS, errSubAssets := fs.Sub(dist, "dist/assets")
	if errSubAssets != nil {
		return Bundle{}, errSubAssets
	}
	indexHTML, errReadFile := dist.ReadFile("dist/index.html")
	if errReadFile != nil {
		return Bundle{}, errReadFile
	}
	return Bundle{
		DistFS:    distFS,
		AssetsFS:  http.FS(assetsFS),
		IndexHTML: indexHTML,
	}, nil
}
