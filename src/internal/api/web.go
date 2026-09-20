// Package api serves the localhost JSON API, hook receiver, and embedded UI.
package api

import (
	"io/fs"
	"net/http"
	"strings"
)

const (
	indexCacheControl = "no-store, max-age=0"
	assetCacheControl = "public, max-age=31536000, immutable"
)

// webHandler serves content-hashed assets with immutable caching while ensuring
// every document navigation receives the current asset manifest.
func webHandler(assets fs.FS) http.Handler {
	static := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") && strings.Contains(pathBase(r.URL.Path), ".") {
			assetName := strings.TrimPrefix(r.URL.Path, "/")
			if _, err := fs.Stat(assets, assetName); err == nil {
				w.Header().Set("Cache-Control", assetCacheControl)
			} else {
				w.Header().Set("Cache-Control", indexCacheControl)
				http.NotFound(w, r)
				return
			}
			static.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", indexCacheControl)
		w.Header().Set("Pragma", "no-cache")
		if strings.Contains(pathBase(r.URL.Path), ".") {
			static.ServeHTTP(w, r)
			return
		}

		request := r.Clone(r.Context())
		urlCopy := *r.URL
		urlCopy.Path = "/"
		request.URL = &urlCopy
		static.ServeHTTP(w, request)
	})
}

func pathBase(path string) string {
	path = strings.TrimSuffix(path, "/")
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
