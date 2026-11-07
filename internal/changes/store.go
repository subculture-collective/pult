// Package changes records every file Theme Tender creates, edits or
// deletes before touching it, so that each change can be undone, and
// recovers from a run that stopped part-way. The contract is
// docs/reference/change-record.md.
package changes

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Change is one [[change]] entry of a transaction file.
type Change struct {
	App      string         `toml:"app"`
	Path     string         `toml:"path"`     // the path the app reads
	Resolved string         `toml:"resolved"` // after following symlinks; the file actually written
	Owner    string         `toml:"owner"`
	Method   string         `toml:"method"`
	Action   string         `toml:"action"` // create | edit | delete
	Before   string         `toml:"before"` // "sha256:…" or "absent"
	After    string         `toml:"after"`
	Mode     string         `toml:"mode"`
	Backup   string         `toml:"backup,omitempty"` // blob of Before, relative to the state directory
	Status   string         `toml:"status,omitempty"` // "failed" when validation failed and the file was restored
	Detail   map[string]any `toml:"detail,omitempty"`
}

// Transaction is one transaction file.
type Transaction struct {
	Schema        int       `toml:"schema"`
	ID            string    `toml:"id"`
	Kind          string    `toml:"kind"`   // apply | uninstall | rollback
	Status        string    `toml:"status"` // pending | complete | failed | rolled-back
	Started       time.Time `toml:"started"`
	Finished      time.Time `toml:"finished,omitempty"`
	Theme         string    `toml:"theme"`
	TenderVersion string    `toml:"tender_version"`
	BundleVersion string    `toml:"bundle_version"`
	RollsBack     string    `toml:"rolls_back,omitempty"` // for kind = "rollback"
	Error         string    `toml:"error,omitempty"`
	Changes       []Change  `toml:"change"`

	file string
}

// Meta describes the run that makes a transaction.
type Meta struct {
	Theme, TenderVersion, BundleVersion string
}

// Store is a change record directory.
type Store struct {
	dir string
	now func() time.Time
	// crash, when set, is called after each protocol step; a non-nil error
	// stops the run there, as if Theme Tender had been killed. Tests use it.
	crash func(point string) error
}

// ErrLocked means another Theme Tender run holds the lock.
var ErrLocked = errors.New("changes: another Theme Tender run holds the lock")

const absent = "absent"

// DefaultDir is $XDG_STATE_HOME/theme-tender, with $XDG_STATE_HOME
// defaulting to $HOME/.local/state.
func DefaultDir() (string, error) {
	if d := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "theme-tender"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "theme-tender"), nil
}

// Open opens or creates the change record in dir.
func Open(dir string) (*Store, error) {
	for _, d := range []string{"transactions", filepath.Join("blobs", "sha256")} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{dir: dir, now: time.Now}, nil
}

func (s *Store) step(point string) error {
	if s.crash != nil {
		return s.crash(point)
	}
	return nil
}

// Transactions returns every transaction, oldest first.
func (s *Store) Transactions() ([]*Transaction, error) {
	names, err := filepath.Glob(filepath.Join(s.dir, "transactions", "*.toml"))
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	var out []*Transaction
	for _, n := range names {
		data, err := os.ReadFile(n)
		if err != nil {
			return nil, err
		}
		tx := &Transaction{}
		if err := toml.Unmarshal(data, tx); err != nil {
			return nil, fmt.Errorf("changes: %s: %w", n, err)
		}
		tx.file = n
		out = append(out, tx)
	}
	return out, nil
}

func (s *Store) writeTransaction(tx *Transaction) error {
	if tx.file == "" {
		tx.file = filepath.Join(s.dir, "transactions", tx.Started.UTC().Format("20060102T150405Z")+"-"+tx.ID+".toml")
	}
	data, err := toml.Marshal(tx)
	if err != nil {
		return err
	}
	return atomicWrite(tx.file, data, 0o600, nil)
}

// FileState is a file Theme Tender currently owns a change to.
type FileState struct {
	Path     string `toml:"path"`
	Resolved string `toml:"resolved"`
	App      string `toml:"app"`
	Method   string `toml:"method"`
	Hash     string `toml:"hash"`     // what the file should contain now
	Pristine string `toml:"pristine"` // what it contained before Theme Tender first touched it
	Backup   string `toml:"backup,omitempty"`
}

// State is current.toml: the active theme and the files Theme Tender has
// changed from their pristine state.
type State struct {
	Theme string      `toml:"theme"`
	Files []FileState `toml:"file"`
}

// State replays the transactions. It does not depend on current.toml,
// which is written after each run for people and tools to read.
func (s *Store) State() (*State, error) {
	txs, err := s.Transactions()
	if err != nil {
		return nil, err
	}
	files := map[string]*FileState{}
	var themes []struct{ id, theme string }
	for _, tx := range txs {
		if tx.Status != "complete" && tx.Status != "failed" {
			continue
		}
		switch tx.Kind {
		case "apply":
			themes = append(themes, struct{ id, theme string }{tx.ID, tx.Theme})
		case "rollback":
			themes = slices.DeleteFunc(themes, func(t struct{ id, theme string }) bool { return t.id == tx.RollsBack })
		case "uninstall":
			themes = nil
		}
		for _, c := range tx.Changes {
			if c.Status == "failed" {
				continue
			}
			f, ok := files[c.Resolved]
			if !ok {
				f = &FileState{Path: c.Path, Resolved: c.Resolved, Pristine: c.Before, Backup: c.Backup}
				files[c.Resolved] = f
			}
			f.App, f.Method, f.Hash = c.App, c.Method, c.After
		}
	}
	st := &State{}
	if len(themes) > 0 {
		st.Theme = themes[len(themes)-1].theme
	}
	for _, k := range slices.Sorted(mapKeys(files)) {
		if f := files[k]; f.Hash != f.Pristine {
			st.Files = append(st.Files, *f)
		}
	}
	return st, nil
}

func (s *Store) writeState() error {
	st, err := s.State()
	if err != nil {
		return err
	}
	data, err := toml.Marshal(st)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir, "current.toml"), data, 0o600, nil)
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// newULID returns a ULID: 48 bits of milliseconds and 80 random bits in
// Crockford base32.
func newULID(t time.Time) string {
	var b [16]byte
	ms := uint64(t.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err)
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var out strings.Builder
	// 128 bits as 26 base32 digits: the first digit holds the top 3 bits.
	var acc uint64
	bits := 2 // pad to 130 bits
	for _, x := range b {
		acc = acc<<8 | uint64(x)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out.WriteByte(alphabet[(acc>>uint(bits))&31])
		}
	}
	return out.String()
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
