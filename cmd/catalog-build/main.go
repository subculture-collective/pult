// Command catalog-build builds the zstd-compressed catalog.db from the
// pinned sources in catalog/sources.json. It runs in CI; see
// docs/reference/catalog-schema.md.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"

	"git.subcult.tv/subculture-collective/theme-tender/internal/catalog"
)

func main() {
	manifest := flag.String("manifest", "catalog/sources.json", "sources manifest")
	src := flag.String("src", ".cache/catalog-src", "directory holding one checkout per source")
	out := flag.String("out", ".cache/catalog.db.zst", "output file")
	fetch := flag.Bool("fetch", false, "check out the pinned sources into -src first (needs git and network)")
	flag.Parse()
	if err := run(*manifest, *src, *out, *fetch); err != nil {
		fmt.Fprintln(os.Stderr, "catalog-build:", err)
		os.Exit(1)
	}
}

func run(manifestPath, src, out string, fetch bool) error {
	m, err := catalog.ReadManifest(manifestPath)
	if err != nil {
		return err
	}
	if fetch {
		if err := catalog.Fetch(m, src); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(".", ".catalog-build-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	st, err := catalog.Build(m, src, io.MultiWriter(f, h))
	if err == nil {
		err = f.Chmod(0o644)
	}
	if err == nil {
		err = f.Close()
	}
	if err != nil {
		f.Close()
		return err
	}
	if err := os.Rename(f.Name(), out); err != nil {
		return err
	}
	fmt.Printf("%s sha256 %x\nthemes %d: %d bundled, %d listed without colors; %d nonfree left out; %d ports\n",
		out, h.Sum(nil), st.Themes, st.Bundled, st.Listed, st.Excluded, st.Ports)
	return nil
}
