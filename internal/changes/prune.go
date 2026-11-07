package changes

import (
	"io/fs"
	"os"
	"path/filepath"
)

// keepTransactions is how many of the newest transactions keep their
// backups. Older transactions can no longer be rolled back on their own;
// uninstall still works, because pristine backups are always kept.
const keepTransactions = 20

// prune deletes blobs nothing needs any more. A blob is kept if one of the
// newest keepTransactions transactions refers to it, a pending transaction
// does (recovery needs it), or it is the pristine backup of a file Theme
// Tender still has a change to. The caller holds the lock and has written
// the transaction and current.toml, so blobs go only after the records
// that stop referring to them. A prune that stops part-way leaves only
// blobs that would have been deleted anyway, so it is best effort.
func (s *Store) prune() {
	txs, err := s.Transactions()
	if err != nil {
		return
	}
	st, err := s.State()
	if err != nil {
		return
	}
	keep := map[string]bool{}
	ref := func(hash, backup string) {
		if backup != "" {
			keep[filepath.Clean(backup)] = true
		} else if hash != absent && hash != "" {
			keep[blobPath(hash)] = true
		}
	}
	n := s.keep
	if n == 0 {
		n = keepTransactions
	}
	for i, tx := range txs {
		if i < len(txs)-n && tx.Status != "pending" {
			continue
		}
		for _, c := range tx.Changes {
			ref(c.Before, c.Backup)
		}
	}
	for _, f := range st.Files {
		ref(f.Pristine, f.Backup)
	}
	root := filepath.Join(s.dir, "blobs")
	var dirs []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		rel, err := filepath.Rel(s.dir, p)
		if err != nil || keep[rel] {
			return nil
		}
		if s.step("prune:"+rel) != nil {
			return filepath.SkipAll
		}
		os.Remove(p)
		return nil
	})
	// Remove emptied fan-out directories, deepest first; non-empty ones and
	// blobs/sha256 itself stay.
	for i := len(dirs) - 1; i >= 0; i-- {
		if d := dirs[i]; d != root && d != filepath.Join(root, "sha256") {
			os.Remove(d)
		}
	}
}
