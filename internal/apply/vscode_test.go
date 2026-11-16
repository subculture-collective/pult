package apply

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestVSCodeApplyAndUninstall(t *testing.T) {
	const file = ".config/Code/User/settings.json"
	const original = "\ufeff{\r\n\t// My editor settings\r\n\t\"editor.fontSize\": 17,\r\n\t\"files.autoSave\": \"off\",\r\n\t\"workbench.colorCustomizations\": {\r\n\t\t\"extension.customColor\": \"#123456\", // leave this alone\r\n\t\t\"editor.background\": \"#111111\",\r\n\t},\r\n}\r\n"
	w := newWorld(t, map[string]string{file: original})
	w.opts.Wants = func(id string) bool { return id == "vscode" }
	before := w.snapshot()
	p, err := Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	if !equal(before, w.snapshot()) {
		t.Fatal("planning changed files")
	}
	if _, err := Execute(w.opts, p); err != nil {
		t.Fatal(err)
	}
	got := w.read(file)
	for _, keep := range []string{"\ufeff{\r\n\t// My editor settings\r\n\t\"editor.fontSize\": 17,", "\"files.autoSave\": \"off\",", "\"extension.customColor\": \"#123456\", // leave this alone"} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost %q in\n%s", keep, got)
		}
	}
	if bytes.Contains(bytes.ReplaceAll([]byte(got), []byte("\r\n"), nil), []byte("\n")) {
		t.Error("changed newline style")
	}
	doc, err := hujson.Parse(bytes.TrimPrefix([]byte(got), []byte("\ufeff")))
	if err != nil {
		t.Fatal(err)
	}
	doc.Standardize()
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(doc.Pack(), &settings); err != nil {
		t.Fatal(err)
	}
	var colors map[string]string
	if err := json.Unmarshal(settings["workbench.colorCustomizations"], &colors); err != nil {
		t.Fatal(err)
	}
	if colors["editor.background"] != "#1e1e2e" || colors["terminal.ansiBlue"] != "#89b4fa" || colors["focusBorder"] == "" {
		t.Fatalf("palette not applied: %v", colors)
	}
	if string(settings["workbench.colorTheme"]) != `"Default Dark Modern"` || settings["editor.tokenColorCustomizations"] == nil || settings["editor.semanticTokenColorCustomizations"] == nil {
		t.Errorf("missing base theme or syntax settings: %s", doc.Pack())
	}
	// The next apply must be empty, including the many separately owned keys.
	p, err = Prepare(w.opts, mocha(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range p.Apps {
		if len(app.Edits) != 0 {
			t.Fatalf("second apply edits %s", app.ID)
		}
	}
	if _, conflicts, err := Uninstall(w.opts, false); err != nil || len(conflicts) != 0 {
		t.Fatalf("uninstall: %v, %v", conflicts, err)
	}
	if !equal(before, w.snapshot()) {
		t.Fatal("uninstall did not restore the original bytes")
	}
}

func TestVSCodeInvalidSettingsStopsPlanning(t *testing.T) {
	w := newWorld(t, map[string]string{".config/Code/User/settings.json": `{"editor.fontSize":14,"editor.fontSize":16}`})
	before := w.snapshot()
	if _, err := Prepare(w.opts, mocha(t)); err == nil || !strings.Contains(err.Error(), "duplicate JSON key") {
		t.Fatalf("accepted ambiguous settings: %v", err)
	}
	if !equal(before, w.snapshot()) {
		t.Fatal("failed planning changed files")
	}
}
