// Command bundle-build packs the catalog, registry and templates into a
// data bundle and signs it with minisign. It runs in the release job; see
// docs/runbooks/bundle-signing.md.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"

	"git.subcult.tv/subculture-collective/theme-tender/internal/bundle"
	"git.subcult.tv/subculture-collective/theme-tender/internal/catalog"
)

func main() {
	cat := flag.String("catalog", ".cache/catalog.db.zst", "catalog built by catalog-build")
	sources := flag.String("manifest", "catalog/sources.json", "sources manifest the catalog was built from")
	version := flag.String("version", "", "bundle version, for example 2026.11.07.1 (required)")
	out := flag.String("out", "", "output file (default dist/bundle-<version>.tar.zst)")
	key := flag.String("sign", "", "minisign secret key file; its password is read from $TENDER_SIGNING_PASSWORD")
	flag.Parse()
	if err := run(*cat, *sources, *version, *out, *key); err != nil {
		fmt.Fprintln(os.Stderr, "bundle-build:", err)
		os.Exit(1)
	}
}

func run(catPath, sourcesPath, version, out, key string) error {
	if !bundle.ValidVersion(version) {
		return fmt.Errorf("-version %q: want YYYY.MM.DD.N, such as 2026.11.07.1", version)
	}
	if out == "" {
		out = "dist/bundle-" + version + ".tar.zst"
	}
	f, err := os.Open(catPath)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	db, err := io.ReadAll(dec)
	dec.Close()
	if err != nil {
		return err
	}
	src, err := catalog.ReadManifest(sourcesPath)
	if err != nil {
		return err
	}
	m := bundle.Manifest{BundleVersion: version, CatalogSchema: bundle.CatalogSchema, RegistrySchema: bundle.RegistrySchema}
	for _, s := range src.Sources {
		m.Sources = append(m.Sources, bundle.Source{ID: s.ID, Revision: s.Revision})
	}
	var buf bytes.Buffer
	if err := bundle.Build(&buf, m, db, os.DirFS(".")); err != nil {
		return err
	}
	if err := os.MkdirAll(dirOf(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Println(out)
	if key == "" {
		return nil
	}
	sig, err := bundle.Sign(buf.Bytes(), key, os.Getenv("TENDER_SIGNING_PASSWORD"), "theme-tender bundle "+version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out+".minisig", sig, 0o644); err != nil {
		return err
	}
	fmt.Println(out + ".minisig")
	return nil
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
