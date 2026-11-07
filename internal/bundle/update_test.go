package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestRelease(t *testing.T) {
	asset := func(v string) string {
		return `[{"name":"bundle-` + v + `.tar.zst","browser_download_url":"https://x/` + v + `"},` +
			`{"name":"bundle-` + v + `.tar.zst.minisig","browser_download_url":"https://x/` + v + `.minisig"}]`
	}
	listing := `[
		{"tag_name":"v0.2.0","assets":[]},
		{"tag_name":"bundle-2026.11.07.10","draft":true,"assets":` + asset("2026.11.07.10") + `},
		{"tag_name":"bundle-2026.11.07.9","prerelease":true,"assets":` + asset("2026.11.07.9") + `},
		{"tag_name":"bundle-2026.11.07.8","assets":[{"name":"bundle-2026.11.07.8.tar.zst","browser_download_url":"https://x/8"}]},
		{"tag_name":"bundle-2026.11.07.2","assets":` + asset("2026.11.07.2") + `},
		{"tag_name":"bundle-2026.11.07.6","assets":` + asset("2026.11.07.6") + `},
		{"tag_name":"bundle-x","assets":` + asset("x") + `}
	]`
	got, err := LatestRelease([]byte(listing))
	if err != nil {
		t.Fatal(err)
	}
	want := Release{"2026.11.07.6", "https://x/2026.11.07.6", "https://x/2026.11.07.6.minisig"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := LatestRelease([]byte(`[]`)); err == nil {
		t.Error("empty listing accepted")
	}
	if _, err := LatestRelease([]byte(`{"message":"not found"}`)); err == nil {
		t.Error("error object accepted")
	}
}

func TestNewer(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"2026.11.07.10", "2026.11.07.9", true},
		{"2026.11.07.9", "2026.11.07.10", false},
		{"2026.11.07.6", "2026.11.07.6", false},
		{"2026.11.07.6.1", "2026.11.07.6", true},
		{"2026.11.07.6", "built-in", true},
		{"built-in", "2026.11.07.6", false},
		{"", "", false},
	} {
		if got := Newer(tt.a, tt.b); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}

func TestInstall(t *testing.T) {
	data := build(t) // manifest version 2026.11.07.1
	key, other := newKey(t, 1), newKey(t, 9)
	sig, err := Sign(data, key.secret, "", "ok")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "share", "bundle.tar.zst")

	failures := []struct {
		name      string
		data, sig []byte
		keys      []string
		version   string
		want      string
	}{
		{"untrusted key", data, sig, []string{other.public}, "2026.11.07.1", "untrusted key"},
		{"tampered", flip(data, len(data)/2), sig, []string{key.public}, "2026.11.07.1", "Invalid signature"},
		{"version does not match the tag", data, sig, []string{key.public}, "2026.11.07.2", "holds bundle version"},
	}
	for _, tt := range failures {
		if _, err := Install(path, tt.data, tt.sig, tt.keys, tt.version); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s: bundle written after a failed check", tt.name)
		}
	}

	b, err := Install(path, data, sig, []string{key.public}, "2026.11.07.1")
	if err != nil {
		t.Fatal(err)
	}
	if b.Origin != path || b.Manifest.BundleVersion != "2026.11.07.1" {
		t.Errorf("installed %+v", b.Manifest)
	}
	got, warn := Load(path, []string{key.public}, &Bundle{Origin: "built-in"})
	if warn != "" || got.Origin != path {
		t.Errorf("load after install: %q %s", warn, got.Origin)
	}
	// A failed install keeps what was there.
	if _, err := Install(path, flip(data, 10), sig, []string{key.public}, "2026.11.07.1"); err == nil {
		t.Fatal("tampered bundle installed")
	}
	if on, _ := os.ReadFile(path); !bytes.Equal(on, data) {
		t.Error("installed bundle changed by a failed install")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 2 {
		t.Errorf("leftover files: %v", entries)
	}
}
