package httpserver

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// SPA serves a built single-page app: real files as they are, every other
// path as index.html so client-side routing works on a hard refresh.
func SPA(bundle fs.FS) (http.Handler, error) {
	if _, err := fs.Stat(bundle, "index.html"); err != nil {
		return nil, fmt.Errorf("ui bundle has no index.html, build the web app first: %w", err)
	}
	files := http.FileServerFS(bundle)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(bundle, name); name != "" && !strings.HasPrefix(path.Base(name), ".") && err == nil && !info.IsDir() {
			// Vite names everything under assets/ by content hash.
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, bundle, "index.html")
	}), nil
}

// DevProxy forwards UI requests, including the HMR websocket, to a Vite dev server.
func DevProxy(origin string) (http.Handler, error) {
	target, err := url.Parse(origin)
	if err != nil {
		return nil, fmt.Errorf("dev server origin: %w", err)
	}
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()
			// SetURL swaps Host for the target's; Vite's host check (DNS-rebinding guard)
			// must see what the browser sent.
			r.Out.Host = r.In.Host
		},
	}, nil
}
