// Command tender applies one palette across installed apps and can undo
// every change. See README.md and docs/plan/m1.md.
package main

import (
	"bufio"
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
	"git.subcult.tv/subculture-collective/theme-tender/internal/catalog"
	"git.subcult.tv/subculture-collective/theme-tender/internal/changes"
	"git.subcult.tv/subculture-collective/theme-tender/internal/owner"
	"git.subcult.tv/subculture-collective/theme-tender/internal/registry"
	"git.subcult.tv/subculture-collective/theme-tender/internal/render"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: tender <command> [flags]

Commands:
  search [words] [-tag t]... [-similar id] [-n 20]   find themes in the catalog
  apply [-dry-run] [-yes] <theme>                    theme every supported app
  uninstall [-yes] [-force]                          put every changed file back
  list                                               show supported apps and their state
  doctor [-recover]                                  check for interrupted runs and owners
  version                                            print the version

Common flags: -catalog PATH, -data DIR, -state DIR
`

// env is everything run reads from the outside world. Tests replace it.
type env struct {
	home   string
	getenv func(string) string
	sys    apply.System
	get    catalog.Getter
	owners owner.Env
}

func main() {
	home, _ := os.UserHomeDir()
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, env{
		home: home, getenv: os.Getenv, sys: realSystem{}, get: httpGet, owners: owner.SystemEnv(),
	}))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, e env) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	c := &cli{env: e, in: bufio.NewReader(stdin), out: stdout}
	var err error
	switch args[0] {
	case "search":
		err = c.search(args[1:])
	case "apply":
		err = c.apply(args[1:])
	case "uninstall":
		err = c.uninstall(args[1:])
	case "list":
		err = c.list(args[1:])
	case "doctor":
		err = c.doctor(args[1:])
	case "version":
		fmt.Fprintln(stdout, "tender", version)
	default:
		fmt.Fprintf(stderr, "tender: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "tender:", err)
		return 1
	}
	return 0
}

type cli struct {
	env
	in  *bufio.Reader
	out io.Writer

	catalogPath, dataDir, stateDir string
}

func (c *cli) flags(name string) *flag.FlagSet {
	fl := flag.NewFlagSet("tender "+name, flag.ContinueOnError)
	fl.SetOutput(c.out)
	fl.StringVar(&c.catalogPath, "catalog", "", "catalog.db or catalog.db.zst (default $TENDER_CATALOG or $XDG_DATA_HOME/theme-tender/catalog.db.zst)")
	fl.StringVar(&c.dataDir, "data", "", "directory holding registry/ and templates/ (default: built in)")
	fl.StringVar(&c.stateDir, "state", "", "change record directory (default $XDG_STATE_HOME/theme-tender)")
	return fl
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
	if p == "" {
		p = filepath.Join(c.xdg("XDG_DATA_HOME", ".local/share"), "theme-tender", "catalog.db.zst")
	}
	db, err := catalog.Open(p)
	if err != nil {
		return nil, fmt.Errorf("%w (build one with make catalog, then pass -catalog)", err)
	}
	return db, nil
}

func (c *cli) options() (apply.Options, error) {
	var data fs.FS = themetender.Data
	if c.dataDir != "" {
		data = os.DirFS(c.dataDir)
	}
	regFS, _ := fs.Sub(data, "registry")
	tmplFS, _ := fs.Sub(data, "templates")
	userDir := filepath.Join(c.xdg("XDG_CONFIG_HOME", ".config"), "theme-tender", "registry.d")
	reg, err := registry.Load(regFS, userDir)
	if err != nil {
		return apply.Options{}, err
	}
	for _, p := range reg.Problems {
		fmt.Fprintln(c.out, "warning:", p)
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
		Store: store, Owners: det, Sys: c.sys, Meta: changes.Meta{TenderVersion: version},
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
	if err := fl.Parse(args); err != nil {
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
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return fmt.Errorf("apply needs one theme ID; find one with tender search")
	}
	db, err := c.openCatalog()
	if err != nil {
		return err
	}
	defer db.Close()
	info, err := db.Info(fl.Arg(0))
	if err != nil {
		return err
	}
	var th apply.Theme
	th.Theme = render.Theme{Name: info.Name, Slug: info.ID, Author: info.Author, Appearance: info.Appearance}
	if info.Bundled {
		th.Palette, err = db.Palette(info.ID)
	} else {
		fmt.Fprintf(c.out, "%s (licence %s) is not bundled; downloading it from %s.\n", info.ID, info.License, info.SourceID)
		th.Palette, err = db.Download(info, filepath.Join(c.xdg("XDG_CACHE_HOME", ".cache"), "theme-tender", "themes"), c.get)
	}
	if err != nil {
		return err
	}
	o, err := c.options()
	if err != nil {
		return err
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
	if err := fl.Parse(args); err != nil {
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
	if err := fl.Parse(args); err != nil {
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
	if err := fl.Parse(args); err != nil {
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
	sys := o.Owners.System()
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

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
