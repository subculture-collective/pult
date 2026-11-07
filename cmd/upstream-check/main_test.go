package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, s string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheck(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "catalog/sources.json", `{"sources": [
		{"id": "same", "url": "https://g/same", "revision": "aaaaaaaaaaaaaaaa"},
		{"id": "moved", "url": "https://g/moved", "revision": "bbbbbbbbbbbbbbbb"},
		{"id": "gone", "url": "https://g/gone", "revision": "cccc"}]}`)
	writeFile(t, root, "registry/apps/tmux.yaml", "# Checked on Arch, tmux 3.5a, 2026-11-06: x\nschema: 1\nid: tmux\nhomepage: https://github.com/tmux/tmux/wiki\n")
	writeFile(t, root, "registry/apps/lazygit.yaml", "# Checked on Arch, lazygit 0.66.2, 2026-11-06: x\nschema: 1\nid: lazygit\nhomepage: https://github.com/jesseduffield/lazygit\n")
	writeFile(t, root, "registry/apps/ghostty.yaml", "# Not installed on the checking host.\nschema: 1\nid: ghostty\nhomepage: https://ghostty.org\n")
	writeFile(t, root, "registry/apps/newapp.yaml", "# Checked on Arch, newapp 1.0, 2026-11-06: x\nschema: 1\nid: newapp\nhomepage: https://newapp.example\n")
	remote := func(args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "https://g/same HEAD":
			return "aaaaaaaaaaaaaaaa\tHEAD\n", nil
		case "https://g/moved HEAD":
			return "dddddddddddddddd\tHEAD\n", nil
		case "--tags --refs https://github.com/tmux/tmux":
			return "x\trefs/tags/3.5\nx\trefs/tags/3.5a\nx\trefs/tags/3.6-rc\nx\trefs/tags/3.6a\nx\trefs/tags/latest\n", nil
		case "--tags --refs https://github.com/jesseduffield/lazygit":
			return "x\trefs/tags/v0.9.0\nx\trefs/tags/v0.66.2\nx\trefs/tags/v0.66.1\n", nil
		case "--tags --refs https://github.com/ghostty-org/ghostty":
			return "x\trefs/tags/tip\nx\trefs/tags/v1.2.3\n", nil
		}
		return "", errors.New("exit status 128")
	}
	got, err := Check(root, remote)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| [same](https://g/same) | `aaaaaaaaaaaa` | `aaaaaaaaaaaa` | current |",
		"| [moved](https://g/moved) | `bbbbbbbbbbbb` | `dddddddddddd` | **moved**",
		"| [gone](https://g/gone) | `cccc` | `?` | lookup failed: exit status 128 |",
		"| tmux | 3.5a | 3.6a | **newer release**",
		"| lazygit | 0.66.2 | 0.66.2 | current |",
		"| ghostty | — | 1.2.3 | never checked against an installed copy |",
		"| newapp | 1.0 | ? | no release repository known",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"0.66.10", "0.66.9", true},
		{"3.5a", "3.5", true},
		{"3.5", "3.5a", false},
		{"3.6", "3.5a", true},
		{"1.0", "1.0.0", false},
	} {
		if newer(tt.a, tt.b) != tt.want {
			t.Errorf("newer(%q, %q) != %v", tt.a, tt.b, tt.want)
		}
	}
}

// TestUpdateIssue opens the tracking issue the first time and edits it
// after that.
func TestUpdateIssue(t *testing.T) {
	var issues []map[string]any
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "token secret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var in map[string]any
		json.Unmarshal(body, &in)
		switch {
		case r.Method == "GET":
			json.NewEncoder(w).Encode(issues)
		case r.Method == "POST":
			in["number"] = 7
			issues = append(issues, in)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(in)
		case r.Method == "PATCH" && r.URL.Path == "/repo/issues/7":
			issues[0]["body"] = in["body"]
			json.NewEncoder(w).Encode(issues[0])
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	api := srv.URL + "/repo"
	if n, err := updateIssue(srv.Client(), api, "secret", "first"); err != nil || n != 7 {
		t.Fatalf("create: %d %v", n, err)
	}
	if n, err := updateIssue(srv.Client(), api, "secret", "second"); err != nil || n != 7 {
		t.Fatalf("update: %d %v", n, err)
	}
	if len(issues) != 1 || issues[0]["body"] != "second" || issues[0]["title"] != IssueTitle {
		t.Errorf("issues = %v (calls %v)", issues, calls)
	}
	if _, err := updateIssue(srv.Client(), api, "", "x"); err == nil {
		t.Error("missing token accepted")
	}
	if _, err := updateIssue(srv.Client(), api, "wrong", "x"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("bad token: %v", err)
	}
}
