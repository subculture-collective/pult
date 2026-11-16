package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/pult/internal/owner"
)

func obsidianWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, nil)
	w.sys.versions["obsidian"] = "1.11.0"
	w.opts.Wants = func(id string) bool { return id == "obsidian" }
	return w
}

func vaultRegistry(w *world, paths ...string) {
	w.t.Helper()
	vaults := map[string]any{}
	for i, p := range paths {
		vaults[string(rune('a'+i))] = map[string]any{"path": p, "open": i == 0}
	}
	b, err := json.Marshal(map[string]any{"vaults": vaults})
	if err != nil {
		w.t.Fatal(err)
	}
	w.write(".config/obsidian/obsidian.json", string(b))
}

func TestObsidianAllVaultsAndUninstall(t *testing.T) {
	w := obsidianWorld(t)
	w.write("Vaults/Notes/note.md", "# My note\nkeep these bytes\n")
	w.write("Vaults/Notes/.obsidian/workspace.json", `{"active":"keep"}`)
	w.write("Vaults/Second/.obsidian/appearance.json", "{\n  \"baseFontSize\": 19,\n  \"cssTheme\": \"Old Theme\",\n  \"theme\": \"moonstone\"\n}\n")
	w.write("Vaults/Second/second.md", "Unrelated note\n")
	first, second := filepath.Join(w.home, "Vaults/Notes"), filepath.Join(w.home, "Vaults/Second")
	vaultRegistry(w, first, second, first+"/.", filepath.Join(w.home, "removed"), "relative-vault")
	before := w.snapshot()
	p, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	if !equal(before, w.snapshot()) {
		t.Fatal("Prepare changed files")
	}
	var app AppPlan
	for _, a := range p.Apps {
		if a.ID == "obsidian" {
			app = a
		}
	}
	if app.Skip != "" || len(app.Edits) != 6 {
		t.Fatalf("expected three files in each unique live vault: skip=%q, %d edits", app.Skip, len(app.Edits))
	}
	if _, err := Execute(w.opts, p); err != nil {
		t.Fatal(err)
	}
	for _, vault := range []string{"Notes", "Second"} {
		base := "Vaults/" + vault + "/.obsidian/"
		var appearance map[string]any
		if err := json.Unmarshal([]byte(w.read(base+"appearance.json")), &appearance); err != nil {
			t.Fatal(err)
		}
		if appearance["cssTheme"] != "Pult" {
			t.Fatalf("theme not selected: %v", appearance)
		}
		if !strings.Contains(w.read(base+"themes/Pult/theme.css"), "--background-primary: #1e1e2e;") {
			t.Fatal("missing palette stylesheet")
		}
		var manifest map[string]any
		if err := json.Unmarshal([]byte(w.read(base+"themes/Pult/manifest.json")), &manifest); err != nil || manifest["name"] != "Pult" {
			t.Fatalf("missing valid theme manifest: %v, %v", manifest, err)
		}
	}
	if !strings.Contains(w.read("Vaults/Second/.obsidian/appearance.json"), `"baseFontSize": 19`) || !strings.Contains(w.read("Vaults/Second/.obsidian/appearance.json"), `"theme": "moonstone"`) {
		t.Fatal("changed unrelated vault appearance settings")
	}
	again, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range again.Apps {
		if len(a.Edits) != 0 {
			t.Fatalf("second apply changes %s", a.ID)
		}
	}
	if _, conflicts, err := Uninstall(w.opts, false); err != nil || len(conflicts) > 0 {
		t.Fatalf("uninstall: %v, %v", conflicts, err)
	}
	if !equal(before, w.snapshot()) {
		t.Fatal("uninstall did not restore every original byte, including notes and workspace")
	}
}

func TestObsidianMissingAndMalformedVaults(t *testing.T) {
	for _, kind := range []string{"no registry", "no vaults", "stale vault", "bad registry", "bad second appearance"} {
		t.Run(kind, func(t *testing.T) {
			w := obsidianWorld(t)
			switch kind {
			case "no registry":
				if err := os.Remove(filepath.Join(w.home, ".config/obsidian/obsidian.json")); err != nil {
					t.Fatal(err)
				}
			case "no vaults":
				vaultRegistry(w)
			case "stale vault":
				vaultRegistry(w, filepath.Join(w.home, "does-not-exist"))
			case "bad registry":
				w.write(".config/obsidian/obsidian.json", `{"vaults":`)
			case "bad second appearance":
				w.write("Vaults/Second/.obsidian/appearance.json", `{"cssTheme":`)
				vaultRegistry(w, filepath.Join(w.home, "Vaults/Notes"), filepath.Join(w.home, "Vaults/Second"))
			}
			before := w.snapshot()
			p, err := Prepare(w.opts, mocha(t))
			if strings.HasPrefix(kind, "bad") {
				if err == nil {
					t.Fatal("malformed metadata accepted")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Execute(w.opts, p); err != nil {
					t.Fatal(err)
				}
			}
			if !equal(before, w.snapshot()) {
				t.Fatal("unavailable or malformed vault changed files")
			}
		})
	}
}

func TestObsidianProtectedAssetAndSymlinkDedup(t *testing.T) {
	w := obsidianWorld(t)
	w.write("external/theme.css", "/* user's generated stylesheet */\n")
	asset := filepath.Join(w.home, "Vaults/Notes/.obsidian/themes/Pult")
	if err := os.MkdirAll(asset, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(w.home, "external/theme.css"), filepath.Join(asset, "theme.css")); err != nil {
		t.Fatal(err)
	}
	before := w.snapshot()
	p, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(w.opts, p); err != nil {
		t.Fatal(err)
	}
	if !equal(before, w.snapshot()) {
		t.Fatal("unconfirmed symlink asset was changed")
	}
	// With an explicitly confirmed symlink vault, two registrations of the
	// same real config directory must still produce only one set of edits.
	if err := os.Remove(filepath.Join(asset, "theme.css")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(w.home, "Alias")
	if err := os.Symlink(filepath.Join(w.home, "Vaults/Notes"), alias); err != nil {
		t.Fatal(err)
	}
	vaultRegistry(w, filepath.Join(w.home, "Vaults/Notes"), alias)
	paths, _, err := obsidianConfigs(filepath.Join(w.home, ".config/obsidian/obsidian.json"))
	if err != nil || len(paths) != 1 || !slices.Contains(paths, filepath.Join(alias, ".obsidian/appearance.json")) {
		t.Fatalf("symlink dedup: %v, %v", paths, err)
	}
	w.opts.Confirm = func(string, owner.Owner) bool { return true }
	p, err = Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range p.Apps {
		if app.ID == "obsidian" && len(app.Edits) != 3 {
			t.Fatalf("duplicate vault edits: %d", len(app.Edits))
		}
	}
	if _, err := Execute(w.opts, p); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(alias); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("vault alias replaced: %v", err)
	}
}
