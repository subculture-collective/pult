package changes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPendingBlocksRuns: after a run stops part-way, nothing else runs
// until Recover, so half-written files are never taken for originals.
func TestPendingBlocksRuns(t *testing.T) {
	f := newFixture(t)
	original := read(t, f.kitty)
	crash := errors.New("crash")
	f.store.crash = func(p string) error {
		if p == "write:0" {
			return crash
		}
		return nil
	}
	if _, err := f.store.Apply(Meta{}, f.edits(), nil); !errors.Is(err, crash) {
		t.Fatal(err)
	}
	f.store.crash = nil
	for name, run := range map[string]func() error{
		"apply":     func() error { _, err := f.store.Apply(Meta{}, f.edits(), nil); return err },
		"uninstall": func() error { _, _, err := f.store.Uninstall(false); return err },
	} {
		if err := run(); !errors.Is(err, ErrPending) {
			t.Errorf("%s while pending: %v", name, err)
		}
	}
	if _, _, err := f.store.Recover(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Apply(Meta{}, f.edits(), nil); err != nil {
		t.Fatal(err)
	}
	if _, c, err := f.store.Uninstall(false); err != nil || len(c) != 0 {
		t.Fatal(c, err)
	}
	if got := read(t, f.kitty); got != original {
		t.Errorf("kitty.conf = %q, want %q", got, original)
	}
}

// TestWriteFailureRollsBack: a write that fails puts back what the run
// already wrote and records the run as failed.
func TestWriteFailureRollsBack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newFixture(t)
	before := f.snapshot(t)
	locked := filepath.Join(f.home, "locked")
	os.MkdirAll(locked, 0o755)
	os.WriteFile(filepath.Join(locked, "x.conf"), []byte("x\n"), 0o644)
	os.Chmod(locked, 0o555)
	defer os.Chmod(locked, 0o755)
	edits := []Edit{
		{App: "kitty", Path: f.kitty, Content: []byte("THEMED\n")},
		{App: "x", Path: filepath.Join(locked, "x.conf"), Content: []byte("THEMED\n")},
	}
	if _, err := f.store.Apply(Meta{}, edits, nil); err == nil || !strings.Contains(err.Error(), "x.conf") {
		t.Fatalf("Apply: %v", err)
	}
	os.Chmod(locked, 0o755)
	after := f.snapshot(t)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed: %q", k, after[k])
		}
	}
	txs, _ := f.store.Transactions()
	if n := len(txs); n != 1 || txs[0].Status != "failed" {
		t.Fatalf("transactions: %d, %+v", n, txs[0])
	}
	if st, _ := f.store.State(); len(st.Files) != 0 {
		t.Errorf("failed run left state: %+v", st.Files)
	}
	// The next run is not blocked, and uninstall still gets the original.
	if _, err := f.store.Apply(Meta{}, edits[:1], nil); err != nil {
		t.Fatal(err)
	}
	if _, c, err := f.store.Uninstall(false); err != nil || len(c) != 0 {
		t.Fatal(c, err)
	}
	if got := read(t, f.kitty); got != before[".config/kitty/kitty.conf"][len("-rw------- "):] {
		t.Errorf("kitty.conf = %q", got)
	}
}

// TestRecoverKeepsEditedFile: a file written by an interrupted run and
// edited since stays recorded, so uninstall can still offer the original.
func TestRecoverKeepsEditedFile(t *testing.T) {
	f := newFixture(t)
	original := read(t, f.kitty)
	f.store.crash = func(p string) error {
		if p == "write:0" {
			return errors.New("crash")
		}
		return nil
	}
	f.store.Apply(Meta{}, f.edits(), nil)
	f.store.crash = nil
	write(t, f.kitty, "user edit\n", 0o600)
	if _, c, err := f.store.Recover(); err != nil || len(c) != 1 {
		t.Fatalf("Recover: %v %v", c, err)
	}
	if got := read(t, f.kitty); got != "user edit\n" {
		t.Fatalf("recover changed the edited file: %q", got)
	}
	if _, c, err := f.store.Uninstall(false); err != nil || len(c) != 1 || read(t, f.kitty) != "user edit\n" {
		t.Fatalf("uninstall without force: %v %v", c, err)
	}
	if _, _, err := f.store.Uninstall(true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, f.kitty); got != original {
		t.Errorf("forced uninstall: %q, want %q", got, original)
	}
}

// TestEditsBetweenApplies: an edit made between two applies is not undone
// by uninstall without confirmation.
func TestEditsBetweenApplies(t *testing.T) {
	f := newFixture(t)
	original := read(t, f.kitty)
	if _, err := f.store.Apply(Meta{}, []Edit{{App: "kitty", Path: f.kitty, Content: []byte(original + "palette a\n")}}, nil); err != nil {
		t.Fatal(err)
	}
	write(t, f.kitty, original+"palette a\nmap ctrl+x my-binding\n", 0o600)
	if _, err := f.store.Apply(Meta{}, []Edit{{App: "kitty", Path: f.kitty, Content: []byte(original + "palette b\nmap ctrl+x my-binding\n")}}, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.store.State(); len(st.Files) != 1 || !st.Files[0].Edited {
		t.Fatalf("state: %+v", st.Files)
	}
	_, c, err := f.store.Uninstall(false)
	if err != nil || len(c) != 1 || c[0].Reason != "edited" || string(c[0].Before) != original {
		t.Fatalf("uninstall: %+v %v", c, err)
	}
	if !strings.Contains(read(t, f.kitty), "my-binding") {
		t.Fatal("uninstall without force dropped the user's edit")
	}
	if _, _, err := f.store.Uninstall(true); err != nil || read(t, f.kitty) != original {
		t.Errorf("forced uninstall: %q %v", read(t, f.kitty), err)
	}
}

func TestSymlinkLoop(t *testing.T) {
	f := newFixture(t)
	a, b := filepath.Join(f.home, "a.conf"), filepath.Join(f.home, "b.conf")
	os.Symlink(b, a)
	os.Symlink(a, b)
	if _, err := f.store.Apply(Meta{}, []Edit{{App: "x", Path: a, Content: []byte("x")}}, nil); err == nil || !strings.Contains(err.Error(), "too many levels") {
		t.Errorf("loop: %v", err)
	}
}

// TestUninstallRestoresDeletedFileMode: a file Pult deleted comes
// back with its own mode.
func TestUninstallRestoresDeletedFileMode(t *testing.T) {
	f := newFixture(t) // kitty.conf is 0600
	if _, err := f.store.Apply(Meta{}, []Edit{{App: "kitty", Path: f.kitty, Delete: true}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, c, err := f.store.Uninstall(false); err != nil || len(c) != 0 {
		t.Fatal(c, err)
	}
	if m := mode(t, f.kitty); m != 0o600 {
		t.Errorf("mode %o, want 600", m)
	}
}

func TestTwoEditsToOneFileRefused(t *testing.T) {
	f := newFixture(t)
	edits := []Edit{{App: "a", Path: f.kitty, Content: []byte("1\n")}, {App: "b", Path: f.kitty, Content: []byte("2\n")}}
	if _, err := f.store.Apply(Meta{}, edits, nil); err == nil || !strings.Contains(err.Error(), "both edit") {
		t.Errorf("duplicate edits: %v", err)
	}
	if txs, _ := f.store.Transactions(); len(txs) != 0 {
		t.Error("a transaction was recorded")
	}
}
