package changes

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

// fixture is a home directory with configs for two apps and a symlinked
// config, as stow would leave it.
type fixture struct {
	home, state string
	store       *Store
	kitty, tmux string // config paths
	link, real  string // symlinked config and its target
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{home: filepath.Join(root, "home"), state: filepath.Join(root, "state")}
	f.kitty = filepath.Join(f.home, ".config/kitty/kitty.conf")
	f.tmux = filepath.Join(f.home, ".config/tmux/tmux.conf")
	f.real = filepath.Join(f.home, "dotfiles/btop/btop.conf")
	f.link = filepath.Join(f.home, ".config/btop/btop.conf")
	write(t, f.kitty, "font_size 12\n", 0o600)
	write(t, f.tmux, "set -g mouse on\n", 0o644)
	write(t, f.real, "color_theme = \"Default\"\n", 0o644)
	if err := os.MkdirAll(filepath.Dir(f.link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.real, f.link); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.state)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 6, 18, 22, 5, 0, time.UTC)
	s.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	f.store = s
	return f
}

// edits is a typical apply: an include line, a new owned file, a select
// edit through a symlink, and a deleted file.
func (f *fixture) edits() []Edit {
	return []Edit{
		{App: "kitty", Path: f.kitty, Method: "include", Content: []byte("font_size 12\ninclude theme-tender.conf\n"), Detail: map[string]any{"line": "include theme-tender.conf"}},
		{App: "kitty", Path: filepath.Join(filepath.Dir(f.kitty), "theme-tender.conf"), Method: "include", Content: []byte("foreground #cdd6f4\n")},
		{App: "btop", Path: f.link, Owner: "stow", Method: "owned", Content: []byte("color_theme = \"theme-tender\"\n")},
		{App: "tmux", Path: f.tmux, Method: "include", Delete: true},
	}
}

func (f *fixture) snapshot(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(f.home, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(f.home, p)
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(p)
			target, _ = filepath.Rel(f.home, target)
			out[rel] = "-> " + target
			return nil
		}
		data, _ := os.ReadFile(p)
		out[rel] = fmt.Sprintf("%v %s", info.Mode().Perm(), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApply(t *testing.T) {
	f := newFixture(t)
	tx, err := f.store.Apply(Meta{Theme: "catppuccin-mocha", TenderVersion: "0.1.0"}, f.edits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Status != "complete" || tx.Kind != "apply" || len(tx.Changes) != 4 || len(tx.ID) != 26 {
		t.Fatalf("transaction = %+v", tx)
	}
	if got := read(t, f.kitty); got != "font_size 12\ninclude theme-tender.conf\n" {
		t.Errorf("kitty.conf = %q", got)
	}
	if mode(t, f.kitty) != 0o600 {
		t.Errorf("kitty.conf mode = %v, want 0600", mode(t, f.kitty))
	}
	if fi, err := os.Lstat(f.link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink replaced: %v %v", fi, err)
	}
	if got := read(t, f.real); got != "color_theme = \"theme-tender\"\n" {
		t.Errorf("symlink target = %q", got)
	}
	if _, err := os.Stat(f.tmux); !os.IsNotExist(err) {
		t.Errorf("tmux.conf not deleted: %v", err)
	}
	byApp := map[string]Change{}
	for _, c := range tx.Changes {
		byApp[c.App+" "+c.Action] = c
	}
	created := byApp["kitty create"]
	if created.Before != "absent" || created.Backup != "" || created.Mode != "0644" {
		t.Errorf("create change = %+v", created)
	}
	btop := byApp["btop edit"]
	if btop.Resolved != f.real || btop.Path != f.link || btop.Owner != "stow" {
		t.Errorf("btop change = %+v", btop)
	}
	edit := byApp["kitty edit"]
	if edit.Before != hashOf([]byte("font_size 12\n")) || edit.Detail["line"] != "include theme-tender.conf" {
		t.Errorf("kitty edit = %+v", edit)
	}
	// The backup restores by hand with zstd, as the contract says.
	blob, err := os.ReadFile(filepath.Join(f.state, edit.Backup))
	if err != nil {
		t.Fatal(err)
	}
	dec, _ := zstd.NewReader(bytes.NewReader(blob))
	defer dec.Close()
	var orig bytes.Buffer
	if _, err := orig.ReadFrom(dec); err != nil || orig.String() != "font_size 12\n" {
		t.Errorf("backup = %q, %v", orig.String(), err)
	}
	if raw, _ := os.ReadFile(tx.file); !strings.Contains(string(raw), "\nfinished = 2026-11-06T") {
		t.Errorf("finished is not a TOML datetime:\n%s", raw)
	}
	// The transaction file on disk says the same.
	txs, err := f.store.Transactions()
	if err != nil || len(txs) != 1 || txs[0].ID != tx.ID || txs[0].Status != "complete" || len(txs[0].Changes) != 4 {
		t.Fatalf("Transactions() = %+v, %v", txs, err)
	}
	st, err := f.store.State()
	if err != nil || st.Theme != "catppuccin-mocha" || len(st.Files) != 4 {
		t.Fatalf("State() = %+v, %v", st, err)
	}
}

func TestApplyUnchangedFileIsNotRecorded(t *testing.T) {
	f := newFixture(t)
	tx, err := f.store.Apply(Meta{Theme: "x"}, []Edit{{App: "kitty", Path: f.kitty, Content: []byte("font_size 12\n")}}, nil)
	if err != nil || len(tx.Changes) != 0 {
		t.Fatalf("tx = %+v, %v", tx, err)
	}
}

func TestValidateFailureRestoresThatApp(t *testing.T) {
	f := newFixture(t)
	before := f.snapshot(t)
	validate := func(app string, paths []string) error {
		if app == "kitty" {
			return errors.New("kitty: invalid color")
		}
		return nil
	}
	tx, err := f.store.Apply(Meta{Theme: "x"}, f.edits(), validate)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Status != "failed" {
		t.Errorf("status = %s, want failed", tx.Status)
	}
	after := f.snapshot(t)
	if k := ".config/kitty/kitty.conf"; before[k] != after[k] {
		t.Errorf("kitty.conf not restored: %q", after[k])
	}
	if _, ok := after[".config/kitty/theme-tender.conf"]; ok {
		t.Error("created kitty file not removed")
	}
	if read(t, f.real) != "color_theme = \"theme-tender\"\n" {
		t.Error("btop change was rolled back with kitty's")
	}
	for _, c := range tx.Changes {
		if (c.App == "kitty") != (c.Status == "failed") {
			t.Errorf("%s %s status %q", c.App, c.Action, c.Status)
		}
	}
	if !strings.Contains(tx.Error, "invalid color") {
		t.Errorf("error = %q", tx.Error)
	}
}

func TestUninstallRestoresPristine(t *testing.T) {
	f := newFixture(t)
	pristine := f.snapshot(t)
	if _, err := f.store.Apply(Meta{Theme: "a"}, f.edits(), nil); err != nil {
		t.Fatal(err)
	}
	second := []Edit{
		{App: "kitty", Path: filepath.Join(filepath.Dir(f.kitty), "theme-tender.conf"), Content: []byte("foreground #4c4f69\n")},
		{App: "btop", Path: f.link, Owner: "stow", Content: []byte("color_theme = \"theme-tender\"\n# b\n")},
	}
	if _, err := f.store.Apply(Meta{Theme: "b"}, second, nil); err != nil {
		t.Fatal(err)
	}
	tx, conflicts, err := f.store.Uninstall(false)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("Uninstall: %v %v", conflicts, err)
	}
	if tx.Kind != "uninstall" || tx.Status != "complete" {
		t.Errorf("tx = %+v", tx)
	}
	if got := f.snapshot(t); !mapsEqual(got, pristine) {
		t.Errorf("after uninstall:\n%v\nwant\n%v", got, pristine)
	}
	st, _ := f.store.State()
	if st.Theme != "" || len(st.Files) != 0 {
		t.Errorf("state after uninstall = %+v", st)
	}
}

func TestUndoRules(t *testing.T) {
	f := newFixture(t)
	tx, err := f.store.Apply(Meta{Theme: "a"}, f.edits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The user edits kitty.conf and removes btop's config afterwards.
	write(t, f.kitty, "font_size 14\ninclude theme-tender.conf\n", 0o600)
	if err := os.Remove(f.real); err != nil {
		t.Fatal(err)
	}
	rb, conflicts, err := f.store.Rollback(tx.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, c := range conflicts {
		reasons[c.Path] = c.Reason
	}
	if reasons[f.kitty] != "edited" || reasons[f.real] != "missing" || len(conflicts) != 2 {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	if read(t, f.kitty) != "font_size 14\ninclude theme-tender.conf\n" {
		t.Error("edited file was changed without confirmation")
	}
	if c := conflicts[slices.IndexFunc(conflicts, func(c Conflict) bool { return c.Path == f.kitty })]; string(c.Before) != "font_size 12\n" || !strings.Contains(string(c.Current), "font_size 14") {
		t.Errorf("conflict lacks the diff inputs: %+v", c)
	}
	if read(t, f.tmux) != "set -g mouse on\n" {
		t.Error("deleted file was not restored")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.kitty), "theme-tender.conf")); !os.IsNotExist(err) {
		t.Error("created file was not removed")
	}
	if rb.Kind != "rollback" || rb.Status != "complete" {
		t.Errorf("rollback tx = %+v", rb)
	}
	// With confirmation the edited file is restored too.
	if _, _, err := f.store.Rollback(tx.ID, true); err != nil {
		t.Fatal(err)
	}
	if read(t, f.kitty) != "font_size 12\n" {
		t.Errorf("forced rollback left %q", read(t, f.kitty))
	}
}

func TestLock(t *testing.T) {
	f := newFixture(t)
	unlock, err := f.store.lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Apply(Meta{}, f.edits(), nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("Apply while locked: %v, want ErrLocked", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Apply(Meta{}, f.edits(), nil); err != nil {
		t.Fatalf("Apply after unlock: %v", err)
	}
}

// TestCrashRecovery stops Apply after every protocol step, then recovers.
// Before the transaction is recorded as complete, every file must be back
// to its original bytes and mode; after it, the applied files stay. No
// transaction may stay pending.
func TestCrashRecovery(t *testing.T) {
	probe := newFixture(t)
	original := probe.snapshot(t)
	var points []string
	probe.store.crash = func(p string) error { points = append(points, p); return nil }
	if _, err := probe.store.Apply(Meta{}, probe.edits(), nil); err != nil {
		t.Fatal(err)
	}
	applied := probe.snapshot(t)
	if len(points) < 10 {
		t.Fatalf("only %d fault points: %v", len(points), points)
	}
	done := false
	for _, point := range points {
		done = done || point == "recorded"
		want, wantRecovered := original, 1
		if done {
			want, wantRecovered = applied, 0
		}
		if point == "locked" { // nothing was written yet
			wantRecovered = 0
		}
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			crash := errors.New("crash")
			f.store.crash = func(p string) error {
				if p == point {
					return crash
				}
				return nil
			}
			if _, err := f.store.Apply(Meta{Theme: "a"}, f.edits(), nil); !errors.Is(err, crash) {
				t.Fatalf("Apply: %v", err)
			}
			f.store.crash = nil
			recovered, conflicts, err := f.store.Recover()
			if err != nil || len(conflicts) != 0 {
				t.Fatalf("Recover: %v %v", conflicts, err)
			}
			if got := f.snapshot(t); !mapsEqual(got, want) {
				t.Errorf("files after recovery:\n%v\nwant\n%v", got, want)
			}
			if len(recovered) != wantRecovered {
				t.Errorf("recovered %d transactions, want %d", len(recovered), wantRecovered)
			}
			txs, _ := f.store.Transactions()
			for _, tx := range txs {
				if tx.Status == "pending" {
					t.Errorf("transaction %s still pending", tx.ID)
				}
			}
			if _, err := f.store.State(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestStateRebuildsWithoutCurrentFile(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.Apply(Meta{Theme: "a"}, f.edits(), nil); err != nil {
		t.Fatal(err)
	}
	want, _ := f.store.State()
	if err := os.Remove(filepath.Join(f.state, "current.toml")); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.State()
	if err != nil || got.Theme != want.Theme || len(got.Files) != len(want.Files) {
		t.Fatalf("rebuilt state = %+v, %v; want %+v", got, err, want)
	}
}

func write(t *testing.T, p, s string, m os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), m); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, m); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
