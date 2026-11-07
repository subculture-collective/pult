package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"git.subcult.tv/subculture-collective/theme-tender/internal/catalog"
	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
)

type fake struct {
	themes  []catalog.Info
	applied []string
	undone  int
}

func newFake() *fake {
	return &fake{themes: []catalog.Info{
		{ID: "catppuccin-latte", Name: "Catppuccin Latte", Bundled: true, Tags: []string{"light", "warm"}},
		{ID: "catppuccin-mocha", Name: "Catppuccin Mocha", Bundled: true, Tags: []string{"cool", "dark", "pastel"}},
		{ID: "solarized-dark", Name: "Solarized Dark", Bundled: true, Tags: []string{"dark", "muted"}},
		{ID: "solarized-light", Name: "Solarized Light", Bundled: true, Tags: []string{"light", "muted"}},
		{ID: "unknown-one", Name: "Unknown One", License: "unknown", Tags: []string{"dark"}},
	}}
}

func (f *fake) Search(q string, tags []string) ([]catalog.Info, error) {
	var out []catalog.Info
	for _, th := range f.themes {
		ok := strings.Contains(strings.ToLower(th.Name), strings.ToLower(q))
		for _, t := range tags {
			ok = ok && contains(th.Tags, t)
		}
		if ok {
			out = append(out, th)
		}
	}
	return out, nil
}

func (f *fake) Similar(id string) ([]catalog.Info, error) {
	var out []catalog.Info
	for _, th := range f.themes {
		if th.ID != id && th.Bundled {
			out = append(out, th)
		}
	}
	return out[:2], nil
}

func (f *fake) Palette(id string) (color.Palette, error) {
	for _, th := range f.themes {
		if th.ID == id && !th.Bundled {
			return nil, errors.New("not bundled")
		}
	}
	p := color.Palette{color.Background: color.MustHex("#1e1e2e"), color.Foreground: color.MustHex("#cdd6f4")}
	for i := range color.ANSIRoles {
		p[color.ANSIRoles[i]] = color.MustHex([]string{"#45475a", "#f38ba8", "#a6e3a1", "#f9e2af", "#89b4fa", "#f5c2e7", "#94e2d5", "#bac2de"}[i%8])
	}
	full, _, err := color.Derive(p)
	return full, err
}

func (f *fake) Apps() ([]AppRow, error) {
	return []AppRow{
		{ID: "btop", Tier: 1, Owner: "none", Status: "version 1.4.7, owned"},
		{ID: "kitty", Tier: 1, Owner: "omarchy", Status: "Omarchy themes it"},
		{ID: "tmux", Tier: 1, Owner: "stow", Status: "owned by stow; asked in tender apply"},
	}, nil
}

func (f *fake) Plan(id string) ([]string, error) {
	return []string{"btop       2 file(s)", "kitty      skip: Omarchy themes it"}, nil
}

func (f *fake) Apply(id string) (string, error) {
	f.applied = append(f.applied, id)
	return "Applied " + id + ": 2 file(s) changed.", nil
}

func (f *fake) Undo() (string, error) {
	f.undone++
	return "Rolled back the last apply.", nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

var (
	enter = tea.KeyPressMsg{Code: tea.KeyEnter}
	tab   = tea.KeyPressMsg{Code: tea.KeyTab}
	esc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	down  = tea.KeyPressMsg{Code: tea.KeyDown}
)

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

// run drives the program with msgs, quits, and returns the final view and
// the fake.
func run(t *testing.T, msgs ...tea.Msg) (string, *fake) {
	t.Helper()
	f := newFake()
	tm := teatest.NewTestModel(t, New(f), teatest.WithInitialTermSize(100, 30))
	for _, m := range msgs {
		tm.Send(m)
	}
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(Model)
	return final.View().Content, f
}

func TestBrowse(t *testing.T) {
	out, _ := run(t, down)
	golden.RequireEqual(t, out)
}

func TestSearch(t *testing.T) {
	out, _ := run(t, key("/"), key("s"), key("o"), key("l"), enter)
	if !strings.Contains(out, "solarized-dark") || strings.Contains(out, "catppuccin-mocha") {
		t.Fatalf("search did not filter:\n%s", out)
	}
	golden.RequireEqual(t, out)
}

func TestTagFilter(t *testing.T) {
	out, _ := run(t, key("2"), key("5"))
	if !strings.Contains(out, "solarized-light") || strings.Contains(out, "catppuccin-latte") {
		t.Fatalf("light+muted filter:\n%s", out)
	}
	golden.RequireEqual(t, out)
}

func TestSimilar(t *testing.T) {
	out, _ := run(t, down, key("s"))
	if !strings.Contains(out, "Themes closest to catppuccin-mocha") {
		t.Fatalf("similar:\n%s", out)
	}
	golden.RequireEqual(t, out)
}

func TestUnbundledPreview(t *testing.T) {
	out, _ := run(t, down, down, down, down)
	if !strings.Contains(out, "is not bundled (licence unknown)") {
		t.Fatalf("unbundled preview:\n%s", out)
	}
}

func TestApps(t *testing.T) {
	out, _ := run(t, tab)
	golden.RequireEqual(t, out)
}

func TestApplyConfirmAndUndo(t *testing.T) {
	out, f := run(t, down, enter)
	if !strings.Contains(out, "Apply Catppuccin Mocha?") || len(f.applied) != 0 {
		t.Fatalf("confirm screen:\n%s", out)
	}
	golden.RequireEqual(t, out)

	out, f = run(t, down, enter, key("y"))
	if len(f.applied) != 1 || f.applied[0] != "catppuccin-mocha" || !strings.Contains(out, "Applied catppuccin-mocha") {
		t.Fatalf("apply: %v\n%s", f.applied, out)
	}
	out, f = run(t, down, enter, key("n"))
	if len(f.applied) != 0 || !strings.Contains(out, "Nothing changed.") {
		t.Fatalf("declined apply: %v\n%s", f.applied, out)
	}
	out, f = run(t, key("u"), key("y"))
	if f.undone != 1 || !strings.Contains(out, "Rolled back the last apply.") {
		t.Fatalf("undo: %d\n%s", f.undone, out)
	}
}

// TestEmptyListKeys: moving, applying and asking for neighbours with no
// themes listed does nothing instead of crashing.
func TestEmptyListKeys(t *testing.T) {
	out, f := run(t, key("/"), key("z"), key("z"), key("z"), enter, down, key("j"), key("k"), key("s"), enter)
	if !strings.Contains(out, "no themes found") || len(f.applied) != 0 {
		t.Fatalf("empty list:\n%s", out)
	}
}
