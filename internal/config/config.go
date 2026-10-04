// Package config resolves the server's runtime settings.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

type Config struct {
	// Addr is the listen address, for example ":8080".
	Addr string
	// UIDevServer is the origin of a running Vite dev server. When set, UI requests
	// are proxied there so the browser gets live reload. When empty, the bundle
	// embedded in the binary is served.
	UIDevServer string
	// DataDir holds everything the app persists. It sits next to the binary, so
	// an install is one directory that moves as a whole.
	DataDir string
}

func Load() (Config, error) {
	exe, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("locate binary: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return Config{}, fmt.Errorf("locate binary: %w", err)
	}

	cfg := Config{
		Addr:        envOr("AIDVENTURE_ADDR", ":8080"),
		UIDevServer: os.Getenv("AIDVENTURE_UI_DEV_SERVER"),
		DataDir:     filepath.Join(filepath.Dir(exe), "data"),
	}
	if cfg.UIDevServer != "" {
		if _, err := url.ParseRequestURI(cfg.UIDevServer); err != nil {
			return Config{}, fmt.Errorf("AIDVENTURE_UI_DEV_SERVER: %w", err)
		}
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
