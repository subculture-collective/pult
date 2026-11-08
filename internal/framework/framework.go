// Package framework holds Pult's Hyprland framework backends. Each
// one themes a framework through the framework's own mechanism: it writes
// a theme or palette file Pult owns, if the framework has a place
// for one, and switches with the framework's own command. It never edits
// files the framework generates. Before switching, it records the
// framework's previous selection, so uninstall can switch back. See
// docs/reference/framework-backends.md.
package framework

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"git.subcult.tv/subculture-collective/pult/internal/changes"
	"git.subcult.tv/subculture-collective/pult/internal/color"
	"git.subcult.tv/subculture-collective/pult/internal/render"
)

// Env is what backends read from the machine. Tests replace it.
type Env struct {
	Home     string
	StateDir string // Pult's state directory, for the switch record
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(argv []string) ([]byte, error)
}

// Backend is one framework's theming mechanism. A selection is the
// backend's own string for "what the framework shows": a theme name, a
// scheme, an accent.
type Backend interface {
	// Name is the framework's name, as owner detection reports it.
	Name() string
	// Current reads the framework's selection now. An error means the
	// backend cannot be used now, for example because the shell is not
	// running; its text is shown to the user.
	Current(env Env) (string, error)
	// Prepare returns the files to write for t and the selection that
	// shows them, or an empty selection and a note when this theme cannot
	// be shown.
	Prepare(env Env, t render.Theme, p color.Palette) (edits []changes.Edit, selection string, note string, err error)
	// Switch returns the commands that select sel.
	Switch(env Env, sel string) [][]string
}

// Backends are the frameworks Pult can theme, by name.
var Backends = map[string]Backend{}

func register(b Backend) { Backends[b.Name()] = b }

// Names lists the frameworks with a backend.
func Names() []string {
	var out []string
	for n := range Backends {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Record is the switch record Pult keeps per framework, written
// through the change record like any other file: Previous is what the
// framework showed before Pult's first switch, Applied what Theme
// Tender selected last.
type Record struct {
	Previous string `json:"previous"`
	Applied  string `json:"applied"`
}

// RecordPath is where a framework's switch record lives.
func RecordPath(stateDir, name string) string {
	return filepath.Join(stateDir, "frameworks", name+".json")
}

// ReadRecord reads a switch record; a missing one is (nil, nil).
func ReadRecord(stateDir, name string) (*Record, error) {
	b, err := os.ReadFile(RecordPath(stateDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Record
	return &r, json.Unmarshal(b, &r)
}

// Plan is what applying a theme does to one framework.
type Plan struct {
	Name   string
	Edits  []changes.Edit // the backend's files and the switch record
	Switch [][]string     // run after the edits are written
	Note   string         // set when the framework is left alone, or a caveat
}

// PlanFor plans a backend for a theme. The previous selection comes from
// an existing record while a Pult selection is still shown, so a
// second apply does not take Pult's own theme for the user's.
func PlanFor(b Backend, env Env, t render.Theme, p color.Palette) (*Plan, error) {
	plan := &Plan{Name: b.Name()}
	cur, err := b.Current(env)
	if err != nil {
		plan.Note = b.Name() + " left alone: " + err.Error()
		return plan, nil
	}
	edits, sel, note, err := b.Prepare(env, t, p)
	if err != nil {
		return nil, err
	}
	plan.Note = note
	if sel == "" {
		if note == "" {
			plan.Note = b.Name() + " left alone"
		}
		return plan, nil
	}
	prev := cur
	if rec, err := ReadRecord(env.StateDir, b.Name()); err == nil && rec != nil && rec.Applied == cur {
		prev = rec.Previous
	}
	rec, _ := json.MarshalIndent(Record{Previous: prev, Applied: sel}, "", "  ")
	for i := range edits {
		edits[i].App, edits[i].Method = b.Name(), "framework"
	}
	edits = append(edits, changes.Edit{App: b.Name(), Path: RecordPath(env.StateDir, b.Name()), Method: "framework", Content: append(rec, '\n')})
	plan.Edits = edits
	plan.Switch = b.Switch(env, sel)
	return plan, nil
}

// Restore returns the commands that switch a framework back to what it
// showed before Pult, if Pult's selection is still shown.
// Read the record before uninstall removes it.
func Restore(b Backend, env Env, rec *Record) [][]string {
	if rec == nil || rec.Previous == "" {
		return nil
	}
	if cur, err := b.Current(env); err != nil || cur != rec.Applied {
		return nil
	}
	return b.Switch(env, rec.Previous)
}
