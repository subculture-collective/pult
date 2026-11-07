// Package apply is Theme Tender's apply pipeline: it finds the installed
// apps, chooses each one's integration, renders templates, plans the edits
// with a diff preview, applies them through the change record, validates
// and reloads, and uninstalls.
package apply

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"git.subcult.tv/subculture-collective/theme-tender/internal/changes"
	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
	"git.subcult.tv/subculture-collective/theme-tender/internal/configedit"
	"git.subcult.tv/subculture-collective/theme-tender/internal/omarchy"
	"git.subcult.tv/subculture-collective/theme-tender/internal/owner"
	"git.subcult.tv/subculture-collective/theme-tender/internal/registry"
	"git.subcult.tv/subculture-collective/theme-tender/internal/render"
)

// System runs commands. Tests replace it.
type System interface {
	LookPath(name string) (string, error)
	// Run runs argv and returns its combined output.
	Run(argv []string) ([]byte, error)
	// Running reports whether a process with this exact name runs.
	Running(name string) bool
}

// Options are the pipeline's inputs.
type Options struct {
	Home      string
	Platform  registry.Platform
	Getenv    func(string) string
	Registry  *registry.Registry
	Templates fs.FS
	Store     *changes.Store
	Owners    *owner.Detector
	Sys       System
	// Confirm is asked before writing a file someone else owns (stow,
	// chezmoi, a generator, a framework). nil means no.
	Confirm func(app string, o owner.Owner) bool
	Meta    changes.Meta
	// Get downloads upstream port files; CacheDir keeps them by hash. With
	// Get nil, ports are only used from the cache.
	Get      func(url string) ([]byte, error)
	CacheDir string
}

// Theme is a catalog theme with its full palette.
type Theme struct {
	render.Theme
	Palette color.Palette
}

// AppPlan is what apply would do to one app.
type AppPlan struct {
	ID          string
	Entry       registry.Entry
	Version     string
	Config      string
	ThemeFile   string
	Owner       owner.Owner
	Integration *registry.Integration
	Edits       []changes.Edit
	Diffs       []string
	Skip        string // why the app is left alone
	// Notes are things worth telling the user, such as a port that could
	// not be used.
	Notes []string
	port  []byte // the downloaded port file, for a port integration
}

// Plan is a whole apply.
type Plan struct {
	Theme Theme
	Apps  []AppPlan
	// Omarchy is the Omarchy theme directory, when Omarchy is installed.
	Omarchy       []changes.Edit
	OmarchySwitch []string
}

// Prepare plans an apply without changing any file.
func Prepare(o Options, th Theme) (*Plan, error) {
	if m := th.Palette.Missing(); len(m) > 0 {
		return nil, fmt.Errorf("apply: theme palette is missing %v", m)
	}
	p := &Plan{Theme: th}
	for _, e := range o.Registry.Entries() {
		ap, err := o.planApp(e, &th)
		if err != nil {
			return nil, fmt.Errorf("apply: %s: %w", e.ID, err)
		}
		p.Apps = append(p.Apps, ap)
	}
	if sys := o.Owners.System(); sys.Omarchy {
		edits, err := omarchy.Edits(o.Home, th.Theme, th.Palette)
		if err != nil {
			return nil, err
		}
		if prev := sys.OmarchyTheme; prev != "" && !strings.HasPrefix(prev, omarchy.Prefix) {
			edits[0].Detail = map[string]any{"previous_theme": prev}
		}
		p.Omarchy = edits
		p.OmarchySwitch = omarchy.SwitchCommand(omarchy.Name(th.Slug))
	}
	return p, nil
}

// Survey reports each app's platform, installation, version, config path,
// owner and integration, without a theme and without asking.
func Survey(o Options) ([]AppPlan, error) {
	o.Confirm = func(string, owner.Owner) bool { return true }
	// Ports apply to one theme: report what the active theme uses.
	if o.Meta.Theme == "" && o.Store != nil {
		if st, err := o.Store.State(); err == nil {
			o.Meta.Theme = st.Theme
		}
	}
	var out []AppPlan
	for _, e := range o.Registry.Entries() {
		ap, err := o.planApp(e, nil)
		if err != nil {
			return nil, fmt.Errorf("apply: %s: %w", e.ID, err)
		}
		out = append(out, ap)
	}
	return out, nil
}

// planApp plans one app; with th nil it stops after choosing the
// integration.
func (o Options) planApp(e registry.Entry, th *Theme) (AppPlan, error) {
	ap := AppPlan{ID: e.ID, Entry: e}
	if !slices.Contains(e.Platforms, o.Platform) {
		ap.Skip = "not available on " + string(o.Platform)
		return ap, nil
	}
	if !o.installed(e) {
		ap.Skip = "not installed"
		return ap, nil
	}
	ap.Version = o.version(e)
	ap.Config = o.configPath(e)
	own, err := o.Owners.File(ap.Config)
	if err != nil {
		return ap, err
	}
	ap.Owner = own
	switch own.Action() {
	case owner.HandOff:
		if own.Kind == owner.Subarchy {
			ap.Skip = "Subarchy default, left alone: " + own.Detail
		} else {
			ap.Skip = "Omarchy themes it: " + own.Detail
		}
		return ap, nil
	case owner.Ask:
		if o.Confirm == nil || !o.Confirm(e.ID, own) {
			ap.Skip = fmt.Sprintf("owned by %s (%s); not confirmed", own.Record(), own.Detail)
			return ap, nil
		}
	}
	themeID := o.Meta.Theme
	if th != nil {
		themeID = th.Slug
	}
	ins := e.Integrations
	var in *registry.Integration
	for {
		in = chooseIntegration(ins, ap.Version, themeID)
		if in == nil {
			ap.Skip = "no integration for version " + ap.Version
			return ap, nil
		}
		if in.Method != registry.Port {
			break
		}
		// Without a theme (Survey) nothing is downloaded: the port counts
		// only if a checked copy is cached, as after an apply that used it.
		fetch := o
		if th == nil {
			fetch.Get = nil
		}
		data, err := fetch.fetchPort(in, themeID)
		if err == nil {
			ap.port = data
			break
		}
		// Fall back to the integrations after the port.
		if th != nil {
			ap.Notes = append(ap.Notes, "upstream port not used: "+err.Error())
		}
		for i := range ins {
			if &ins[i] == in {
				ins = ins[i+1:]
				break
			}
		}
	}
	ap.Integration = in
	if in.Method == registry.Manual {
		ap.Skip = "manual setup: " + in.URL
		return ap, nil
	}
	if th == nil {
		return ap, nil
	}
	return ap, o.planEdits(&ap, *th)
}

func (o Options) planEdits(ap *AppPlan, th Theme) error {
	in := ap.Integration
	original, err := readFile(ap.Config)
	if err != nil {
		return err
	}
	// Editors work on the text after a UTF-8 byte order mark, which is put
	// back in front, so a line added "at the start" goes after it.
	bom := []byte("\ufeff")
	cfg := bytes.TrimPrefix(original, bom)
	rendered := ap.port
	if in.Template != "" {
		out, err := render.File(o.Templates, in.Template, th.Theme, th.Palette)
		if err != nil {
			return err
		}
		rendered = []byte(out)
	}
	ap.ThemeFile = o.themeFile(ap)
	var newCfg []byte
	switch in.Method {
	case registry.Include:
		if in.Line != "" {
			line := strings.ReplaceAll(in.Line, "{theme_file}", ap.ThemeFile)
			newCfg = cfg
			if !includesFile(cfg, ap.ThemeFile) {
				newCfg = configedit.IncludeLine(cfg, line, in.Position == "start")
			}
		} else if in.Format == "toml" {
			newCfg, err = configedit.TOMLAppend(cfg, in.Key, ap.ThemeFile)
		} else {
			err = fmt.Errorf("include by key in %s files is not supported", in.Format)
		}
	case registry.Owned, registry.Port:
		newCfg = cfg
		if in.Select != nil {
			newCfg, err = set(in.Select.Format, cfg, in.Select.Set)
		}
	case registry.Keys:
		switch in.Format {
		case "toml":
			newCfg, err = configedit.TOMLKeys(cfg, rendered, in.Paths)
		case "yaml":
			newCfg, err = configedit.YAMLKeys(cfg, rendered, in.Paths)
		default:
			err = fmt.Errorf("keys in %s files are not supported", in.Format)
		}
	case registry.ANSI:
		newCfg, err = set(in.Format, cfg, in.Set)
	}
	if err != nil {
		return err
	}
	if ap.ThemeFile != "" {
		before, err := readFile(ap.ThemeFile)
		if err != nil {
			return err
		}
		ap.add(o.edit(ap, ap.ThemeFile, rendered), before)
	}
	if len(cfg) < len(original) {
		newCfg = append(slices.Clone(bom), newCfg...)
	}
	ap.add(o.edit(ap, ap.Config, newCfg), original)
	return nil
}

func (ap *AppPlan) add(e changes.Edit, before []byte) {
	if d := Diff(e.Path, before, e.Content); d != "" {
		ap.Edits = append(ap.Edits, e)
		ap.Diffs = append(ap.Diffs, d)
	}
}

// themeFile is the file Theme Tender owns for the app's integration, or ""
// for methods that only edit the config.
func (o Options) themeFile(ap *AppPlan) string {
	switch in := ap.Integration; in.Method {
	case registry.Include:
		return o.ownedFile(ap.Config, in.OwnedFile)
	case registry.Owned, registry.Port:
		return filepath.Join(filepath.Dir(ap.Config), filepath.FromSlash(in.Target))
	}
	return ""
}

// fetchPort returns a port's file for a theme: from the cache if it holds
// a copy with the pinned hash, else downloaded from the pinned revision
// and checked against that hash.
func (o Options) fetchPort(in *registry.Integration, themeID string) ([]byte, error) {
	f := in.Flavors[themeID]
	cache := ""
	if o.CacheDir != "" {
		cache = filepath.Join(o.CacheDir, "ports", f.SHA256)
		if b, err := os.ReadFile(cache); err == nil && sha256Hex(b) == f.SHA256 {
			return b, nil
		}
	}
	if o.Get == nil {
		return nil, errors.New("downloads are off")
	}
	url := "https://raw.githubusercontent.com/" + strings.TrimPrefix(in.URL, "https://github.com/") + "/" + in.Revision + "/" + f.Path
	b, err := o.Get(url)
	if err != nil {
		return nil, err
	}
	if sha256Hex(b) != f.SHA256 {
		return nil, fmt.Errorf("%s does not match its pinned SHA-256", url)
	}
	if cache != "" && os.MkdirAll(filepath.Dir(cache), 0o755) == nil {
		os.WriteFile(cache, b, 0o644) // best effort; a missing cache only costs a download
	}
	return b, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// edit records the app's version and integration with each change, so
// doctor can tell when the app was upgraded since.
func (o Options) edit(ap *AppPlan, path string, content []byte) changes.Edit {
	detail := map[string]any{"integration": describe(ap.Integration)}
	if ap.Version != "" {
		detail["app_version"] = ap.Version
	}
	return changes.Edit{App: ap.ID, Path: path, Owner: ap.Owner.Record(), Method: string(ap.Integration.Method), Content: content, Detail: detail}
}

// describe names an integration: its method and version range.
func describe(in *registry.Integration) string {
	if in.Versions == "" {
		return string(in.Method)
	}
	return string(in.Method) + " (" + in.Versions + ")"
}

// Upgrade is an app whose version differs from the one recorded when
// Theme Tender last changed its files.
type Upgrade struct {
	App, Was, Now string
	// WasIntegration is what the last apply used; NowIntegration is what
	// an apply would choose now, or why the app would be skipped.
	WasIntegration, NowIntegration string
	// Validate is the validator's result against the current files, when
	// the integration is unchanged and the app has a validator.
	Validated   bool
	ValidateErr error
}

// Upgrades lists apps Theme Tender still has changes to whose version is
// now different. Changes recorded before versions were recorded are
// skipped.
func Upgrades(o Options) ([]Upgrade, error) {
	st, err := o.Store.State()
	if err != nil {
		return nil, err
	}
	themed := map[string]bool{}
	for _, f := range st.Files {
		themed[f.App] = true
	}
	txs, err := o.Store.Transactions()
	if err != nil {
		return nil, err
	}
	last := map[string]map[string]any{}
	for _, tx := range txs {
		if tx.Kind != "apply" || (tx.Status != "complete" && tx.Status != "failed") {
			continue
		}
		for _, c := range tx.Changes {
			if c.Status != "failed" && c.Detail != nil {
				last[c.App] = c.Detail
			}
		}
	}
	apps, err := Survey(o)
	if err != nil {
		return nil, err
	}
	var out []Upgrade
	for _, ap := range apps {
		was, _ := last[ap.ID]["app_version"].(string)
		if !themed[ap.ID] || was == "" || ap.Version == "" || was == ap.Version {
			continue
		}
		u := Upgrade{App: ap.ID, Was: was, Now: ap.Version, NowIntegration: "none: " + ap.Skip}
		u.WasIntegration, _ = last[ap.ID]["integration"].(string)
		if ap.Integration != nil {
			u.NowIntegration = describe(ap.Integration)
		}
		if u.NowIntegration == u.WasIntegration && ap.Entry.Validate != nil {
			ap.ThemeFile = o.themeFile(&ap)
			argv := substitute(ap.Entry.Validate.Argv, ap)
			u.Validated = true
			if b, err := o.Sys.Run(argv); err != nil {
				u.ValidateErr = fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, strings.TrimSpace(string(b)))
			}
		}
		out = append(out, u)
	}
	return out, nil
}

// includesFile reports whether a config already loads file on a line that
// is not a comment, by any spelling of its name: Subarchy's layered user
// files ship the include line already, in whatever form the distribution
// chose.
func includesFile(cfg []byte, file string) bool {
	base := filepath.Base(file)
	for _, l := range strings.Split(string(cfg), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, ";") || strings.HasPrefix(l, "//") {
			continue
		}
		// Only a line that loads a file counts: a key binding that sources
		// the theme file on demand does not load it at start.
		directive := strings.FieldsFunc(l, func(r rune) bool { return r == ' ' || r == '\t' || r == '=' })[0]
		if !slices.Contains(includeDirectives, directive) {
			continue
		}
		for i := strings.Index(l, base); i >= 0; {
			end := i + len(base)
			before := i == 0 || strings.ContainsRune(" \t\"'/=?~", rune(l[i-1]))
			after := end == len(l) || strings.ContainsRune(" \t\"'", rune(l[end]))
			if before && after {
				return true
			}
			next := strings.Index(l[i+1:], base)
			if next < 0 {
				break
			}
			i += 1 + next
		}
	}
	return false
}

// includeDirectives are the line keywords that load another config file in
// the apps with include integrations (kitty, foot, Ghostty, tmux).
var includeDirectives = []string{"include", "config-file", "source-file", "source", "import"}

func set(format string, src []byte, values map[string]any) ([]byte, error) {
	switch format {
	case "toml":
		return configedit.TOMLSet(src, values)
	case "ini":
		return configedit.INISet(src, values)
	case "kdl":
		return configedit.KDLSet(src, values)
	case "args":
		return configedit.ArgsSet(src, values)
	case "yaml":
		// Build a fragment holding each dotted key and replace those keys.
		frag := map[string]any{}
		var paths []string
		for k, v := range values {
			paths = append(paths, k)
			m, parts := frag, strings.Split(k, ".")
			for _, p := range parts[:len(parts)-1] {
				next, ok := m[p].(map[string]any)
				if !ok {
					next = map[string]any{}
					m[p] = next
				}
				m = next
			}
			m[parts[len(parts)-1]] = v
		}
		b, err := yaml.Marshal(frag)
		if err != nil {
			return nil, err
		}
		slices.Sort(paths)
		return configedit.YAMLKeys(src, b, paths)
	}
	return nil, fmt.Errorf("setting keys in %s files is not supported", format)
}

// chooseIntegration picks the first integration whose versions range
// contains version. With an unknown version, the first integration without
// a range, or else the first one, is used. A port counts only for the
// themes it has a flavor for.
func chooseIntegration(ins []registry.Integration, version, themeID string) *registry.Integration {
	var usable []int
	for i, in := range ins {
		if in.Method == registry.Port {
			if _, ok := in.Flavors[themeID]; !ok {
				continue
			}
		}
		usable = append(usable, i)
	}
	for _, i := range usable {
		in := ins[i]
		r, _ := registry.ParseRange(in.Versions)
		if version == "" {
			if in.Versions == "" {
				return &ins[i]
			}
			continue
		}
		if ok, err := r.Contains(version); err == nil && ok {
			return &ins[i]
		}
	}
	if version == "" && len(usable) > 0 {
		return &ins[usable[0]]
	}
	return nil
}

func (o Options) installed(e registry.Entry) bool {
	for _, b := range e.Detect.Binaries {
		if _, err := o.Sys.LookPath(b); err == nil {
			return true
		}
	}
	if o.Platform == registry.MacOS {
		for _, app := range e.Detect.MacOSApps {
			for _, dir := range []string{"/Applications", "/Applications/Utilities", "/System/Applications", "/System/Applications/Utilities", filepath.Join(o.Home, "Applications")} {
				if fi, err := os.Stat(filepath.Join(dir, app)); err == nil && fi.IsDir() {
					return true
				}
			}
		}
	}
	return false
}

func (o Options) version(e registry.Entry) string {
	v := e.Detect.Version
	if v == nil {
		return ""
	}
	argv := slices.Clone(v.Argv)
	// The same program may be installed under another of its names (helix
	// is hx elsewhere): run the version command with the one found.
	if _, err := o.Sys.LookPath(argv[0]); err != nil && slices.Contains(e.Detect.Binaries, argv[0]) {
		for _, b := range e.Detect.Binaries {
			if _, err := o.Sys.LookPath(b); err == nil {
				argv[0] = b
				break
			}
		}
	}
	out, err := o.Sys.Run(argv)
	if err != nil {
		return ""
	}
	if m := regexp.MustCompile(v.Regex).FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// configPath is the config file the app reads: from its environment
// variable if set, else the first existing candidate, else the first.
func (o Options) configPath(e registry.Entry) string {
	paths := e.Config.Paths[o.Platform]
	for _, ev := range e.Config.Env {
		v := o.Getenv(ev.Name)
		if ev.Kind == "file" {
			parts := strings.Split(v, ",")
			v = parts[len(parts)-1]
		}
		// A relative value depends on the directory Theme Tender runs in,
		// which need not be the app's; it is ignored, as for XDG_*.
		if !filepath.IsAbs(v) {
			continue
		}
		if ev.Kind == "file" {
			return v
		}
		return filepath.Join(v, filepath.Base(o.expand(paths[0])))
	}
	for _, p := range paths {
		if _, err := os.Stat(o.expand(p)); err == nil {
			return o.expand(p)
		}
	}
	return o.expand(paths[0])
}

func (o Options) ownedFile(config, owned string) string {
	if strings.HasPrefix(owned, "$") {
		return o.expand(owned)
	}
	return filepath.Join(filepath.Dir(config), owned)
}

func (o Options) expand(p string) string {
	cfg := o.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(cfg) {
		cfg = filepath.Join(o.Home, ".config")
	}
	if rest, ok := strings.CutPrefix(p, "$MAC_CONFIG_HOME/"); ok {
		mac := o.Getenv("XDG_CONFIG_HOME")
		if !filepath.IsAbs(mac) {
			mac = filepath.Join(o.Home, "Library", "Application Support")
		}
		return filepath.Join(mac, rest)
	}
	p = strings.Replace(p, "$XDG_CONFIG_HOME", cfg, 1)
	return strings.Replace(p, "$HOME", o.Home, 1)
}

func readFile(p string) ([]byte, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// Result is what Execute did.
type Result struct {
	Tx       *changes.Transaction
	Commands []CommandResult // reloads and the Omarchy switch
}

// CommandResult is one command Theme Tender ran after writing.
type CommandResult struct {
	App    string
	Argv   []string
	Output string
	Err    error
}

// Execute applies a plan: every edit goes through the change record, each
// app's validate command runs before the transaction completes, and then
// reload commands and the Omarchy switch run for apps that kept their
// changes.
func Execute(o Options, p *Plan) (*Result, error) {
	var edits []changes.Edit
	apps := map[string]AppPlan{}
	for _, ap := range p.Apps {
		if ap.Skip == "" {
			edits = append(edits, ap.Edits...)
			apps[ap.ID] = ap
		}
	}
	edits = append(edits, p.Omarchy...)
	validate := func(app string, _ []string) error {
		ap, ok := apps[app]
		if !ok || ap.Entry.Validate == nil {
			return nil
		}
		argv := substitute(ap.Entry.Validate.Argv, ap)
		if out, err := o.Sys.Run(argv); err != nil {
			return fmt.Errorf("%s: %s: %w: %s", app, strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	tx, err := o.Store.Apply(o.Meta, edits, validate)
	if err != nil {
		return nil, err
	}
	res := &Result{Tx: tx}
	ok := map[string]bool{} // apps whose changes were kept
	for _, c := range tx.Changes {
		ok[c.App] = c.Status != "failed"
	}
	for _, ap := range p.Apps {
		if ap.Skip == "" && ok[ap.ID] {
			res.Commands = append(res.Commands, o.reload(ap)...)
		}
	}
	if len(p.OmarchySwitch) > 0 && ok["omarchy"] {
		out, err := o.Sys.Run(p.OmarchySwitch)
		res.Commands = append(res.Commands, CommandResult{App: "omarchy", Argv: p.OmarchySwitch, Output: string(out), Err: err})
	}
	return res, nil
}

func (o Options) reload(ap AppPlan) []CommandResult {
	var out []CommandResult
	for _, r := range ap.Entry.Reload {
		if len(r.Platforms) > 0 && !slices.Contains(r.Platforms, o.Platform) {
			continue
		}
		if r.When == "running" && (len(ap.Entry.Detect.Binaries) == 0 || !o.Sys.Running(ap.Entry.Detect.Binaries[0])) {
			continue
		}
		argv := substitute(r.Argv, ap)
		b, err := o.Sys.Run(argv)
		out = append(out, CommandResult{App: ap.ID, Argv: argv, Output: string(b), Err: err})
	}
	return out
}

func substitute(argv []string, ap AppPlan) []string {
	out := slices.Clone(argv)
	for i := range out {
		out[i] = strings.NewReplacer("{config}", ap.Config, "{theme_file}", ap.ThemeFile).Replace(out[i])
	}
	return out
}

// Uninstall restores every file Theme Tender changed, switches Omarchy
// back to the theme that was active before Theme Tender's first apply
// (if a Theme Tender theme is still active), and reloads the apps.
func Uninstall(o Options, force bool) (*Result, []changes.Conflict, error) {
	txs, err := o.Store.Transactions()
	if err != nil {
		return nil, nil, err
	}
	previous := ""
	for _, tx := range txs {
		for _, c := range tx.Changes {
			if prev, ok := c.Detail["previous_theme"].(string); ok && c.App == "omarchy" && previous == "" && tx.Kind == "apply" {
				previous = prev
			}
		}
	}
	tx, conflicts, err := o.Store.Uninstall(force)
	if err != nil {
		return nil, nil, err
	}
	res := &Result{Tx: tx}
	if cur := o.Owners.System().OmarchyTheme; previous != "" && strings.HasPrefix(cur, omarchy.Prefix) {
		argv := omarchy.SwitchCommand(previous)
		out, err := o.Sys.Run(argv)
		res.Commands = append(res.Commands, CommandResult{App: "omarchy", Argv: argv, Output: string(out), Err: err})
	}
	done := map[string]bool{}
	for _, c := range tx.Changes {
		if e, ok := o.Registry.Get(c.App); ok && !done[c.App] {
			done[c.App] = true
			// The theme file is gone, so reloads that read it are skipped.
			e.Reload = slices.DeleteFunc(slices.Clone(e.Reload), func(r registry.Reload) bool {
				return slices.ContainsFunc(r.Argv, func(a string) bool { return strings.Contains(a, "{theme_file}") })
			})
			res.Commands = append(res.Commands, o.reload(AppPlan{ID: c.App, Entry: e, Config: o.configPath(e)})...)
		}
	}
	return res, conflicts, nil
}
