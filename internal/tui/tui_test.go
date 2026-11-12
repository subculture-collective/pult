package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"git.subcult.tv/subculture-collective/pult/internal/catalog"
	"git.subcult.tv/subculture-collective/pult/internal/color"
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
		{ID: "tmux", Tier: 1, Owner: "stow", Status: "owned by stow; asked in pult apply"},
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
	enter  = tea.KeyPressMsg{Code: tea.KeyEnter}
	esc    = tea.KeyPressMsg{Code: tea.KeyEscape}
	down   = tea.KeyPressMsg{Code: tea.KeyDown}
	tabKey = tea.KeyPressMsg{Code: tea.KeyTab}
)

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

// run drives a 100x30 program with msgs, quits, and returns the final view
// and the fake.
func run(t *testing.T, msgs ...tea.Msg) (string, *fake) {
	t.Helper()
	return runSized(t, 100, 30, newFake(), msgs...)
}

func runSized(t *testing.T, w, h int, f *fake, msgs ...tea.Msg) (string, *fake) {
	t.Helper()
	tm := teatest.NewTestModel(t, New(f), teatest.WithInitialTermSize(w, h))
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
	if !strings.Contains(out, "Closest to catppuccin-mocha") {
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
	out, _ := run(t, key("]"))
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

// at returns the layout of a w x h screen, for aiming mouse clicks.
func at(w, h int) layout {
	m := New(newFake())
	m.width, m.height = w, h
	return m.layout()
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// row is the screen position of list row i.
func row(r rect, i int) (int, int) { return r.x + 2, r.y + 1 + i }

func TestFullscreenWithMouse(t *testing.T) {
	v := New(newFake()).View()
	if !v.AltScreen || v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("alt screen %v, mouse mode %v", v.AltScreen, v.MouseMode)
	}
	lines := strings.Split(v.Content, "\n")
	if len(lines) != 30 {
		t.Fatalf("view has %d lines, want 30", len(lines))
	}
}

func TestClickSelectsThenApplies(t *testing.T) {
	l := at(100, 30)
	out, f := run(t, click(row(l.list, 2)))
	if !strings.Contains(out, "3 of 5") || !strings.Contains(out, "Solarized Dark") {
		t.Fatalf("click did not select row 3:\n%s", out)
	}
	m := New(newFake())
	m.list.cursor = 2
	m.openPlan()
	d := m.dialogBox()
	ok, no := d.ok, d.no
	out, f = run(t, click(row(l.list, 2)), click(row(l.list, 2)), click(ok.x, ok.y))
	if len(f.applied) != 1 || f.applied[0] != "solarized-dark" || !strings.Contains(out, "Applied solarized-dark") {
		t.Fatalf("click apply: %v\n%s", f.applied, out)
	}
	_, f = run(t, click(row(l.list, 2)), click(row(l.list, 2)), click(no.x, no.y))
	if len(f.applied) != 0 {
		t.Fatalf("cancel button applied: %v", f.applied)
	}
}

func TestClickTagTabAndSearch(t *testing.T) {
	l := at(100, 30)
	light := l.tagCells[1]
	out, _ := run(t, click(light.x, light.y))
	if !strings.Contains(out, "catppuccin-latte") || strings.Contains(out, "catppuccin-mocha") {
		t.Fatalf("click light tag:\n%s", out)
	}
	apps := l.tabs[1]
	out, _ = run(t, click(apps.x, apps.y))
	if !strings.Contains(out, "Details") || !strings.Contains(out, "version 1.4.7, owned") {
		t.Fatalf("click apps tab:\n%s", out)
	}
	out, _ = run(t, click(l.search.x+3, l.search.y+1), key("m"), key("o"), key("c"))
	if !strings.Contains(out, "catppuccin-mocha") || strings.Contains(out, "solarized-dark") {
		t.Fatalf("click search and type:\n%s", out)
	}
}

func TestWheelScrolls(t *testing.T) {
	f := newFake()
	for i := 0; i < 20; i++ {
		f.themes = append(f.themes, catalog.Info{ID: fmt.Sprintf("extra-%02d", i), Name: fmt.Sprintf("Extra %02d", i), Bundled: true})
	}
	l := at(100, 14)
	x, y := row(l.list, 0)
	out, _ := runSized(t, 100, 14, f, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown}, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if strings.Contains(out, "catppuccin-latte") || !strings.Contains(out, "extra-01") {
		t.Fatalf("wheel did not scroll:\n%s", out)
	}
}

func TestHelp(t *testing.T) {
	out, _ := run(t, key("?"))
	if !strings.Contains(out, "click again") {
		t.Fatalf("help:\n%s", out)
	}
	out, _ = run(t, key("?"), key("x"))
	if strings.Contains(out, "click again") {
		t.Fatalf("help did not close:\n%s", out)
	}
}

func TestTagsPaneKeys(t *testing.T) {
	out, _ := run(t, tabKey, tabKey, tea.KeyPressMsg{Code: tea.KeyRight}, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !strings.Contains(out, "catppuccin-latte") || strings.Contains(out, "catppuccin-mocha") {
		t.Fatalf("tags pane toggle light:\n%s", out)
	}
}

// TestLongMessageDialog: a result too wide for the footer opens a dialog.
func TestLongMessageDialog(t *testing.T) {
	m := New(newFake())
	m.notify(strings.Repeat("word ", 40))
	if m.dialog != messageDialog || m.status != "" {
		t.Fatalf("dialog %v status %q", m.dialog, m.status)
	}
}

// TestDialogFitsSmallScreen: a dialog never runs off a short terminal.
func TestDialogFitsSmallScreen(t *testing.T) {
	m := New(newFake())
	m.width, m.height = 60, 16
	m.dialog = helpDialog
	d := m.dialogBox()
	if d.r.y < 0 || d.r.y+d.r.h > 16 || d.ok.y >= d.r.y+d.r.h-1 {
		t.Fatalf("dialog %+v, ok %+v on a 16-row screen", d.r, d.ok)
	}
}
