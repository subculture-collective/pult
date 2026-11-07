// Package owner finds who else manages a config file, so Theme Tender asks
// or hands off instead of overwriting it silently: Omarchy, a Hyprland
// framework, a palette generator (matugen, wallust, pywal), chezmoi, stow
// or another symlink setup.
package owner

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Kind is who owns a file. Lower values win when several apply.
type Kind int

const (
	None Kind = iota
	Symlink
	Stow
	Chezmoi
	Generator
	Framework
	Omarchy
	Subarchy
)

// Action is what Theme Tender should do with a file.
type Action int

const (
	Write   Action = iota // nobody else owns it
	Ask                   // writing would change a dotfiles repository or fight a generator
	HandOff               // the owner has a theme mechanism Theme Tender uses instead
)

func (a Action) String() string { return [...]string{"write", "ask", "hand off"}[a] }

// Owner is a file's owner.
type Owner struct {
	Kind   Kind
	Name   string // generator or framework name, or stow package
	Source string // the file that really holds the config: link target or chezmoi source
	Detail string // evidence, for the prompt
}

// Record is the change record's owner value.
func (o Owner) Record() string {
	switch o.Kind {
	case Symlink:
		return "symlink"
	case Stow:
		return "stow"
	case Chezmoi:
		return "chezmoi"
	case Generator:
		return "generator:" + o.Name
	case Framework:
		return "framework:" + o.Name
	case Omarchy:
		return "omarchy"
	case Subarchy:
		return "subarchy"
	}
	return "none"
}

// Action says whether Theme Tender may write the file.
func (o Owner) Action() Action {
	switch o.Kind {
	case None:
		return Write
	case Omarchy, Subarchy:
		return HandOff
	}
	return Ask
}

// Env is the environment detection reads. Tests replace it with a fixture.
type Env struct {
	Root     string // prefix for system paths such as /usr/share/omarchy; "" means /
	Home     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(argv []string) ([]byte, error) // stdout of a command
}

// SystemEnv is the real environment.
func SystemEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{
		Home: home, Getenv: os.Getenv, LookPath: exec.LookPath,
		Run: func(argv []string) ([]byte, error) { return exec.Command(argv[0], argv[1:]...).Output() },
	}
}

func (e Env) xdg(name, fallback string) string {
	if v := e.Getenv(name); filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(e.Home, fallback)
}

// frameworks lists each Hyprland framework's marker directories, under
// $XDG_CONFIG_HOME unless absolute after expansion. Sources: the projects'
// repositories (HyDE Configs/.config/hyde, ML4W dotfiles/.config/ml4w,
// end-4 dots/.config/quickshell/ii), Caelestia's and DankMaterialShell's
// READMEs and settings code, and Noctalia's earlier releases.
var frameworks = []struct {
	name string
	dirs []string // config-relative; "state:" and "cache:" prefixes for the other XDG roots
}{
	{"caelestia", []string{"caelestia", "quickshell/caelestia", "state:caelestia"}},
	{"dms", []string{"DankMaterialShell", "dms", "quickshell/dms"}},
	{"end-4", []string{"quickshell/ii", "illogical-impulse"}},
	{"hyde", []string{"hyde", "state:hyde", "cache:hyde"}},
	{"ml4w", []string{"ml4w", "ml4w-dotfiles-settings"}},
	{"noctalia", []string{"noctalia", "quickshell/noctalia-shell"}},
}

// System is what detection found on the machine as a whole.
type System struct {
	Subarchy     bool // /etc/os-release says ID=subarchy
	Omarchy      bool
	OmarchyTheme string
	Frameworks   []string
	Generators   []string
}

// Detector holds what New found; File checks one path against it.
// chezmoi is asked about each file with "chezmoi source-path <path>".
type Detector struct {
	env          Env
	sys          System
	omarchyDirs  []string
	omarchyHooks map[string][]byte // theme-set hook name -> contents
	// subarchyDefaults is where Subarchy ships the defaults its user files
	// layer over; Theme Tender never edits them.
	subarchyDefaults string
	frameworkDirs    map[string][]string
	generatorFiles   map[string][]string // generator -> output files
	generatorDirs    map[string][]string // generator -> output directories
}

// New scans the machine for Omarchy, frameworks and generators.
func New(env Env) (*Detector, error) {
	d := &Detector{env: env, frameworkDirs: map[string][]string{}, generatorFiles: map[string][]string{}, generatorDirs: map[string][]string{}}
	cfg := env.xdg("XDG_CONFIG_HOME", ".config")
	state := env.xdg("XDG_STATE_HOME", ".local/state")
	cache := env.xdg("XDG_CACHE_HOME", ".cache")

	d.sys.Subarchy = osReleaseID(filepath.Join(env.Root, "/etc/os-release")) == "subarchy"
	d.subarchyDefaults = filepath.Join(env.Root, "/usr/share/subcult/default")

	// Omarchy: the install, then the current theme name.
	installs := []string{filepath.Join(env.Home, ".local/share/omarchy"), filepath.Join(env.Root, "/usr/share/omarchy")}
	if p := env.Getenv("OMARCHY_PATH"); filepath.IsAbs(p) {
		installs = append([]string{p}, installs...)
	}
	for _, p := range installs {
		if isDir(p) {
			d.sys.Omarchy = true
			d.omarchyDirs = append(d.omarchyDirs, p)
		}
	}
	if d.sys.Omarchy {
		d.omarchyDirs = append(d.omarchyDirs, filepath.Join(cfg, "omarchy"), filepath.Join(state, "omarchy"))
		// Theme-set hooks run on every theme switch and may regenerate
		// configs from the Omarchy palette.
		d.omarchyHooks = map[string][]byte{}
		hookDir := filepath.Join(cfg, "omarchy", "hooks", "theme-set.d")
		if entries, err := os.ReadDir(hookDir); err == nil {
			for _, e := range entries {
				if b, err := readSmall(filepath.Join(hookDir, e.Name())); err == nil && b != nil {
					d.omarchyHooks[e.Name()] = b
				}
			}
		}
		if b, err := os.ReadFile(filepath.Join(state, "omarchy/current/theme.name")); err == nil {
			d.sys.OmarchyTheme = strings.TrimSpace(string(b))
		} else if t, err := os.Readlink(filepath.Join(cfg, "omarchy/current/theme")); err == nil {
			d.sys.OmarchyTheme = filepath.Base(t)
		}
	}

	for _, f := range frameworks {
		var dirs []string
		found := false
		for _, rel := range f.dirs {
			p := filepath.Join(cfg, rel)
			if r, ok := strings.CutPrefix(rel, "state:"); ok {
				p = filepath.Join(state, r)
			} else if r, ok := strings.CutPrefix(rel, "cache:"); ok {
				p = filepath.Join(cache, r)
			}
			dirs = append(dirs, p)
			found = found || isDir(p)
		}
		if found {
			d.sys.Frameworks = append(d.sys.Frameworks, f.name)
			d.frameworkDirs[f.name] = dirs
		}
	}

	if outs, ok, err := d.tomlTargets(filepath.Join(cfg, "matugen/config.toml"), "output_path"); err != nil {
		return nil, err
	} else if ok {
		d.sys.Generators = append(d.sys.Generators, "matugen")
		d.generatorFiles["matugen"] = outs
	}
	if isDir(filepath.Join(cache, "wal")) || isDir(filepath.Join(cfg, "wal")) {
		d.sys.Generators = append(d.sys.Generators, "pywal")
	}
	d.generatorDirs["pywal"] = []string{filepath.Join(cache, "wal")}
	if outs, ok, err := d.tomlTargets(filepath.Join(cfg, "wallust/wallust.toml"), "target"); err != nil {
		return nil, err
	} else if ok {
		d.sys.Generators = append(d.sys.Generators, "wallust")
		d.generatorFiles["wallust"] = outs
	}
	slices.Sort(d.sys.Generators)
	return d, nil
}

// System reports machine-wide owners. A framework found here but not
// linked to a particular file still calls for one question before apply.
func (d *Detector) System() System { return d.sys }

// tomlTargets reads every string value named key anywhere in a generator's
// TOML config, with "~" expanded.
func (d *Detector) tomlTargets(file, key string) ([]string, bool, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, true, nil // present but unreadable: still the user's generator
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if s, ok := val.(string); ok && k == key {
					out = append(out, d.expand(s))
				} else {
					walk(val)
				}
			}
		case []any:
			for _, val := range x {
				walk(val)
			}
		}
	}
	walk(doc)
	slices.Sort(out)
	return out, true, nil
}

func (d *Detector) expand(p string) string {
	switch {
	case p == "~":
		return d.env.Home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(d.env.Home, p[2:])
	case strings.HasPrefix(p, "$HOME/"):
		return filepath.Join(d.env.Home, p[6:])
	}
	return p
}

// File finds the owner of the config file at path, which may not exist.
func (d *Detector) File(path string) (Owner, error) {
	resolved, link, err := d.resolve(path)
	if err != nil {
		return Owner{}, err
	}
	content, _ := readSmall(resolved)
	dir := filepath.Dir(path)

	refers := func(dirs, files []string) (string, bool) {
		for _, p := range dirs {
			if within(resolved, p) || within(path, p) || d.mentions(content, p, "") {
				return p, true
			}
		}
		for _, f := range files {
			if f == path || f == resolved || d.mentions(content, f, dir) {
				return f, true
			}
		}
		return "", false
	}

	if within(resolved, d.subarchyDefaults) || within(path, d.subarchyDefaults) {
		return Owner{Kind: Subarchy, Source: resolved, Detail: "Subarchy ships it as a default in " + d.subarchyDefaults + "; Theme Tender never edits it"}, nil
	}
	if d.sys.Omarchy {
		if p, ok := refers(d.omarchyDirs, nil); ok {
			return Owner{Kind: Omarchy, Detail: "uses Omarchy's theme files (" + d.short(p) + ")"}, nil
		}
		if d.mentions(content, filepath.Join(d.env.Home, ".config/omarchy"), "") {
			return Owner{Kind: Omarchy, Detail: "uses Omarchy's theme files"}, nil
		}
		for _, name := range slices.Sorted(maps.Keys(d.omarchyHooks)) {
			if d.mentions(d.omarchyHooks[name], path, "") || (resolved != path && d.mentions(d.omarchyHooks[name], resolved, "")) {
				return Owner{Kind: Omarchy, Detail: "the Omarchy theme-set hook " + name + " rewrites it on every theme switch"}, nil
			}
		}
		if generatedByOmarchy(content) {
			return Owner{Kind: Omarchy, Detail: "its header says Omarchy generates it"}, nil
		}
	}
	for _, name := range d.sys.Frameworks {
		if p, ok := refers(d.frameworkDirs[name], nil); ok {
			return Owner{Kind: Framework, Name: name, Detail: name + " manages it (" + d.short(p) + ")"}, nil
		}
	}
	for _, name := range []string{"matugen", "pywal", "wallust"} {
		if p, ok := refers(d.generatorDirs[name], d.generatorFiles[name]); ok {
			return Owner{Kind: Generator, Name: name, Detail: name + " writes its colors (" + d.short(p) + ")"}, nil
		}
	}
	if _, err := d.env.LookPath("chezmoi"); err == nil {
		if out, err := d.env.Run([]string{"chezmoi", "source-path", path}); err == nil {
			if src := strings.TrimSpace(string(out)); src != "" {
				return Owner{Kind: Chezmoi, Source: src, Detail: "chezmoi manages it from " + d.short(src)}, nil
			}
		}
	}
	if link != "" {
		if pkg, ok := stowPackage(d.env.Home, link, resolved, path); ok {
			return Owner{Kind: Stow, Name: filepath.Base(pkg), Source: resolved, Detail: "stow package " + d.short(pkg) + " links it"}, nil
		}
		return Owner{Kind: Symlink, Source: resolved, Detail: "it is a symlink to " + d.short(resolved)}, nil
	}
	return Owner{Kind: None}, nil
}

// resolve follows symlinks in path. link is the deepest symlinked prefix
// of path, if any.
func (d *Detector) resolve(path string) (resolved, link string, err error) {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	cur := string(filepath.Separator)
	for i, p := range parts {
		if p == "" {
			continue
		}
		next := filepath.Join(cur, p)
		fi, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			return filepath.Join(append([]string{cur}, parts[i:]...)...), link, nil
		}
		if err != nil {
			return "", "", err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			t, err := filepath.EvalSymlinks(next)
			if err != nil {
				return "", "", fmt.Errorf("owner: %s: %w", next, err)
			}
			link, next = filepath.Join(append([]string{string(filepath.Separator)}, parts[:i+1]...)...), t
		}
		cur = next
	}
	return cur, link, nil
}

// stowPackage recognises stow's layout: the link at ~/rel points at
// <stow dir>/<package>/rel.
func stowPackage(home, link, resolved, path string) (string, bool) {
	rel, err := filepath.Rel(home, link)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	relPath, _ := filepath.Rel(home, path)
	suffix := string(filepath.Separator) + relPath
	if !strings.HasSuffix(resolved, suffix) {
		return "", false
	}
	pkg := strings.TrimSuffix(resolved, suffix)
	return pkg, pkg != "" && filepath.Dir(pkg) != pkg
}

// mentions reports whether content refers to p, written absolute, with
// "~/" or "$HOME/", or, for a file under dir, by its path relative to dir
// with or without "./".
func (d *Detector) mentions(content []byte, p, dir string) bool {
	if len(content) == 0 {
		return false
	}
	forms := []string{p}
	if rel, err := filepath.Rel(d.env.Home, p); err == nil && !strings.HasPrefix(rel, "..") {
		forms = append(forms, "~/"+rel, "$HOME/"+rel, "${HOME}/"+rel)
	}
	if rel, err := filepath.Rel(dir, p); dir != "" && err == nil && filepath.IsLocal(rel) {
		forms = append(forms, rel, "./"+rel)
	}
	for _, f := range forms {
		if containsWord(content, f) {
			return true
		}
	}
	return false
}

// containsWord finds s not preceded or followed by a path character, so
// "kitty.conf" does not match "mykitty.conf".
func containsWord(b []byte, s string) bool {
	for i := 0; ; {
		j := bytes.Index(b[i:], []byte(s))
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(s)
		before := start == 0 || !isPathByte(b[start-1])
		after := end == len(b) || !isPathByte(b[end]) || b[end] == '/'
		if before && after {
			return true
		}
		i = start + 1
	}
}

func isPathByte(c byte) bool {
	return c == '/' || c == '.' || c == '_' || c == '-' || c == '~' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// osReleaseID is the ID field of an os-release file, or "".
func osReleaseID(file string) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "ID="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

// generatedByOmarchy looks for a "generated ... Omarchy" note in the first
// lines, such as "# Generated from the active Omarchy palette".
func generatedByOmarchy(content []byte) bool {
	lines := strings.SplitN(string(content), "\n", 11)
	for _, l := range lines[:min(10, len(lines))] {
		l = strings.ToLower(l)
		if strings.Contains(l, "generated") && strings.Contains(l, "omarchy") {
			return true
		}
	}
	return false
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

func (d *Detector) short(p string) string {
	if rel, err := filepath.Rel(d.env.Home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return p
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func readSmall(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return nil, err
	}
	return os.ReadFile(p)
}
