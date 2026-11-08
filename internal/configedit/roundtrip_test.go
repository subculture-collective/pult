package configedit

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// changedLines returns the line range of a that differs from b, after
// removing the longest common prefix and suffix of lines.
func changedLines(a, b []string) (from, to int) {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return p, len(a) - s
}

// TestRoundTripStarship applies the palette table to starship's own
// Catppuccin preset: only lines inside [palettes.pult] and the
// palette key may change, and a second apply changes nothing.
func TestRoundTripStarship(t *testing.T) {
	src, err := os.ReadFile("testdata/starship-catppuccin-powerline.toml")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ path, fragment string }{
		{"palette", "palette = \"pult\"\n"},
		{"palettes.pult", "[palettes.pult]\nred = \"#ff0000\"\nmauve = \"#cba6f7\"\n"},
	} {
		out, err := TOMLKeys(src, []byte(step.fragment), []string{step.path})
		if err != nil {
			t.Fatalf("%s: %v", step.path, err)
		}
		again, err := TOMLKeys(out, []byte(step.fragment), []string{step.path})
		if err != nil || string(again) != string(out) {
			t.Fatalf("%s: second apply changed the file (err %v)", step.path, err)
		}
		a, b := splitLines(src), splitLines(out)
		from, to := changedLines(a, b)
		for _, l := range a[from:to] {
			if !strings.HasPrefix(l, "palette = ") {
				t.Errorf("%s: line %q changed", step.path, l)
			}
		}
		if step.path == "palettes.pult" && (to != from || !strings.HasSuffix(string(out), step.fragment)) {
			t.Errorf("new table was not appended unchanged: %d..%d", from, to)
		}
		src = out
	}
}

// TestRoundTripLazygit replaces gui.theme in lazygit's default config: only
// the theme block may change.
func TestRoundTripLazygit(t *testing.T) {
	src, err := os.ReadFile("testdata/lazygit-default.yml")
	if err != nil {
		t.Fatal(err)
	}
	fragment := "gui:\n  theme:\n    activeBorderColor:\n      - \"#89b4fa\"\n      - bold\n"
	out, err := YAMLKeys(src, []byte(fragment), []string{"gui.theme"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := YAMLKeys(out, []byte(fragment), []string{"gui.theme"})
	if err != nil || string(again) != string(out) {
		t.Fatalf("second apply changed the file (err %v)", err)
	}
	root, _ := yamlRoot(src)
	k, v, _ := yamlFind(root, []string{"gui", "theme"})
	a := splitLines(src)
	blockFrom, blockTo := k.Line-1, blockEnd(a, k.Line-1, k.Column, v)
	from, to := changedLines(a, splitLines(out))
	if from < blockFrom || to > blockTo {
		t.Errorf("lines %d..%d changed, outside the theme block %d..%d", from, to, blockFrom, blockTo)
	}
	var before, after map[string]any
	if yaml.Unmarshal(src, &before) != nil || yaml.Unmarshal(out, &after) != nil {
		t.Fatal("unparsable YAML")
	}
	delete(before["gui"].(map[string]any), "theme")
	delete(after["gui"].(map[string]any), "theme")
	if !yamlEqual(before, after) {
		t.Error("values outside gui.theme changed")
	}
}

func yamlEqual(a, b any) bool {
	x, _ := yaml.Marshal(a)
	y, _ := yaml.Marshal(b)
	return string(x) == string(y)
}
