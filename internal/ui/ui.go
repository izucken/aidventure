// Package ui carries the built browser app inside the binary.
package ui

import (
	"embed"
	"io/fs"
)

// Vite writes into static/dist; static/ itself holds only the .gitkeep that
// keeps the directory embeddable before the first build.
//
//go:embed all:static
var static embed.FS

func Bundle() (fs.FS, error) {
	return fs.Sub(static, "static/dist")
}
