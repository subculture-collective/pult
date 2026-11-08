package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultReleases is the Gitea API listing of Pult's releases.
// Bundle releases are tagged bundle-<version> by bundle-release.yml and
// carry two assets: bundle-<version>.tar.zst and its .minisig.
const DefaultReleases = "https://git.subcult.tv/api/v1/repos/subculture-collective/pult/releases?limit=50"

// Release is a published bundle.
type Release struct {
	Version   string
	BundleURL string
	SigURL    string
}

// LatestRelease picks the newest bundle release from a Gitea release
// listing. Drafts, prereleases, other tags and releases missing either
// asset are skipped; the listing's order does not matter.
func LatestRelease(listing []byte) (Release, error) {
	var rels []struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(listing, &rels); err != nil {
		return Release{}, fmt.Errorf("bundle: release listing: %w", err)
	}
	var best Release
	for _, r := range rels {
		v, ok := strings.CutPrefix(r.Tag, "bundle-")
		if !ok || r.Draft || r.Prerelease || !validVersion(v) {
			continue
		}
		cand := Release{Version: v}
		for _, a := range r.Assets {
			switch a.Name {
			case "bundle-" + v + ".tar.zst":
				cand.BundleURL = a.URL
			case "bundle-" + v + ".tar.zst.minisig":
				cand.SigURL = a.URL
			}
		}
		if cand.BundleURL != "" && cand.SigURL != "" && (best.Version == "" || Newer(v, best.Version)) {
			best = cand
		}
	}
	if best.Version == "" {
		return Release{}, fmt.Errorf("bundle: no published bundle release found")
	}
	return best, nil
}

// Newer reports whether bundle version a is newer than b. Versions are
// dotted numbers (2026.11.07.6). A version that is not, such as
// "built-in", is older than every numbered one.
func Newer(a, b string) bool {
	if !validVersion(a) {
		return false
	}
	if !validVersion(b) {
		return true
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func validVersion(v string) bool {
	if v == "" {
		return false
	}
	for _, p := range strings.Split(v, ".") {
		if _, err := strconv.Atoi(p); err != nil || strings.HasPrefix(p, "-") || strings.HasPrefix(p, "+") {
			return false
		}
	}
	return true
}

// Install checks a downloaded bundle and its signature the same way Load
// does, checks that its manifest names version, and only then writes path
// and path.minisig. Each file is written to a temporary name and renamed,
// so a failure leaves the installed bundle as it was. A crash between the
// two renames leaves a pair that does not verify; Load then falls back to
// the built-in bundle until the next update.
func Install(path string, data, sig []byte, keys []string, version string) (*Bundle, error) {
	if err := Verify(data, sig, keys); err != nil {
		return nil, err
	}
	b, err := Read(data)
	if err != nil {
		return nil, err
	}
	if b.Manifest.BundleVersion != version {
		return nil, fmt.Errorf("bundle: release %s holds bundle version %q", version, b.Manifest.BundleVersion)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmpData, err := writeTemp(path, data)
	if err != nil {
		return nil, err
	}
	tmpSig, err := writeTemp(path+".minisig", sig)
	if err != nil {
		os.Remove(tmpData)
		return nil, err
	}
	if err := os.Rename(tmpData, path); err != nil {
		os.Remove(tmpData)
		os.Remove(tmpSig)
		return nil, err
	}
	if err := os.Rename(tmpSig, path+".minisig"); err != nil {
		os.Remove(tmpSig)
		return nil, err
	}
	b.Origin = path
	return b, nil
}

func writeTemp(path string, b []byte) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o644)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
