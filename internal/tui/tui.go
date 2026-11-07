// Package tui is Theme Tender's terminal interface: browse and search the
// catalog with tag filters, list similar themes, preview a theme's
// swatches, see each app's tier and owner, apply a theme and undo it.
package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.subcult.tv/subculture-collective/theme-tender/internal/catalog"
	"git.subcult.tv/subculture-collective/theme-tender/internal/color"
)

// Backend is what the interface needs from the rest of Theme Tender.
type Backend interface {
	Search(query string, tags []string) ([]catalog.Info, error)
	Similar(id string) ([]catalog.Info, error)
	// Palette returns a bundled theme's colors; unbundled themes return an error.
	Palette(id string) (color.Palette, error)
	Apps() ([]AppRow, error)
	// Plan describes what applying a theme would do, one line per app.
	Plan(id string) ([]string, error)
	Apply(id string) (string, error)
	// Undo rolls back the last apply.
	Undo() (string, error)
}

// AppRow is one line of the app list.
type AppRow struct {
	ID     string
	Tier   int
	Owner  string
	Status string
}

// Tags are the computed tags, toggled with keys 1 to 9.
var Tags = []string{"dark", "light", "pastel", "neon", "muted", "warm", "cool", "high-contrast", "low-contrast"}

type screen int

const (
	themesScreen screen = iota
	appsScreen
)

type confirm int

const (
	noConfirm confirm = iota
	confirmApply
	confirmUndo
)

// Model is the Bubble Tea model.
type Model struct {
	b       Backend
	screen  screen
	search  textinput.Model
	tags    map[string]bool
	similar string // theme the list shows neighbours of, or ""
	themes  []catalog.Info
	cursor  int
	apps    []AppRow
	confirm confirm
	plan    []string
	status  string
	width   int
	height  int
}

// New returns the model with the first search already run.
func New(b Backend) Model {
	in := textinput.New()
	in.Prompt = "Search: "
	in.Placeholder = "name, family or author"
	in.SetWidth(40)
	m := Model{b: b, search: in, tags: map[string]bool{}, width: 100, height: 30}
	m.refresh()
	return m
}

func (m *Model) refresh() {
	var res []catalog.Info
	var err error
	if m.similar != "" {
		res, err = m.b.Similar(m.similar)
	} else {
		var tags []string
		for _, t := range Tags {
			if m.tags[t] {
				tags = append(tags, t)
			}
		}
		res, err = m.b.Search(m.search.Value(), tags)
	}
	if err != nil {
		m.status = "error: " + err.Error()
		return
	}
	m.themes = res
	m.cursor = min(m.cursor, max(0, len(res)-1))
}

func (m Model) selected() (catalog.Info, bool) {
	if m.cursor >= 0 && m.cursor < len(m.themes) {
		return m.themes[m.cursor], true
	}
	return catalog.Info{}, false
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.confirm != noConfirm {
		c := m.confirm
		m.confirm, m.plan = noConfirm, nil
		if key != "y" {
			m.status = "Nothing changed."
			return m, nil
		}
		var msg string
		var err error
		if c == confirmApply {
			th, _ := m.selected()
			msg, err = m.b.Apply(th.ID)
		} else {
			msg, err = m.b.Undo()
		}
		if err != nil {
			msg = "error: " + err.Error()
		}
		m.status = msg
		if m.screen == appsScreen {
			m.loadApps()
		}
		return m, nil
	}
	if m.search.Focused() {
		switch key {
		case "esc", "enter":
			m.search.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(k)
		m.similar = ""
		m.refresh()
		return m, cmd
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "tab":
		if m.screen == themesScreen {
			m.screen = appsScreen
			m.loadApps()
		} else {
			m.screen = themesScreen
		}
		return m, nil
	case "u":
		m.confirm, m.status = confirmUndo, ""
		return m, nil
	}
	if m.screen != themesScreen {
		return m, nil
	}
	switch key {
	case "/":
		m.status = ""
		return m, m.search.Focus()
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = max(0, min(len(m.themes)-1, m.cursor+1))
	case "s":
		if th, ok := m.selected(); ok {
			m.similar, m.cursor = th.ID, 0
			m.refresh()
		}
	case "esc":
		m.similar = ""
		m.refresh()
	case "enter":
		th, ok := m.selected()
		if !ok {
			return m, nil
		}
		plan, err := m.b.Plan(th.ID)
		if err != nil {
			m.status = "error: " + err.Error()
			return m, nil
		}
		m.plan, m.confirm, m.status = plan, confirmApply, ""
	default:
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			t := Tags[key[0]-'1']
			m.tags[t] = !m.tags[t]
			m.similar, m.cursor = "", 0
			m.refresh()
		}
	}
	return m, nil
}

func (m *Model) loadApps() {
	apps, err := m.b.Apps()
	if err != nil {
		m.status = "error: " + err.Error()
		return
	}
	m.apps = apps
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	activeStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	cursorStyle = lipgloss.NewStyle().Bold(true)
)

// View implements tea.Model.
func (m Model) View() tea.View {
	var b strings.Builder
	tab := func(name string, s screen) string {
		if m.screen == s {
			return activeStyle.Render(name)
		}
		return dimStyle.Render(name)
	}
	fmt.Fprintf(&b, "%s  %s  %s\n", titleStyle.Render("Theme Tender"), tab("themes", themesScreen), tab("apps", appsScreen))
	b.WriteString(dimStyle.Render("tab switch · / search · 1-9 tags · s similar · esc clear · enter apply · u undo · q quit"))
	b.WriteString("\n\n")
	switch {
	case m.confirm == confirmApply:
		th, _ := m.selected()
		fmt.Fprintf(&b, "Apply %s?\n\n", th.Name)
		for _, l := range m.plan {
			b.WriteString("  " + l + "\n")
		}
		b.WriteString("\nFiles other tools own are left alone here; use tender apply to confirm them.\n[y/N] ")
	case m.confirm == confirmUndo:
		b.WriteString("Roll back the last apply? [y/N] ")
	case m.screen == appsScreen:
		m.viewApps(&b)
	default:
		m.viewThemes(&b)
	}
	if m.status != "" {
		b.WriteString("\n\n" + m.status)
	}
	return tea.NewView(b.String())
}

func (m Model) viewThemes(b *strings.Builder) {
	b.WriteString(m.search.View() + "\n")
	var tags []string
	for i, t := range Tags {
		label := fmt.Sprintf("%d %s", i+1, t)
		if m.tags[t] {
			tags = append(tags, activeStyle.Render(label))
		} else {
			tags = append(tags, dimStyle.Render(label))
		}
	}
	b.WriteString(strings.Join(tags, "  ") + "\n")
	if m.similar != "" {
		fmt.Fprintf(b, "Themes closest to %s (esc to go back)\n", m.similar)
	}
	b.WriteString("\n")
	rows := max(5, m.height-14)
	start := max(0, min(m.cursor-rows/2, len(m.themes)-rows))
	for i := start; i < min(len(m.themes), start+rows); i++ {
		th := m.themes[i]
		mark := " "
		if !th.Bundled {
			mark = "↓"
		}
		line := fmt.Sprintf("%s %-30s %-28s %s", mark, truncate(th.ID, 30), truncate(th.Name, 28), strings.Join(th.Tags, ","))
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if len(m.themes) == 0 {
		b.WriteString("  no themes found\n")
	}
	if th, ok := m.selected(); ok {
		b.WriteString("\n")
		if p, err := m.b.Palette(th.ID); err == nil {
			b.WriteString(swatches(p))
		} else {
			fmt.Fprintf(b, "%s is not bundled (licence %s); its colors are downloaded when you apply it.\n", th.Name, th.License)
		}
	}
}

func (m Model) viewApps(b *strings.Builder) {
	fmt.Fprintf(b, "%-10s %-4s %-22s %s\n", "app", "tier", "owner", "state")
	for _, a := range m.apps {
		fmt.Fprintf(b, "%-10s %-4d %-22s %s\n", a.ID, a.Tier, a.Owner, a.Status)
	}
}

// swatches shows ANSI 0-15, the 14 accents and text on the background.
func swatches(p color.Palette) string {
	block := func(r color.Role) string {
		return lipgloss.NewStyle().Background(lipgloss.Color(p[r].Hex())).Render("   ")
	}
	var b strings.Builder
	for row := 0; row < 2; row++ {
		for _, r := range color.ANSIRoles[row*8 : row*8+8] {
			b.WriteString(block(r))
		}
		b.WriteString("\n")
	}
	for _, r := range color.CatppuccinRoles[:14] {
		b.WriteString(block(r))
	}
	b.WriteString("\n")
	sample := lipgloss.NewStyle().Background(lipgloss.Color(p[color.Background].Hex()))
	line := sample.Foreground(lipgloss.Color(p[color.Foreground].Hex())).Render(" text ")
	for _, r := range []color.Role{color.Red, color.Green, color.Yellow, color.Blue, color.Mauve, color.Teal} {
		line += sample.Foreground(lipgloss.Color(p[r].Hex())).Render(" " + string(r) + " ")
	}
	b.WriteString(line + "\n")
	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
