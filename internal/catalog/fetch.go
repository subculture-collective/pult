package catalog

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Fetch checks out each source's Paths at its pinned Revision into
// root/<id> with git, replacing any previous checkout, and checks that the
// commit date matches the manifest.
func Fetch(m Manifest, root string) error {
	for _, s := range m.Sources {
		dir := filepath.Join(root, s.ID)
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		steps := [][]string{
			{"init", "-q", dir},
			{"-C", dir, "remote", "add", "origin", s.URL},
			append([]string{"-C", dir, "sparse-checkout", "set", "--no-cone"}, s.Paths...),
			{"-C", dir, "fetch", "-q", "--depth", "1", "--filter=blob:none", "origin", s.Revision},
			{"-C", dir, "checkout", "-q", "FETCH_HEAD"},
		}
		for _, args := range steps {
			if _, err := git(args...); err != nil {
				return fmt.Errorf("catalog: fetch %s: %w", s.ID, err)
			}
		}
		head, err := git("-C", dir, "log", "-1", "--format=%H %cI")
		if err != nil {
			return fmt.Errorf("catalog: fetch %s: %w", s.ID, err)
		}
		rev, date, _ := strings.Cut(head, " ")
		want, _ := time.Parse(time.RFC3339, s.Date)
		got, err := time.Parse(time.RFC3339, date)
		if rev != s.Revision || err != nil || !got.Equal(want) {
			return fmt.Errorf("catalog: fetch %s: checked out %s from %s, manifest says %s from %s", s.ID, rev, date, s.Revision, s.Date)
		}
	}
	return nil
}

func git(args ...string) (string, error) {
	var out, errOut bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
