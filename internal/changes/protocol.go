package changes

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/klauspost/compress/zstd"
)

// Edit is one file change Theme Tender wants to make.
type Edit struct {
	App, Path, Owner, Method string
	Delete                   bool
	Content                  []byte      // the new contents, unless Delete
	Mode                     fs.FileMode // for a new file; 0 means 0644
	Detail                   map[string]any
}

// Validator checks an app's written files and returns an error if the app
// rejects them. paths are the files changed for that app.
type Validator func(app string, paths []string) error

// Conflict is a file an undo did not touch.
type Conflict struct {
	Path   string
	Reason string // "edited": changed since Theme Tender wrote it; "missing"
	// Before and Current are the contents to show in a diff.
	Before, Current []byte
}

type plan struct {
	c       Change
	content []byte // nil for a delete
	before  []byte // nil if absent
	perm    fs.FileMode
	owner   *owner
}

type owner struct{ uid, gid int }

// Apply records and makes edits, following the write protocol: lock,
// pending transaction, backups, atomic writes, validation, completion. An
// app whose validation fails has its files restored and its changes marked
// failed; the transaction is then "failed". Edits that would not change a
// file are left out.
func (s *Store) Apply(m Meta, edits []Edit, validate Validator) (*Transaction, error) {
	var plans []plan
	for _, e := range edits {
		p, skip, err := planEdit(e)
		if err != nil {
			return nil, err
		}
		if !skip {
			plans = append(plans, p)
		}
	}
	return s.run("apply", "", m, plans, validate)
}

func planEdit(e Edit) (plan, bool, error) {
	resolved, err := resolve(e.Path)
	if err != nil {
		return plan{}, false, err
	}
	p := plan{c: Change{App: e.App, Path: e.Path, Resolved: resolved, Owner: orNone(e.Owner), Method: e.Method, Detail: e.Detail}}
	if err := p.readBefore(); err != nil {
		return plan{}, false, err
	}
	if p.perm == 0 {
		p.perm = e.Mode
		if p.perm == 0 {
			p.perm = 0o644
		}
	}
	if !e.Delete {
		p.content = e.Content
		if p.content == nil {
			p.content = []byte{}
		}
	}
	p.finish()
	return p, p.c.Before == p.c.After, nil
}

func (p *plan) readBefore() error {
	fi, err := os.Stat(p.c.Resolved)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		p.c.Before = absent
		return nil
	case err != nil:
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("changes: %s is not a regular file", p.c.Resolved)
	}
	p.before, err = os.ReadFile(p.c.Resolved)
	if err != nil {
		return err
	}
	p.c.Before, p.perm = hashOf(p.before), fi.Mode().Perm()
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		p.owner = &owner{int(st.Uid), int(st.Gid)}
	}
	return nil
}

func (p *plan) finish() {
	p.c.After, p.c.Action = absent, "delete"
	if p.content != nil {
		p.c.After, p.c.Action = hashOf(p.content), "edit"
		if p.before == nil {
			p.c.Action = "create"
		}
	}
	p.c.Mode = fmt.Sprintf("%04o", p.perm)
	if p.before != nil {
		p.c.Backup = blobPath(p.c.Before)
	}
}

// resolve follows symlinks in path, including a dangling final one.
func resolve(path string) (string, error) {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r, nil
	}
	dir, base := filepath.Split(path)
	if rdir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = rdir
	}
	full := filepath.Join(dir, base)
	if target, err := os.Readlink(full); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		return resolve(target)
	}
	return full, nil
}

func (s *Store) run(kind, rollsBack string, m Meta, plans []plan, validate Validator) (*Transaction, error) {
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := s.step("locked"); err != nil {
		return nil, err
	}
	tx := &Transaction{
		Schema: 1, ID: newULID(s.now()), Kind: kind, Status: "pending", Started: s.now().UTC(),
		Theme: m.Theme, TenderVersion: m.TenderVersion, BundleVersion: m.BundleVersion, RollsBack: rollsBack,
	}
	for _, p := range plans {
		tx.Changes = append(tx.Changes, p.c)
	}
	if err := s.writeTransaction(tx); err != nil {
		return nil, err
	}
	if err := s.step("pending"); err != nil {
		return nil, err
	}
	for i, p := range plans {
		if p.before != nil {
			if err := s.storeBlob(p.before); err != nil {
				return nil, err
			}
		}
		if err := s.step(fmt.Sprintf("blob:%d", i)); err != nil {
			return nil, err
		}
	}
	for i, p := range plans {
		if err := replaceFile(p.c.Resolved, p.content, p.perm, p.owner); err != nil {
			return nil, fmt.Errorf("changes: write %s: %w", p.c.Resolved, err)
		}
		if err := s.step(fmt.Sprintf("write:%d", i)); err != nil {
			return nil, err
		}
	}
	if validate != nil {
		var apps []string
		for _, p := range plans {
			if !slices.Contains(apps, p.c.App) {
				apps = append(apps, p.c.App)
			}
		}
		var errs []string
		for _, app := range apps {
			var paths []string
			for _, p := range plans {
				if p.c.App == app {
					paths = append(paths, p.c.Resolved)
				}
			}
			verr := validate(app, paths)
			if verr == nil {
				continue
			}
			errs = append(errs, verr.Error())
			for i := len(plans) - 1; i >= 0; i-- {
				if p := plans[i]; p.c.App == app {
					if err := replaceFile(p.c.Resolved, p.before, p.perm, p.owner); err != nil {
						return nil, fmt.Errorf("changes: restore %s: %w", p.c.Resolved, err)
					}
					tx.Changes[i].Status = "failed"
				}
			}
		}
		if len(errs) > 0 {
			tx.Status, tx.Error = "failed", strings.Join(errs, "; ")
		}
	}
	if err := s.step("validated"); err != nil {
		return nil, err
	}
	if tx.Status == "pending" {
		tx.Status = "complete"
	}
	tx.Finished = s.now().UTC()
	if err := s.writeTransaction(tx); err != nil {
		return nil, err
	}
	if err := s.step("recorded"); err != nil {
		return nil, err
	}
	if err := s.writeState(); err != nil {
		return nil, err
	}
	if err := s.step("state"); err != nil {
		return nil, err
	}
	return tx, nil
}

// Rollback undoes one transaction, newest change first. A file whose
// contents are what the transaction wrote is restored; a file changed
// since is left alone and reported, unless force is set; a missing file
// is reported and skipped.
func (s *Store) Rollback(id string, force bool) (*Transaction, []Conflict, error) {
	txs, err := s.Transactions()
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(txs, func(tx *Transaction) bool { return tx.ID == id })
	if i < 0 {
		return nil, nil, fmt.Errorf("changes: no transaction %s", id)
	}
	tx := txs[i]
	var targets []target
	for j := len(tx.Changes) - 1; j >= 0; j-- {
		if c := tx.Changes[j]; c.Status != "failed" {
			targets = append(targets, target{c, c.After, c.Before, c.Backup})
		}
	}
	plans, conflicts, err := s.undoPlans(targets, force)
	if err != nil {
		return nil, nil, err
	}
	out, err := s.run("rollback", id, Meta{Theme: tx.Theme, TenderVersion: tx.TenderVersion, BundleVersion: tx.BundleVersion}, plans, nil)
	return out, conflicts, err
}

// Uninstall returns every file Theme Tender changed to its pristine state:
// its contents before Theme Tender first touched it.
func (s *Store) Uninstall(force bool) (*Transaction, []Conflict, error) {
	st, err := s.State()
	if err != nil {
		return nil, nil, err
	}
	var targets []target
	for _, f := range st.Files {
		targets = append(targets, target{Change{App: f.App, Path: f.Path, Resolved: f.Resolved, Method: f.Method}, f.Hash, f.Pristine, f.Backup})
	}
	plans, conflicts, err := s.undoPlans(targets, force)
	if err != nil {
		return nil, nil, err
	}
	out, err := s.run("uninstall", "", Meta{}, plans, nil)
	return out, conflicts, err
}

// Recover rolls back every pending transaction, the trace of a run that
// stopped part-way, and marks it rolled-back.
func (s *Store) Recover() ([]*Transaction, []Conflict, error) {
	unlock, err := s.lock()
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	txs, err := s.Transactions()
	if err != nil {
		return nil, nil, err
	}
	var recovered []*Transaction
	var conflicts []Conflict
	for _, tx := range txs {
		if tx.Status != "pending" {
			continue
		}
		for j := len(tx.Changes) - 1; j >= 0; j-- {
			c := tx.Changes[j]
			cur, data, err := current(c.Resolved)
			if err != nil {
				return nil, nil, err
			}
			switch cur {
			case c.Before:
				continue // not written yet
			case c.After:
				before, err := s.readBlob(c.Before, c.Backup)
				if err != nil {
					return nil, nil, err
				}
				perm, own := fileMode(c), ownerOf(c.Resolved)
				if err := replaceFile(c.Resolved, before, perm, own); err != nil {
					return nil, nil, err
				}
			default:
				before, _ := s.readBlob(c.Before, c.Backup)
				conflicts = append(conflicts, Conflict{Path: c.Resolved, Reason: "edited", Before: before, Current: data})
			}
		}
		tx.Status = "rolled-back"
		tx.Finished = s.now().UTC()
		if err := s.writeTransaction(tx); err != nil {
			return nil, nil, err
		}
		recovered = append(recovered, tx)
	}
	if err := s.writeState(); err != nil {
		return nil, nil, err
	}
	return recovered, conflicts, nil
}

// target is a file to return to the content hashed want, expected to hold
// the content hashed have now.
type target struct {
	c          Change
	have, want string
	backup     string
}

func (s *Store) undoPlans(targets []target, force bool) ([]plan, []Conflict, error) {
	var plans []plan
	var conflicts []Conflict
	for _, t := range targets {
		cur, data, err := current(t.c.Resolved)
		if err != nil {
			return nil, nil, err
		}
		before, err := s.readBlob(t.want, t.backup)
		if err != nil {
			return nil, nil, err
		}
		if cur != t.have {
			reason := "edited"
			if cur == absent {
				reason = "missing"
			}
			conflicts = append(conflicts, Conflict{Path: t.c.Resolved, Reason: reason, Before: before, Current: data})
			if !force || reason == "missing" {
				continue
			}
		}
		p := plan{c: Change{App: t.c.App, Path: t.c.Path, Resolved: t.c.Resolved, Owner: orNone(t.c.Owner), Method: t.c.Method}, content: before, perm: 0o644}
		if err := p.readBefore(); err != nil {
			return nil, nil, err
		}
		if p.before == nil {
			p.perm = fileMode(t.c)
		}
		p.finish()
		if p.c.Before != p.c.After {
			plans = append(plans, p)
		}
	}
	return plans, conflicts, nil
}

func current(path string) (hash string, data []byte, err error) {
	data, err = os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return absent, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return hashOf(data), data, nil
}

func fileMode(c Change) fs.FileMode {
	var m uint32
	if _, err := fmt.Sscanf(c.Mode, "%o", &m); err != nil || m == 0 {
		return 0o644
	}
	return fs.FileMode(m)
}

func ownerOf(path string) *owner {
	if fi, err := os.Stat(path); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			return &owner{int(st.Uid), int(st.Gid)}
		}
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func blobPath(hash string) string {
	hex := strings.TrimPrefix(hash, "sha256:")
	return filepath.Join("blobs", "sha256", hex[:2], hex[2:]+".zst")
}

func (s *Store) storeBlob(data []byte) error {
	file := filepath.Join(s.dir, blobPath(hashOf(data)))
	if _, err := os.Stat(file); err == nil {
		return nil
	}
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		return err
	}
	enc.Write(data)
	if err := enc.Close(); err != nil {
		return err
	}
	return atomicWrite(file, buf.Bytes(), 0o600, nil)
}

// readBlob returns the contents hashed hash, or nil for "absent".
func (s *Store) readBlob(hash, backup string) ([]byte, error) {
	if hash == absent {
		return nil, nil
	}
	if backup == "" {
		backup = blobPath(hash)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, backup))
	if err != nil {
		return nil, fmt.Errorf("changes: backup of %s: %w", hash, err)
	}
	dec, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(dec); err != nil {
		return nil, err
	}
	if hashOf(out.Bytes()) != hash {
		return nil, fmt.Errorf("changes: backup %s does not match its hash", backup)
	}
	return out.Bytes(), nil
}

// replaceFile replaces path with data atomically, or removes it if data is nil.
func replaceFile(path string, data []byte, perm fs.FileMode, own *owner) error {
	if data == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return syncDir(filepath.Dir(path))
	}
	return atomicWrite(path, data, perm, own)
}

// atomicWrite writes a temporary file in the same directory, sets its
// mode and owner, syncs it and renames it over path.
func atomicWrite(path string, data []byte, perm fs.FileMode, own *owner) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".theme-tender-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return err
	}
	if own != nil && (own.uid != os.Geteuid() || own.gid != os.Getegid()) {
		if err := f.Chown(own.uid, own.gid); err != nil {
			return fmt.Errorf("keep owner %d:%d: %w", own.uid, own.gid, err)
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
