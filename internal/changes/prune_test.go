package changes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func blobCount(t *testing.T, f *fixture) int {
	t.Helper()
	n := 0
	filepath.WalkDir(filepath.Join(f.state, "blobs"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	return n
}

// themeEdit rewrites kitty.conf with a different theme line each time, so
// every apply stores a new backup.
func themeEdit(f *fixture, i int) []Edit {
	return []Edit{{App: "kitty", Path: f.kitty, Content: []byte(fmt.Sprintf("font_size 12\n# theme %d\n", i))}}
}

func TestPruneKeepsRecentAndPristine(t *testing.T) {
	f := newFixture(t)
	f.store.keep = 3
	pristine := f.snapshot(t)
	for i := range 10 {
		if _, err := f.store.Apply(Meta{Theme: fmt.Sprint(i)}, themeEdit(f, i), nil); err != nil {
			t.Fatal(err)
		}
	}
	// The pristine backup plus the before-blobs of the last three applies
	// (themes 6, 7 and 8).
	if n := blobCount(t, f); n != 4 {
		t.Errorf("%d blobs, want 4", n)
	}
	// The newest transaction still rolls back.
	txs, _ := f.store.Transactions()
	if _, c, err := f.store.Rollback(txs[len(txs)-1].ID, false); err != nil || len(c) != 0 {
		t.Fatalf("rollback: %v %v", c, err)
	}
	if got := read(t, f.kitty); got != "font_size 12\n# theme 8\n" {
		t.Errorf("after rollback: %q", got)
	}
	// Uninstall still restores the original, ten transactions back.
	if _, c, err := f.store.Uninstall(false); err != nil || len(c) != 0 {
		t.Fatalf("uninstall: %v %v", c, err)
	}
	if got := f.snapshot(t); !mapsEqual(got, pristine) {
		t.Errorf("after uninstall:\n%v\nwant\n%v", got, pristine)
	}
	// With nothing outstanding, only the last three transactions' blobs
	// are left, and empty fan-out directories are gone.
	if n := blobCount(t, f); n > 3 {
		t.Errorf("%d blobs after uninstall", n)
	}
	dirs, _ := os.ReadDir(filepath.Join(f.state, "blobs", "sha256"))
	for _, d := range dirs {
		if entries, _ := os.ReadDir(filepath.Join(f.state, "blobs", "sha256", d.Name())); len(entries) == 0 {
			t.Errorf("empty directory %s left", d.Name())
		}
	}
}

// TestPruneCrash stops pruning after each deletion; the record still
// rolls back the newest transaction and uninstalls.
func TestPruneCrash(t *testing.T) {
	probe := newFixture(t)
	probe.store.keep = 2
	var points []string
	probe.store.crash = func(p string) error {
		if strings.HasPrefix(p, "prune:") {
			points = append(points, p)
		}
		return nil
	}
	for i := range 5 {
		if _, err := probe.store.Apply(Meta{}, themeEdit(probe, i), nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(points) == 0 {
		t.Fatal("no prune points")
	}
	for k := range points {
		t.Run(fmt.Sprint(k), func(t *testing.T) {
			f := newFixture(t)
			f.store.keep = 2
			pristine := f.snapshot(t)
			seen := 0
			f.store.crash = func(p string) error {
				if strings.HasPrefix(p, "prune:") {
					if seen == k {
						seen++
						return errors.New("crash")
					}
					seen++
				}
				return nil
			}
			for i := range 5 {
				if _, err := f.store.Apply(Meta{}, themeEdit(f, i), nil); err != nil {
					t.Fatal(err)
				}
			}
			f.store.crash = nil
			txs, _ := f.store.Transactions()
			if _, c, err := f.store.Rollback(txs[len(txs)-1].ID, false); err != nil || len(c) != 0 {
				t.Fatalf("rollback: %v %v", c, err)
			}
			if _, c, err := f.store.Uninstall(false); err != nil || len(c) != 0 {
				t.Fatalf("uninstall: %v %v", c, err)
			}
			if got := f.snapshot(t); !mapsEqual(got, pristine) {
				t.Errorf("after uninstall:\n%v\nwant\n%v", got, pristine)
			}
		})
	}
}

// TestPristineAfterUninstall: edits the user makes after an uninstall are
// what the next uninstall restores, not the contents from before the first
// apply.
func TestPristineAfterUninstall(t *testing.T) {
	for _, undo := range []string{"uninstall", "rollback"} {
		t.Run(undo, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.store.Apply(Meta{}, themeEdit(f, 1), nil); err != nil {
				t.Fatal(err)
			}
			if undo == "uninstall" {
				_, _, err := f.store.Uninstall(false)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				txs, _ := f.store.Transactions()
				if _, _, err := f.store.Rollback(txs[0].ID, false); err != nil {
					t.Fatal(err)
				}
			}
			write(t, f.kitty, "font_size 14\n", 0o600)
			if _, err := f.store.Apply(Meta{}, themeEdit(f, 2), nil); err != nil {
				t.Fatal(err)
			}
			if _, c, err := f.store.Uninstall(false); err != nil || len(c) != 0 {
				t.Fatalf("uninstall: %v %v", c, err)
			}
			if got := read(t, f.kitty); got != "font_size 14\n" {
				t.Errorf("kitty.conf = %q, want the user's edit", got)
			}
		})
	}
}
