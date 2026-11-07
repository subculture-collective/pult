// Package themetender holds the data built into the binary: the app
// registry, the templates and, in release builds, the catalog. It is the
// fallback when no valid signed bundle is installed.
package themetender

import (
	"bytes"
	"embed"
	"errors"
	"io"
	"io/fs"

	"github.com/klauspost/compress/zstd"

	"git.subcult.tv/subculture-collective/theme-tender/internal/bundle"
)

// Data holds registry/schema.json, registry/apps/*.yaml, templates/ and
// builtin/.
//
//go:embed registry/schema.json registry/apps/*.yaml templates builtin
var Data embed.FS

// Builtin is the built-in bundle. Its Catalog is nil in source builds.
func Builtin() (*bundle.Bundle, error) {
	reg, err := fs.Sub(Data, "registry")
	if err != nil {
		return nil, err
	}
	tmpl, err := fs.Sub(Data, "templates")
	if err != nil {
		return nil, err
	}
	b := &bundle.Bundle{Registry: reg, Templates: tmpl, Origin: "built-in", Manifest: bundle.Manifest{BundleVersion: "built-in"}}
	raw, err := Data.ReadFile("builtin/catalog.db.zst")
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	if b.Catalog, err = io.ReadAll(dec); err != nil {
		return nil, err
	}
	return b, nil
}
