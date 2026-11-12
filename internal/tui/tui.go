// Package tui is Pult's terminal interface: a fullscreen, mouse-aware
// browser for the catalog with a search box, tag filters, a theme list,
// a swatch preview and an apps tab. From it you can list similar themes,
// apply a theme and undo the last apply.
package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"git.subcult.tv/subculture-collective/pult/internal/catalog"
	"git.subcult.tv/subculture-collective/pult/internal/color"
)

// Backend is what the interface needs from the rest of Pult.
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

type tab int

const (
	themesTab tab = iota
	appsTab
)

var tabNames = []string{"Themes", "Apps"}

// pane is the focused pane on the themes tab.
type pane int

const (
	searchPane pane = iota
	tagsPane
	listPane
)

type dialog int

const (
	noDialog dialog = iota
	confirmApply
	confirmUndo
	messageDialog
	helpDialog
)

// scroller is a list's selected row and the first row it shows.
type scroller struct{ cursor, offset int }

// clamp keeps the cursor inside n items and visible in a window of rows.
func (s *scroller) clamp(n, rows int) {
	rows = max(rows, 1)
	s.cursor = max(0, min(s.cursor, n-1))
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+rows {
		s.offset = s.cursor - rows + 1
	}
	s.offset = max(0, min(s.offset, n-rows))
}

func (s *scroller) move(d, n, rows int) {
	s.cursor += d
	s.clamp(n, rows)
}

// scroll moves the window, as the mouse wheel does, and keeps the cursor
// inside it.
func (s *scroller) scroll(d, n, rows int) {
	rows = max(rows, 1)
	s.offset = max(0, min(s.offset+d, n-rows))
	s.cursor = max(s.offset, min(s.cursor, s.offset+rows-1))
	s.cursor = max(0, min(s.cursor, n-1))
}

// Model is the Bubble Tea model.
type Model struct {
	b         Backend
	tab       tab
	focus     pane
	search    textinput.Model
	tags      map[string]bool
	tagCursor int
	similar   string // theme the list shows neighbours of, or ""
	themes    []catalog.Info
	list      scroller
	apps      []AppRow
	appList   scroller
	dialog    dialog
	plan      []string
	dlgOff    int // first line the open dialog shows
	message   string
	status    string
	width     int
	height    int
}

// New returns the model with the first search already run.
func New(b Backend) Model {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "name, family or author"
	m := Model{b: b, search: in, tags: map[string]bool{}, focus: listPane, width: 100, height: 30}
	m.fit()
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
		m.notify("error: " + err.Error())
		return
	}
	m.themes = res
	m.list.clamp(len(res), m.listRows())
}

// fit resizes the search box and keeps both lists in view after the
// window changes size.
func (m *Model) fit() {
	l := m.layout()
	m.search.SetWidth(max(1, l.search.w-2-ansi.StringWidth(m.search.Prompt)-1))
	m.list.clamp(len(m.themes), m.listRows())
	m.appList.clamp(len(m.apps), m.appRows())
}

func (m Model) listRows() int { return m.layout().list.h - 2 }
func (m Model) appRows() int  { return m.layout().apps.h - 2 }

func (m Model) selected() (catalog.Info, bool) {
	if m.list.cursor >= 0 && m.list.cursor < len(m.themes) {
		return m.themes[m.list.cursor], true
	}
	return catalog.Info{}, false
}

// notify puts a message in the footer, or in a dialog when the footer is
// too narrow for it.
func (m *Model) notify(msg string) {
	if ansi.StringWidth(msg) <= m.width-2 {
		m.status = msg
		return
	}
	m.status, m.dialog, m.message, m.dlgOff = "", messageDialog, msg, 0
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.fit()
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseClickMsg:
		return m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		return m.wheel(msg.Mouse())
	}
	return m, nil
}

func (m *Model) setTab(t tab) {
	m.tab = t
	if t == appsTab {
		m.search.Blur()
		m.loadApps()
	} else if m.focus == searchPane {
		m.search.Focus()
	}
}

func (m *Model) focusPane(p pane) tea.Cmd {
	m.focus = p
	if p == searchPane {
		return m.search.Focus()
	}
	m.search.Blur()
	return nil
}

func (m *Model) cycleFocus(d int) tea.Cmd {
	return m.focusPane(pane((int(m.focus) + d + 3) % 3))
}

func (m *Model) toggleTag(i int) {
	t := Tags[i]
	m.tags[t] = !m.tags[t]
	m.similar, m.list = "", scroller{}
	m.refresh()
}

func (m *Model) openPlan() {
	th, ok := m.selected()
	if !ok {
		return
	}
	plan, err := m.b.Plan(th.ID)
	if err != nil {
		m.notify("error: " + err.Error())
		return
	}
	m.plan, m.dlgOff, m.dialog, m.status = plan, 0, confirmApply, ""
}

func (m *Model) showSimilar() {
	if th, ok := m.selected(); ok {
		m.similar, m.list = th.ID, scroller{}
		m.refresh()
	}
}

// navDelta maps a movement key to a cursor change for a list of rows.
func navDelta(key string, rows int) (int, bool) {
	switch key {
	case "up", "k":
		return -1, true
	case "down", "j":
		return 1, true
	case "pgup", "ctrl+b":
		return -rows, true
	case "pgdown", "ctrl+f":
		return rows, true
	case "home", "g":
		return -1 << 30, true
	case "end", "G":
		return 1 << 30, true
	}
	return 0, false
}

func (m Model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.dialog != noDialog {
		return m.dialogKey(key)
	}
	if m.tab == themesTab && m.focus == searchPane {
		switch key {
		case "esc", "enter":
			return m, m.focusPane(listPane)
		case "tab":
			return m, m.cycleFocus(1)
		case "shift+tab":
			return m, m.cycleFocus(-1)
		}
		before := m.search.Value()
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(k)
		if m.search.Value() != before {
			m.similar, m.list = "", scroller{}
			m.refresh()
		}
		return m, cmd
	}
	switch key {
	case "q":
		return m, tea.Quit
	case "]", "[":
		m.setTab((m.tab + 1) % 2)
		return m, nil
	case "u":
		m.dialog, m.status, m.dlgOff = confirmUndo, "", 0
		return m, nil
	case "?":
		m.dialog, m.dlgOff = helpDialog, 0
		return m, nil
	}
	if m.tab == appsTab {
		if d, ok := navDelta(key, m.appRows()); ok {
			m.appList.move(d, len(m.apps), m.appRows())
		}
		return m, nil
	}
	switch key {
	case "tab":
		return m, m.cycleFocus(1)
	case "shift+tab":
		return m, m.cycleFocus(-1)
	case "/":
		m.status = ""
		return m, m.focusPane(searchPane)
	case "s":
		m.showSimilar()
		return m, nil
	case "esc":
		if m.similar != "" {
			m.similar, m.list = "", scroller{}
			m.refresh()
		}
		return m, nil
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		m.tagCursor = int(key[0] - '1')
		m.toggleTag(m.tagCursor)
		return m, nil
	}
	if m.focus == tagsPane {
		switch key {
		case "left", "h", "up", "k":
			m.tagCursor = max(0, m.tagCursor-1)
		case "right", "l", "down", "j":
			m.tagCursor = min(len(Tags)-1, m.tagCursor+1)
		case "space", "enter", "x":
			m.toggleTag(m.tagCursor)
		}
		return m, nil
	}
	if d, ok := navDelta(key, m.listRows()); ok {
		m.list.move(d, len(m.themes), m.listRows())
	} else if key == "enter" {
		m.openPlan()
	}
	return m, nil
}

func (m Model) dialogKey(key string) (tea.Model, tea.Cmd) {
	if d, ok := navDelta(key, m.dialogRows()); ok {
		m.scrollDialog(d)
		return m, nil
	}
	if m.dialog == messageDialog || m.dialog == helpDialog {
		m.dialog = noDialog
		return m, nil
	}
	if key == "y" {
		m.confirm()
	} else {
		m.cancel()
	}
	return m, nil
}

func (m *Model) cancel() {
	m.dialog, m.plan = noDialog, nil
	m.status = "Nothing changed."
}

// confirm runs the apply or undo the open dialog asked about.
func (m *Model) confirm() {
	c := m.dialog
	m.dialog, m.plan = noDialog, nil
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
	m.notify(msg)
	if m.tab == appsTab {
		m.loadApps()
	}
}

func (m *Model) loadApps() {
	apps, err := m.b.Apps()
	if err != nil {
		m.notify("error: " + err.Error())
		return
	}
	m.apps = apps
	m.appList.clamp(len(apps), m.appRows())
}

func (m Model) click(ms tea.Mouse) (tea.Model, tea.Cmd) {
	if ms.Button != tea.MouseLeft {
		return m, nil
	}
	x, y := ms.X, ms.Y
	if m.dialog != noDialog {
		d := m.dialogBox()
		switch {
		case m.dialog == messageDialog || m.dialog == helpDialog:
			if !d.r.has(x, y) || d.ok.has(x, y) {
				m.dialog = noDialog
			}
		case d.ok.has(x, y):
			m.confirm()
		case d.no.has(x, y), !d.r.has(x, y):
			m.cancel()
		}
		return m, nil
	}
	l := m.layout()
	for i, r := range l.tabs {
		if r.has(x, y) {
			m.setTab(tab(i))
			return m, nil
		}
	}
	if m.tab == appsTab {
		if in := l.apps.inner(); in.has(x, y) {
			if i := m.appList.offset + y - in.y; i < len(m.apps) {
				m.appList.cursor = i
				m.appList.clamp(len(m.apps), m.appRows())
			}
		}
		return m, nil
	}
	switch {
	case l.search.has(x, y):
		return m, m.focusPane(searchPane)
	case l.tags.has(x, y):
		m.focusPane(tagsPane)
		for i, r := range l.tagCells {
			if r.has(x, y) {
				m.tagCursor = i
				m.toggleTag(i)
			}
		}
	case l.list.has(x, y):
		m.focusPane(listPane)
		in := l.list.inner()
		if !in.has(x, y) {
			break
		}
		i := m.list.offset + y - in.y
		switch {
		case i >= len(m.themes):
		case i == m.list.cursor:
			m.openPlan()
		default:
			m.list.cursor = i
			m.list.clamp(len(m.themes), m.listRows())
		}
	}
	return m, nil
}

func (m Model) wheel(ms tea.Mouse) (tea.Model, tea.Cmd) {
	d := 0
	switch ms.Button {
	case tea.MouseWheelUp:
		d = -3
	case tea.MouseWheelDown:
		d = 3
	default:
		return m, nil
	}
	if m.dialog != noDialog {
		m.scrollDialog(d)
		return m, nil
	}
	l := m.layout()
	switch {
	case m.tab == appsTab && l.apps.has(ms.X, ms.Y):
		m.appList.scroll(d, len(m.apps), m.appRows())
	case m.tab == themesTab && l.list.has(ms.X, ms.Y):
		m.list.scroll(d, len(m.themes), m.listRows())
	}
	return m, nil
}
