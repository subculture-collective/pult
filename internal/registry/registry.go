// Package registry loads app registry entries: the built-in entries from
// the data bundle and the user's overrides. The contract is
// docs/reference/registry-schema.md; the JSON Schema is registry/schema.json.
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// Platform is an operating system an entry supports.
type Platform string

const (
	Linux Platform = "linux"
	MacOS Platform = "macos"
)

// Method is an integration method.
type Method string

const (
	Include Method = "include"
	Owned   Method = "owned"
	Keys    Method = "keys"
	ANSI    Method = "ansi"
	Port    Method = "port"
	Manual  Method = "manual"
)

// Entry is one app's registry entry, after user overrides.
type Entry struct {
	Schema       int                   `json:"schema"`
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Homepage     string                `json:"homepage"`
	Platforms    []Platform            `json:"platforms"`
	Tier         int                   `json:"tier"`
	Detect       Detect                `json:"detect"`
	Config       Config                `json:"config"`
	Integrations []Integration         `json:"integrations"`
	Reload       []Reload              `json:"reload"`
	Validate     *Command              `json:"validate"`
	Ports        map[string]PortSource `json:"ports"`
}

type Detect struct {
	Binaries  []string            `json:"binaries"`
	MacOSApps []string            `json:"macos_apps"`
	Packages  map[string][]string `json:"packages"`
	Version   *VersionProbe       `json:"version"`
}

// VersionProbe runs Argv and takes the version from Regex's one capture group.
type VersionProbe struct {
	Argv  []string `json:"argv"`
	Regex string   `json:"regex"`
}

type Config struct {
	Env   []EnvVar              `json:"env"`
	Paths map[Platform][]string `json:"paths"`
}

// EnvVar overrides the config location. Kind "dir" names the directory
// holding the config file; "file" names the file itself.
type EnvVar struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Integration is one way to theme an app. Which fields are set depends on
// Method; registry/schema.json lists them.
type Integration struct {
	Method    Method         `json:"method"`
	Versions  string         `json:"versions"`
	Template  string         `json:"template"`
	OwnedFile string         `json:"owned_file"`
	Line      string         `json:"line"`
	Position  string         `json:"position"`
	Format    string         `json:"format"`
	Key       string         `json:"key"`
	Target    string         `json:"target"`
	Select    *Edit          `json:"select"`
	Paths     []string       `json:"paths"`
	Set       map[string]any `json:"set"`
	URL       string         `json:"url"`
}

// Edit sets keys in a config file.
type Edit struct {
	Format string         `json:"format"`
	Set    map[string]any `json:"set"`
}

// Command is an argument vector. It never passes through a shell.
type Command struct {
	Argv []string `json:"argv"`
}

// Reload is a command run after apply. When is "running" or "always". An
// empty Platforms means every platform the entry supports.
type Reload struct {
	Argv      []string   `json:"argv"`
	When      string     `json:"when"`
	Platforms []Platform `json:"platforms"`
}

// PortSource is a known upstream port for a palette family.
type PortSource struct {
	URL  string `json:"url"`
	Kind string `json:"kind"`
}

// Registry holds the loaded entries.
type Registry struct {
	entries map[string]Entry
	// Problems lists user override files that were skipped and why. The
	// built-in entry, if any, is used unchanged in their place.
	Problems []error
}

// Get returns the entry for id.
func (r *Registry) Get(id string) (Entry, bool) {
	e, ok := r.entries[id]
	return e, ok
}

// Entries returns all entries sorted by ID.
func (r *Registry) Entries() []Entry {
	ids := slices.Sorted(maps.Keys(r.entries))
	out := make([]Entry, len(ids))
	for i, id := range ids {
		out[i] = r.entries[id]
	}
	return out
}

// Load reads schema.json and apps/<id>.yaml from builtin, then applies the
// <id>.yaml overrides in userDir (normally
// ~/.config/theme-tender/registry.d). An override replaces fields of the
// built-in entry with the same id: mappings merge key by key, and any other
// value, lists included, is replaced. "disabled: true" removes the entry.
// A file for a new id must be a complete entry.
//
// Any problem with a built-in entry is an error. A problem with a user file
// skips that file and is recorded in Registry.Problems. A missing userDir,
// or an empty one, is not a problem.
func Load(builtin fs.FS, userDir string) (*Registry, error) {
	v, err := newValidator(builtin)
	if err != nil {
		return nil, err
	}
	docs := map[string]map[string]any{}
	names, err := fs.Glob(builtin, "apps/*.yaml")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		data, err := fs.ReadFile(builtin, name)
		if err != nil {
			return nil, err
		}
		doc, err := v.entry(name, data)
		if err != nil {
			return nil, err
		}
		docs[doc["id"].(string)] = doc
	}

	reg := &Registry{entries: map[string]Entry{}}
	if userDir != "" {
		names, err := filepath.Glob(filepath.Join(userDir, "*.yaml"))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if err := v.override(docs, name); err != nil {
				reg.Problems = append(reg.Problems, err)
			}
		}
	}

	for id, doc := range docs {
		e, err := decode(doc)
		if err != nil {
			return nil, fmt.Errorf("registry: %s: %w", id, err)
		}
		reg.entries[id] = e
	}
	return reg, nil
}

// schemaURL names schema.json in validation errors.
const schemaURL = "theme-tender:registry/schema.json"

type validator struct {
	entrySchema, overrideSchema *jsonschema.Schema
}

func newValidator(builtin fs.FS) (*validator, error) {
	data, err := fs.ReadFile(builtin, "schema.json")
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("registry: schema.json: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, err
	}
	v := &validator{}
	if v.entrySchema, err = c.Compile(schemaURL + "#/$defs/entry"); err != nil {
		return nil, fmt.Errorf("registry: schema.json: %w", err)
	}
	if v.overrideSchema, err = c.Compile(schemaURL + "#/$defs/override"); err != nil {
		return nil, fmt.Errorf("registry: schema.json: %w", err)
	}
	return v, nil
}

// entry parses and fully checks a built-in entry.
func (v *validator) entry(name string, data []byte) (map[string]any, error) {
	doc, err := parseYAML(data)
	if err == nil {
		err = matchFileName(doc, name)
	}
	if err == nil {
		if _, ok := doc["disabled"]; ok {
			err = errors.New("disabled is only allowed in user overrides")
		}
	}
	if err == nil {
		err = v.check(doc)
	}
	if err != nil {
		return nil, fmt.Errorf("registry: %s: %w", name, err)
	}
	return doc, nil
}

// override applies one user file to docs, or leaves docs unchanged and
// returns why not.
func (v *validator) override(docs map[string]map[string]any, name string) error {
	fail := func(err error) error { return fmt.Errorf("registry: %s: %w", name, err) }
	data, err := os.ReadFile(name)
	if err != nil {
		return fail(err)
	}
	doc, err := parseYAML(data)
	if err != nil {
		return fail(err)
	}
	if err := v.overrideSchema.Validate(doc); err != nil {
		return fail(err)
	}
	if err := matchFileName(doc, name); err != nil {
		return fail(err)
	}
	id := doc["id"].(string)
	base, known := docs[id]
	disabled, _ := doc["disabled"].(bool)
	delete(doc, "disabled")
	if disabled {
		if !known {
			return fail(fmt.Errorf("no entry %q to disable", id))
		}
		delete(docs, id)
		return nil
	}
	merged := doc
	if known {
		merged = merge(base, doc)
	}
	if err := v.check(merged); err != nil {
		return fail(err)
	}
	docs[id] = merged
	return nil
}

// check validates doc against the entry schema, then the rules the schema
// cannot express.
func (v *validator) check(doc map[string]any) error {
	if err := v.entrySchema.Validate(doc); err != nil {
		return err
	}
	e, err := decode(doc)
	if err != nil {
		return err
	}
	return e.check()
}

var placeholder = regexp.MustCompile(`\{[^{}\s]*\}`)

func (e Entry) check() error {
	var errs []error
	for _, p := range e.Platforms {
		if len(e.Config.Paths[p]) == 0 {
			errs = append(errs, fmt.Errorf("config.paths has no %s paths", p))
		}
	}
	for p := range e.Config.Paths {
		if !slices.Contains(e.Platforms, p) {
			errs = append(errs, fmt.Errorf("config.paths has %s paths but platforms does not list %s", p, p))
		}
	}
	if v := e.Detect.Version; v != nil {
		if re, err := regexp.Compile(v.Regex); err != nil {
			errs = append(errs, fmt.Errorf("detect.version.regex: %w", err))
		} else if re.NumSubexp() != 1 {
			errs = append(errs, fmt.Errorf("detect.version.regex %q needs exactly one capture group, has %d", v.Regex, re.NumSubexp()))
		}
		errs = append(errs, checkPlaceholders("detect.version.argv", v.Argv, nil)...)
	}
	commandVars := []string{"{config}", "{theme_file}"}
	for i, r := range e.Reload {
		errs = append(errs, checkPlaceholders(fmt.Sprintf("reload[%d].argv", i), r.Argv, commandVars)...)
		for _, p := range r.Platforms {
			if !slices.Contains(e.Platforms, p) {
				errs = append(errs, fmt.Errorf("reload[%d] lists %s, which platforms does not", i, p))
			}
		}
	}
	if e.Validate != nil {
		errs = append(errs, checkPlaceholders("validate.argv", e.Validate.Argv, commandVars)...)
	}
	for i, in := range e.Integrations {
		if _, err := ParseRange(in.Versions); err != nil {
			errs = append(errs, fmt.Errorf("integrations[%d].versions: %w", i, err))
		}
		errs = append(errs, checkPlaceholders(fmt.Sprintf("integrations[%d].line", i), []string{in.Line}, []string{"{theme_file}"})...)
	}
	return errors.Join(errs...)
}

func checkPlaceholders(field string, args, allowed []string) []error {
	var errs []error
	for _, a := range args {
		for _, p := range placeholder.FindAllString(a, -1) {
			if !slices.Contains(allowed, p) {
				errs = append(errs, fmt.Errorf("%s: placeholder %s is not allowed here", field, p))
			}
		}
	}
	return errs
}

func parseYAML(data []byte) (map[string]any, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	// Round-trip through JSON so the schema validator sees JSON types.
	j, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		return nil, err
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New("not a YAML mapping")
	}
	return m, nil
}

func matchFileName(doc map[string]any, name string) error {
	id, _ := doc["id"].(string)
	if stem := strings.TrimSuffix(path.Base(filepath.ToSlash(name)), ".yaml"); id != stem {
		return fmt.Errorf("id %q does not match file name %s.yaml", id, stem)
	}
	return nil
}

// merge returns base with over applied: mappings merge recursively, other
// values are replaced.
func merge(base, over map[string]any) map[string]any {
	out := maps.Clone(base)
	for k, v := range over {
		bm, ok1 := out[k].(map[string]any)
		om, ok2 := v.(map[string]any)
		if ok1 && ok2 {
			out[k] = merge(bm, om)
		} else {
			out[k] = v
		}
	}
	return out
}

func decode(doc map[string]any) (Entry, error) {
	j, err := json.Marshal(doc)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	err = json.Unmarshal(j, &e)
	return e, err
}
