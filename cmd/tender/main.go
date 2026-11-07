// Command tender applies one palette across installed apps and can undo
// every change. See README.md and docs/plan/m1.md.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	themetender "git.subcult.tv/subculture-collective/theme-tender"
	"git.subcult.tv/subculture-collective/theme-tender/internal/apply"
	"git.subcult.tv/subculture-collective/theme-tender/internal/bundle"
	"git.subcult.tv/subculture-collective/theme-tender/internal/catalog"
	"git.subcult.tv/subculture-collective/theme-tender/internal/changes"
	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
	"git.subcult.tv/subculture-collective/theme-tender/internal/owner"
	"git.subcult.tv/subculture-collective/theme-tender/internal/profile"
	"git.subcult.tv/subculture-collective/theme-tender/internal/registry"
	"git.subcult.tv/subculture-collective/theme-tender/internal/render"
	"git.subcult.tv/subculture-collective/theme-tender/internal/tui"

	tea "charm.land/bubbletea/v2"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: tender <command> [flags]

Commands:
  search [words] [-tag t]... [-similar id] [-n 20]   find themes in the catalog
  apply [-dry-run] [-yes] [-profile p] [theme]       theme every supported app (theme: catalog ID or palette file)
  palette [-format json|toml] <theme>                print the resolved palette, changing nothing
  render [-o file] [-aether] <theme> <template>      render one template, changing nothing
  uninstall [-yes] [-force]                          put every changed file back
  tui                                                browse, preview, apply and undo in the terminal
  list                                               show supported apps and their state
  doctor [-recover]                                  check for interrupted runs and owners
  update [-url URL]                                  install the newest signed data bundle
  profiles                                           list profiles in $XDG_CONFIG_HOME/theme-tender/profiles
  version                                            print the version

Common flags: -bundle PATH, -catalog PATH, -data DIR, -state DIR
`

// env is everything run reads from the outside world. Tests replace it.
type env struct {
	home   string
	getenv func(string) string
	sys    apply.System
	get    catalog.Getter // theme downloads
	fetch  catalog.Getter // release listings and bundles, which are larger
	owners owner.Env
	keys   []string // trusted bundle signing keys
	root   string   // prefix for system paths such as /Applications; tests set it
}

func main() {
	home, _ := os.UserHomeDir()
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, env{
		home: home, getenv: os.Getenv, sys: realSystem{}, get: httpGetter(1 << 20), fetch: httpGetter(256 << 20), owners: owner.SystemEnv(), keys: bundle.TrustedKeys,
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, e env) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	c := &cli{env: e, in: bufio.NewReader(stdin), out: stdout, errw: stderr}
	var err error
	switch args[0] {
	case "search":
		err = c.search(args[1:])
	case "apply":
		err = c.apply(args[1:])
	case "palette":
		err = c.palette(args[1:])
	case "render":
		err = c.render(args[1:])
	case "uninstall":
		err = c.uninstall(args[1:])
	case "list":
		err = c.list(args[1:])
	case "tui":
		err = c.tui(args[1:])
	case "doctor":
		err = c.doctor(args[1:])
	case "profiles":
		err = c.profiles(args[1:])
	case "update":
		err = c.update(args[1:])
	case "version":
		fmt.Fprintln(stdout, "tender", version)
	default:
		fmt.Fprintf(stderr, "tender: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var bad usageError
	if errors.As(err, &bad) {
		return 2 // flag has already said what is wrong
	}
	if err != nil {
		fmt.Fprintln(stderr, "tender:", err)
		return 1
	}
	return 0
}

type cli struct {
	env
	in   *bufio.Reader
	out  io.Writer
	errw io.Writer // warnings, so they never mix into palette or render output

	catalogPath, dataDir, stateDir, bundlePath string
	data                                       *bundle.Bundle
}

func (c *cli) flags(name string) *flag.FlagSet {
	fl := flag.NewFlagSet("tender "+name, flag.ContinueOnError)
	fl.SetOutput(c.errw)
	fl.StringVar(&c.catalogPath, "catalog", "", "catalog.db or catalog.db.zst (default $TENDER_CATALOG or $XDG_DATA_HOME/theme-tender/catalog.db.zst)")
	fl.StringVar(&c.dataDir, "data", "", "directory holding registry/ and templates/ (default: built in)")
	fl.StringVar(&c.stateDir, "state", "", "change record directory (default $XDG_STATE_HOME/theme-tender)")
	fl.StringVar(&c.bundlePath, "bundle", "", "signed data bundle (default $XDG_DATA_HOME/theme-tender/bundle.tar.zst); unsigned or altered bundles are ignored")
	return fl
}

// usageError is a flag the command does not take; flag has printed why.
type usageError struct{ error }

// parse parses flags anywhere among the arguments, so
// "tender search catppuccin -tag dark" works as written in the usage.
// Arguments after "--" are never flags. Help goes to stdout.
func (c *cli) parse(fl *flag.FlagSet, args []string) error {
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "-h" || a == "-help" || a == "--help" {
			fl.SetOutput(c.out)
		}
	}
	var words []string
	for len(args) > 0 {
		if err := fl.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return err
			}
			return usageError{err}
		}
		rest := fl.Args()
		if len(rest) == 0 {
			break
		}
		if used := len(args) - len(rest); used > 0 && args[used-1] == "--" {
			words = append(words, rest...)
			break
		}
		words, args = append(words, rest[0]), rest[1:]
	}
	return fl.Parse(append([]string{"--"}, words...))
}

// bundle is the signed bundle if one is installed and verifies, else the
// built-in one.
func (c *cli) bundle() (*bundle.Bundle, error) {
	if c.data != nil {
		return c.data, nil
	}
	builtin, err := themetender.Builtin()
	if err != nil {
		return nil, err
	}
	b, warn := bundle.Load(c.bundleFile(), c.keys, builtin)
	if warn != "" {
		fmt.Fprintln(c.errw, "warning:", warn)
	}
	c.data = b
	return b, nil
}

func (c *cli) bundleFile() string {
	if c.bundlePath != "" {
		return c.bundlePath
	}
	return filepath.Join(c.xdg("XDG_DATA_HOME", ".local/share"), "theme-tender", "bundle.tar.zst")
}

// update installs the newest published bundle if it is newer than the one
// in use and verifies.
func (c *cli) update(args []string) error {
	fl := c.flags("update")
	url := fl.String("url", bundle.DefaultReleases, "Gitea API release listing to read")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	cur, err := c.bundle()
	if err != nil {
		return err
	}
	listing, err := c.fetch(*url)
	if err != nil {
		return fmt.Errorf("reading the release list: %w", err)
	}
	rel, err := bundle.LatestRelease(listing)
	if err != nil {
		return err
	}
	if !bundle.Newer(rel.Version, cur.Manifest.BundleVersion) {
		fmt.Fprintf(c.out, "Data bundle %s is up to date (newest published: %s).\n", cur.Manifest.BundleVersion, rel.Version)
		return nil
	}
	data, err := c.fetch(rel.BundleURL)
	if err != nil {
		return fmt.Errorf("downloading bundle %s: %w", rel.Version, err)
	}
	sig, err := c.fetch(rel.SigURL)
	if err != nil {
		return fmt.Errorf("downloading the signature of bundle %s: %w", rel.Version, err)
	}
	b, err := bundle.Install(c.bundleFile(), data, sig, c.keys, rel.Version)
	if err != nil {
		return fmt.Errorf("bundle %s not installed: %w", rel.Version, err)
	}
	fmt.Fprintf(c.out, "Installed data bundle %s (was %s) at %s.\n", rel.Version, cur.Manifest.BundleVersion, b.Origin)
	return nil
}

func (c *cli) xdg(name, fallback string) string {
	if v := c.getenv(name); filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(c.home, fallback)
}

func (c *cli) openCatalog() (*catalog.DB, error) {
	p := c.catalogPath
	if p == "" {
		p = c.getenv("TENDER_CATALOG")
	}
	if p != "" {
		return catalog.Open(p)
	}
	b, err := c.bundle()
	if err != nil {
		return nil, err
	}
	if b.Catalog == nil {
		return nil, fmt.Errorf("no catalog: this build has none built in and no signed bundle is installed (build one with make catalog, then pass -catalog)")
	}
	return catalog.OpenBytes(b.Catalog)
}

func (c *cli) options() (apply.Options, error) {
	b, err := c.bundle()
	if err != nil {
		return apply.Options{}, err
	}
	regFS, tmplFS := b.Registry, b.Templates
	if c.dataDir != "" {
		regFS, _ = fs.Sub(os.DirFS(c.dataDir), "registry")
		tmplFS, _ = fs.Sub(os.DirFS(c.dataDir), "templates")
	}
	userDir := filepath.Join(c.xdg("XDG_CONFIG_HOME", ".config"), "theme-tender", "registry.d")
	reg, err := registry.Load(regFS, userDir)
	if err != nil {
		return apply.Options{}, err
	}
	for _, p := range reg.Problems {
		fmt.Fprintln(c.errw, "warning:", p)
	}
	state := c.stateDir
	if state == "" {
		state = filepath.Join(c.xdg("XDG_STATE_HOME", ".local/state"), "theme-tender")
	}
	store, err := changes.Open(state)
	if err != nil {
		return apply.Options{}, err
	}
	det, err := owner.New(c.owners)
	if err != nil {
		return apply.Options{}, err
	}
	platform := registry.Linux
	if runtime.GOOS == "darwin" {
		platform = registry.MacOS
	}
	return apply.Options{
		Home: c.home, Platform: platform, Getenv: c.getenv, Registry: reg, Templates: tmplFS,
		Store: store, Owners: det, Sys: c.sys, Meta: changes.Meta{TenderVersion: version, BundleVersion: b.Manifest.BundleVersion},
		Root: c.root, Get: c.get, CacheDir: filepath.Join(c.xdg("XDG_CACHE_HOME", ".cache"), "theme-tender"),
	}, nil
}

func (c *cli) ask(question string) bool {
	fmt.Fprintf(c.out, "%s [y/N] ", question)
	line, _ := c.in.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func (c *cli) search(args []string) error {
	fl := c.flags("search")
	var tags multi
	fl.Var(&tags, "tag", "require a tag (repeatable): dark, light, pastel, neon, muted, warm, cool, high-contrast, low-contrast")
	similar := fl.String("similar", "", "list the themes closest to this theme ID")
	n := fl.Int("n", 20, "maximum number of results")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	db, err := c.openCatalog()
	if err != nil {
		return err
	}
	defer db.Close()
	var res []catalog.Info
	if *similar != "" {
		res, err = db.Similar(*similar, *n)
	} else {
		res, err = db.Search(strings.Join(fl.Args(), " "), tags, *n)
	}
	if err != nil {
		return err
	}
	for _, i := range res {
		mark := ""
		if !i.Bundled {
			mark = "  (downloaded on first use)"
		}
		fmt.Fprintf(c.out, "%-32s %-28s %s%s\n", i.ID, i.Name, strings.Join(i.Tags, ","), mark)
	}
	if len(res) == 0 {
		fmt.Fprintln(c.out, "no themes found")
	}
	return nil
}

func (c *cli) apply(args []string) error {
	fl := c.flags("apply")
	dry := fl.Bool("dry-run", false, "show the plan and diffs, change nothing")
	yes := fl.Bool("yes", false, "apply without asking for final confirmation (files other tools own are still asked about)")
	profName := fl.String("profile", "", "profile name (in $XDG_CONFIG_HOME/theme-tender/profiles) or file: theme, apps and font")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	var prof *profile.Profile
	if *profName != "" {
		var err error
		if prof, err = profile.Load(*profName, c.profileDir()); err != nil {
			return err
		}
	}
	themeID := ""
	switch {
	case fl.NArg() == 1:
		themeID = fl.Arg(0)
	case fl.NArg() == 0 && prof != nil && prof.Theme != "":
		themeID = prof.Theme
	default:
		return fmt.Errorf("apply needs one theme ID or palette file (find one with tender search), or a profile that names one")
	}
	th, info, err := c.theme(themeID, true)
	if err != nil {
		return err
	}
	o, err := c.options()
	if err != nil {
		return err
	}
	if prof != nil {
		o.Wants = prof.Wants
		th.Font = prof.Font
		c.checkFont(prof.Font)
	}
	o.Meta.Theme = info.ID
	o.Confirm = func(app string, ow owner.Owner) bool {
		if *dry {
			return true
		}
		return c.ask(fmt.Sprintf("%s: %s, so writing it changes %s's files. Write it anyway?", app, ow.Detail, ow.Record()))
	}
	plan, err := apply.Prepare(o, th)
	if err != nil {
		return err
	}
	changed := len(plan.Omarchy)
	for _, ap := range plan.Apps {
		for _, n := range ap.Notes {
			fmt.Fprintf(c.out, "%-10s note: %s\n", ap.ID, n)
		}
		if ap.Skip != "" {
			fmt.Fprintf(c.out, "%-10s skip: %s\n", ap.ID, ap.Skip)
			continue
		}
		fmt.Fprintf(c.out, "%-10s %s via %s, %d file(s)\n", ap.ID, ap.Config, ap.Integration.Method, len(ap.Edits))
		changed += len(ap.Edits)
	}
	if len(plan.Omarchy) > 0 {
		fmt.Fprintf(c.out, "%-10s theme %s, then %s\n", "omarchy", filepath.Dir(plan.Omarchy[0].Path), strings.Join(plan.OmarchySwitch, " "))
	}
	for _, ap := range plan.Apps {
		for _, d := range ap.Diffs {
			fmt.Fprint(c.out, "\n", d)
		}
	}
	if changed == 0 {
		fmt.Fprintln(c.out, "\nNothing to change.")
		return nil
	}
	if *dry {
		return nil
	}
	if !*yes && !c.ask(fmt.Sprintf("\nApply %s to these files?", info.Name)) {
		fmt.Fprintln(c.out, "Nothing changed.")
		return nil
	}
	res, err := apply.Execute(o, plan)
	if err != nil {
		return err
	}
	return c.report(res, "Applied "+info.Name)
}

// loadTheme reads a theme's palette, downloading an unbundled one.
// theme loads a catalog theme, or a palette file when id is a path.
func (c *cli) theme(id string, say bool) (apply.Theme, catalog.Info, error) {
	if profile.IsPath(id) {
		t, p, err := profile.LoadPalette(id)
		info := catalog.Info{ID: t.Slug, Name: t.Name, Author: t.Author, Appearance: t.Appearance, License: "user", Bundled: true}
		return apply.Theme{Theme: t, Palette: p}, info, err
	}
	db, err := c.openCatalog()
	if err != nil {
		return apply.Theme{}, catalog.Info{}, err
	}
	defer db.Close()
	return c.loadTheme(db, id, say)
}

func (c *cli) profileDir() string {
	return filepath.Join(c.xdg("XDG_CONFIG_HOME", ".config"), "theme-tender", "profiles")
}

// checkFont warns when a profile's font is not installed. Only Linux has
// fontconfig to ask; Theme Tender never installs fonts.
func (c *cli) checkFont(f *render.Font) {
	if f == nil || runtime.GOOS != "linux" {
		return
	}
	out, err := c.sys.Run([]string{"fc-list", ":", "family"})
	if err != nil {
		return
	}
	if !strings.Contains(strings.ToLower(string(out)), strings.ToLower(f.Family)) {
		fmt.Fprintf(c.errw, "warning: font %q is not installed (fc-list); terminals will fall back to another font\n", f.Family)
	}
}

// profiles lists the user's profiles.
func (c *cli) profiles(args []string) error {
	fl := c.flags("profiles")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	names, err := profile.List(c.profileDir())
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintf(c.out, "No profiles in %s. See docs/reference/profiles.md.\n", c.profileDir())
	}
	for _, n := range names {
		p, err := profile.Load(n, c.profileDir())
		if err != nil {
			fmt.Fprintf(c.out, "%-16s error: %v\n", n, err)
			continue
		}
		apps := "all apps"
		if len(p.Apps) > 0 {
			apps = strings.Join(p.Apps, ",")
		}
		font := ""
		if p.Font != nil {
			font = ", font " + p.Font.Family
		}
		fmt.Fprintf(c.out, "%-16s theme %s, %s%s\n", n, orUnknown(p.Theme), apps, font)
	}
	return nil
}

func (c *cli) loadTheme(db *catalog.DB, id string, say bool) (apply.Theme, catalog.Info, error) {
	info, err := db.Info(id)
	if err != nil {
		return apply.Theme{}, info, err
	}
	th := apply.Theme{Theme: render.Theme{Name: info.Name, Slug: info.ID, Author: info.Author, Appearance: info.Appearance}}
	if info.Bundled {
		th.Palette, err = db.Palette(info.ID)
	} else {
		if say {
			fmt.Fprintf(c.out, "%s (licence %s) is not bundled; downloading it from %s.\n", info.ID, info.License, info.SourceID)
		}
		th.Palette, err = db.Download(info, filepath.Join(c.xdg("XDG_CACHE_HOME", ".cache"), "theme-tender", "themes"), c.get)
	}
	return th, info, err
}

func (c *cli) report(res *apply.Result, done string) error {
	failed := 0
	for _, ch := range res.Tx.Changes {
		if ch.Status == "failed" {
			failed++
		}
	}
	for _, r := range res.Commands {
		if r.Err != nil {
			fmt.Fprintf(c.out, "%s: %s failed: %v %s\n", r.App, strings.Join(r.Argv, " "), r.Err, strings.TrimSpace(r.Output))
		}
	}
	fmt.Fprintf(c.out, "%s: %d file(s) changed, transaction %s (%s).\n", done, len(res.Tx.Changes)-failed, res.Tx.ID, res.Tx.Status)
	if res.Tx.Error != "" {
		fmt.Fprintln(c.out, "Validation failed, those apps were restored:", res.Tx.Error)
	}
	return nil
}

func (c *cli) uninstall(args []string) error {
	fl := c.flags("uninstall")
	yes := fl.Bool("yes", false, "don't ask for confirmation")
	force := fl.Bool("force", false, "also restore files edited since Theme Tender wrote them")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	o, err := c.options()
	if err != nil {
		return err
	}
	st, err := o.Store.State()
	if err != nil {
		return err
	}
	if len(st.Files) == 0 {
		fmt.Fprintln(c.out, "Theme Tender has not changed any file.")
		return nil
	}
	for _, f := range st.Files {
		fmt.Fprintf(c.out, "%-10s %s\n", f.App, f.Path)
	}
	if !*yes && !c.ask(fmt.Sprintf("Restore these %d files?", len(st.Files))) {
		fmt.Fprintln(c.out, "Nothing changed.")
		return nil
	}
	res, conflicts, err := apply.Uninstall(o, *force)
	if err != nil {
		return err
	}
	for _, cf := range conflicts {
		if cf.Reason == "missing" {
			fmt.Fprintf(c.out, "%s is missing; skipped.\n", cf.Path)
			continue
		}
		fmt.Fprintf(c.out, "%s was edited after Theme Tender wrote it; left as it is (use -force to restore it):\n%s", cf.Path, apply.Diff(cf.Path, cf.Current, cf.Before))
	}
	return c.report(res, "Uninstalled")
}

func (c *cli) list(args []string) error {
	fl := c.flags("list")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	o, err := c.options()
	if err != nil {
		return err
	}
	apps, err := apply.Survey(o)
	if err != nil {
		return err
	}
	for _, ap := range apps {
		state := ap.Skip
		if state == "" {
			state = fmt.Sprintf("version %s, %s, owner %s", orUnknown(ap.Version), ap.Integration.Method, ap.Owner.Record())
		}
		fmt.Fprintf(c.out, "%-10s tier %d  %s\n", ap.ID, ap.Entry.Tier, state)
	}
	return nil
}

func (c *cli) doctor(args []string) error {
	fl := c.flags("doctor")
	doRecover := fl.Bool("recover", false, "roll back interrupted runs")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	o, err := c.options()
	if err != nil {
		return err
	}
	txs, err := o.Store.Transactions()
	if err != nil {
		return err
	}
	pending := 0
	for _, tx := range txs {
		if tx.Status == "pending" {
			pending++
			fmt.Fprintf(c.out, "Interrupted %s from %s: %d planned change(s).\n", tx.Kind, tx.Started.Format(time.RFC3339), len(tx.Changes))
		}
	}
	if pending > 0 && *doRecover {
		rec, conflicts, err := o.Store.Recover()
		if err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Rolled back %d interrupted run(s).\n", len(rec))
		for _, cf := range conflicts {
			fmt.Fprintf(c.out, "%s changed since; left as it is.\n", cf.Path)
		}
	} else if pending > 0 {
		fmt.Fprintln(c.out, "Run tender doctor -recover to roll them back.")
	} else {
		fmt.Fprintln(c.out, "No interrupted runs.")
	}
	st, err := o.Store.State()
	if err != nil {
		return err
	}
	theme := st.Theme
	if theme == "" {
		theme = "none"
	}
	fmt.Fprintf(c.out, "Active theme: %s; %d file(s) changed.\n", theme, len(st.Files))
	if len(st.Files) > 0 {
		ups, err := apply.Upgrades(o)
		if err != nil {
			return err
		}
		if len(ups) == 0 {
			fmt.Fprintln(c.out, "No app upgrades since the last apply.")
		}
		for _, u := range ups {
			fmt.Fprintf(c.out, "%s upgraded from %s to %s since the last apply: ", u.App, u.Was, u.Now)
			switch {
			case u.WasIntegration != u.NowIntegration:
				fmt.Fprintf(c.out, "Theme Tender would now use %s instead of %s. Run tender apply %s again.\n", u.NowIntegration, u.WasIntegration, st.Theme)
			case u.ValidateErr != nil:
				fmt.Fprintf(c.out, "its validator rejects the current files (%v). Run tender apply %s again.\n", u.ValidateErr, st.Theme)
			case u.Validated:
				fmt.Fprintln(c.out, "same integration, and its validator accepts the current files.")
			default:
				fmt.Fprintln(c.out, "same integration; it has no validator to check the files with.")
			}
		}
	}
	if b, err := c.bundle(); err == nil {
		fmt.Fprintf(c.out, "Data bundle: %s, version %s, catalog %s.\n", b.Origin, b.Manifest.BundleVersion, map[bool]string{true: "included", false: "not included"}[b.Catalog != nil])
	}
	sys := o.Owners.System()
	if sys.Subarchy {
		fmt.Fprintln(c.out, "Subarchy: yes. Its defaults in /usr/share/subcult/default are never edited.")
	}
	if sys.Omarchy {
		fmt.Fprintf(c.out, "Omarchy: yes, current theme %s.\n", sys.OmarchyTheme)
	} else {
		fmt.Fprintln(c.out, "Omarchy: no.")
	}
	if len(sys.Frameworks) > 0 {
		fmt.Fprintf(c.out, "Hyprland frameworks: %s. Theme Tender asks before changing their files.\n", strings.Join(sys.Frameworks, ", "))
	}
	if len(sys.Generators) > 0 {
		fmt.Fprintf(c.out, "Palette generators: %s.\n", strings.Join(sys.Generators, ", "))
	}
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

type realSystem struct{}

func (realSystem) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (realSystem) Run(argv []string) ([]byte, error) {
	return exec.Command(argv[0], argv[1:]...).CombinedOutput()
}

func (realSystem) Running(name string) bool { return exec.Command("pgrep", "-x", name).Run() == nil }

// httpGetter fetches a URL's body, refusing bodies over limit bytes.
func httpGetter(limit int64) catalog.Getter {
	return func(url string) ([]byte, error) {
		client := &http.Client{Timeout: 2 * time.Minute}
		resp, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", url, resp.Status)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err == nil && int64(len(b)) > limit {
			err = fmt.Errorf("%s: larger than %d bytes", url, limit)
		}
		return b, err
	}
}

func (c *cli) tui(args []string) error {
	fl := c.flags("tui")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	b, err := c.tuiBackend()
	if err != nil {
		return err
	}
	defer b.db.Close()
	_, err = tea.NewProgram(tui.New(b)).Run()
	return err
}

func (c *cli) tuiBackend() (*tuiBackend, error) {
	db, err := c.openCatalog()
	if err != nil {
		return nil, err
	}
	o, err := c.options()
	if err != nil {
		db.Close()
		return nil, err
	}
	return &tuiBackend{c: c, db: db, o: o}, nil
}

// tuiBackend connects the TUI to the catalog and the apply pipeline. Files
// other tools own are never confirmed here: Confirm stays nil, so they are
// skipped and shown as owned.
type tuiBackend struct {
	c    *cli
	db   *catalog.DB
	o    apply.Options
	plan *apply.Plan
}

func (b *tuiBackend) Search(q string, tags []string) ([]catalog.Info, error) {
	return b.db.Search(q, tags, 500)
}

func (b *tuiBackend) Similar(id string) ([]catalog.Info, error) { return b.db.Similar(id, 30) }

func (b *tuiBackend) Palette(id string) (color.Palette, error) { return b.db.Palette(id) }

func (b *tuiBackend) Apps() ([]tui.AppRow, error) {
	apps, err := apply.Survey(b.o)
	if err != nil {
		return nil, err
	}
	var out []tui.AppRow
	for _, ap := range apps {
		status := ap.Skip
		if status == "" {
			status = fmt.Sprintf("version %s, %s", orUnknown(ap.Version), ap.Integration.Method)
		}
		out = append(out, tui.AppRow{ID: ap.ID, Tier: ap.Entry.Tier, Owner: ap.Owner.Record(), Status: status})
	}
	return out, nil
}

func (b *tuiBackend) Plan(id string) ([]string, error) {
	th, _, err := b.c.loadTheme(b.db, id, false)
	if err != nil {
		return nil, err
	}
	o := b.o
	o.Meta.Theme = id
	if b.plan, err = apply.Prepare(o, th); err != nil {
		return nil, err
	}
	var lines []string
	for _, ap := range b.plan.Apps {
		for _, n := range ap.Notes {
			lines = append(lines, fmt.Sprintf("%-10s note: %s", ap.ID, n))
		}
		if ap.Skip != "" {
			lines = append(lines, fmt.Sprintf("%-10s skip: %s", ap.ID, ap.Skip))
		} else {
			lines = append(lines, fmt.Sprintf("%-10s %d file(s) via %s", ap.ID, len(ap.Edits), ap.Integration.Method))
		}
	}
	if len(b.plan.Omarchy) > 0 {
		lines = append(lines, fmt.Sprintf("%-10s theme %s", "omarchy", strings.Join(b.plan.OmarchySwitch[1:], " ")))
	}
	return lines, nil
}

func (b *tuiBackend) Apply(id string) (string, error) {
	if b.plan == nil || b.plan.Theme.Slug != id {
		if _, err := b.Plan(id); err != nil {
			return "", err
		}
	}
	o := b.o
	o.Meta.Theme = id
	res, err := apply.Execute(o, b.plan)
	b.plan = nil
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("Applied %s: transaction %s (%s).", id, res.Tx.ID, res.Tx.Status)
	if res.Tx.Error != "" {
		msg += " Validation failed, those apps were restored: " + res.Tx.Error
	}
	return msg, nil
}

// Undo rolls back the newest apply that has not been rolled back.
func (b *tuiBackend) Undo() (string, error) {
	txs, err := b.o.Store.Transactions()
	if err != nil {
		return "", err
	}
	undone := map[string]bool{}
	for _, tx := range txs {
		if tx.Kind == "rollback" {
			undone[tx.RollsBack] = true
		}
	}
	for i := len(txs) - 1; i >= 0; i-- {
		tx := txs[i]
		if tx.Kind == "uninstall" {
			break
		}
		if tx.Kind != "apply" || undone[tx.ID] || (tx.Status != "complete" && tx.Status != "failed") {
			continue
		}
		rb, conflicts, err := b.o.Store.Rollback(tx.ID, false)
		if err != nil {
			return "", err
		}
		msg := fmt.Sprintf("Rolled back %s (%d file(s)).", tx.Theme, len(rb.Changes))
		if len(conflicts) > 0 {
			msg += fmt.Sprintf(" %d file(s) changed since were left alone; tender uninstall shows them.", len(conflicts))
		}
		return msg, nil
	}
	return "Nothing to undo.", nil
}

// palette prints a theme's full palette: the 26 roles, ANSI 0-15 and the
// terminal specials, with derived roles filled in. Nothing is written.
// The format is docs/reference/palette-export.md.
func (c *cli) palette(args []string) error {
	fl := c.flags("palette")
	format := fl.String("format", "json", "json or toml")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return fmt.Errorf("palette needs one theme ID")
	}
	th, info, err := c.theme(fl.Arg(0), false)
	if err != nil {
		return err
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	var b strings.Builder
	switch *format {
	case "json":
		fmt.Fprintf(&b, "{\n  \"schema\": 1,\n  \"theme\": {\"id\": %s, \"name\": %s, \"author\": %s, \"appearance\": %s, \"license\": %s},\n  \"colors\": {\n",
			q(info.ID), q(info.Name), q(info.Author), q(info.Appearance), q(info.License))
		roles := color.AllRoles()
		for i, r := range roles {
			sep := ","
			if i == len(roles)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "    %s: %s%s\n", q(string(r)), q(th.Palette[r].Hex()), sep)
		}
		b.WriteString("  }\n}\n")
	case "toml":
		fmt.Fprintf(&b, "schema = 1\n\n[theme]\nid = %s\nname = %s\nauthor = %s\nappearance = %s\nlicense = %s\n\n[colors]\n",
			q(info.ID), q(info.Name), q(info.Author), q(info.Appearance), q(info.License))
		for _, r := range color.AllRoles() {
			fmt.Fprintf(&b, "%s = %s\n", r, q(th.Palette[r].Hex()))
		}
	default:
		return fmt.Errorf("unknown format %q; use json or toml", *format)
	}
	_, err = io.WriteString(c.out, b.String())
	return err
}

// render renders one Mustache template, a file or a bundled template name
// such as kitty/theme-tender.conf.mustache, to stdout or -o. It does not
// go through the change record.
func (c *cli) render(args []string) error {
	fl := c.flags("render")
	out := fl.String("o", "", "write to this file instead of stdout")
	aether := fl.Bool("aether", false, "the template uses Aether's {name} placeholders")
	if err := c.parse(fl, args); err != nil {
		return err
	}
	if fl.NArg() != 2 {
		return fmt.Errorf("render needs a theme ID and a template")
	}
	th, _, err := c.theme(fl.Arg(0), false)
	if err != nil {
		return err
	}
	name := fl.Arg(1)
	src, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		o, oerr := c.options()
		if oerr != nil {
			return oerr
		}
		if src, err = fs.ReadFile(o.Templates, name); err != nil {
			return fmt.Errorf("%s is neither a file nor a bundled template", name)
		}
	} else if err != nil {
		return err
	}
	if *aether {
		conv, err := render.AetherToMustache(string(src))
		if err != nil {
			return err
		}
		src = []byte(conv)
	}
	tm, err := render.Parse(name, string(src))
	if err != nil {
		return err
	}
	vars, err := render.Vars(th.Theme, th.Palette)
	if err != nil {
		return err
	}
	text, err := tm.Render(vars)
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = io.WriteString(c.out, text)
		return err
	}
	return os.WriteFile(*out, []byte(text), 0o644)
}
