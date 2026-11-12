package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"git.subcult.tv/subculture-collective/pult/internal/color"
)

// The interface draws with the terminal's own 16 colours, so it follows
// whatever theme Pult last applied.
var (
	boldStyle     = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	onStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	focusBorder   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	plainBorder   = lipgloss.NewStyle().Faint(true)
	activeTab     = lipgloss.NewStyle().Reverse(true).Bold(true)
	selectedFocus = lipgloss.NewStyle().Reverse(true)
	selectedBlur  = lipgloss.NewStyle().Bold(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
)

type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }
func (r rect) inner() rect       { return rect{r.x + 1, r.y + 1, r.w - 2, r.h - 2} }

// layout is where everything sits on screen. View draws from it and the
// mouse handlers hit-test against it, so the two never disagree.
type layout struct {
	w, h     int
	tabs     []rect
	search   rect
	tags     rect
	tagCells []rect
	list     rect
	preview  rect
	apps     rect
	detail   rect
}

const brand = " Pult  "

func (m Model) layout() layout {
	w, h := max(m.width, 40), max(m.height, 12)
	l := layout{w: w, h: h}
	x := ansi.StringWidth(brand)
	for _, name := range tabNames {
		lw := len(name) + 2
		l.tabs = append(l.tabs, rect{x, 0, lw, 1})
		x += lw + 1
	}
	bodyY, bodyH := 1, h-2
	leftW := max(w/2, 30)
	cells, rows := tagCells(leftW - 2)
	l.search = rect{0, bodyY, leftW, 3}
	l.tags = rect{0, bodyY + 3, leftW, rows + 2}
	for _, c := range cells {
		l.tagCells = append(l.tagCells, rect{c.x + 1, c.y + l.tags.y + 1, c.w, 1})
	}
	l.list = rect{0, l.tags.y + l.tags.h, leftW, max(3, bodyH-3-l.tags.h)}
	l.preview = rect{leftW, bodyY, w - leftW, bodyH}
	l.apps = rect{0, bodyY, leftW, bodyH}
	l.detail = l.preview
	return l
}

func tagLabel(i int) string { return fmt.Sprintf("%d %s", i+1, Tags[i]) }

// tagCells places the tag labels in rows of width w, relative to the
// pane's inside, and returns how many rows they take.
func tagCells(w int) ([]rect, int) {
	var out []rect
	x, y := 0, 0
	for i := range Tags {
		lw := len(tagLabel(i))
		if x > 0 && x+lw > w {
			x, y = 0, y+1
		}
		out = append(out, rect{x, y, lw, 1})
		x += lw + 2
	}
	return out, y + 1
}

// pad truncates or pads s to exactly w cells.
func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// box draws a bordered pane with a title in its top edge and an optional
// note in its bottom edge.
func box(r rect, title, note string, body []string, focused bool) []string {
	bs := plainBorder
	ts := boldStyle
	if focused {
		bs, ts = focusBorder, accentStyle
	}
	iw := r.w - 2
	if title != "" {
		title = " " + ansi.Truncate(title, max(0, iw-4), "…") + " "
	}
	top := bs.Render("╭─") + ts.Render(title) + bs.Render(strings.Repeat("─", max(0, iw-1-ansi.StringWidth(title)))+"╮")
	note = ansi.Truncate(note, max(0, iw-2), "…")
	nw := ansi.StringWidth(note)
	bottom := bs.Render("╰" + strings.Repeat("─", max(0, iw-1-nw)))
	if nw > 0 {
		bottom += dimStyle.Render(note)
	} else if iw > 0 {
		bottom += bs.Render("─")
	}
	bottom += bs.Render("╯")
	out := []string{top}
	side := bs.Render("│")
	for i := 0; i < r.h-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		out = append(out, side+pad(line, iw)+side)
	}
	return append(out, bottom)
}

// View implements tea.Model.
func (m Model) View() tea.View {
	l := m.layout()
	screen := make([]string, l.h)
	screen[0] = m.tabBar(l)
	var left, right []string
	if m.tab == appsTab {
		left = m.appsPane(l)
		right = m.detailPane(l)
	} else {
		left = append(m.searchPane(l), m.tagsPane(l)...)
		left = append(left, m.listPane(l)...)
		right = m.previewPane(l)
	}
	for i := 0; i < l.h-2; i++ {
		var a, b string
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		screen[i+1] = a + b
	}
	screen[l.h-1] = m.footer(l)
	content := strings.Join(screen, "\n")
	if m.dialog != noDialog {
		d := m.dialogBox()
		content = lipgloss.NewCompositor(
			lipgloss.NewLayer(content),
			lipgloss.NewLayer(strings.Join(d.lines, "\n")).X(d.r.x).Y(d.r.y).Z(1),
		).Render()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "Pult"
	return v
}

func (m Model) tabBar(l layout) string {
	s := boldStyle.Render(brand)
	for i, name := range tabNames {
		label := " " + name + " "
		if tab(i) == m.tab {
			s += activeTab.Render(label)
		} else {
			s += dimStyle.Render(label)
		}
		s += " "
	}
	count := fmt.Sprintf("%d listed ", len(m.themes))
	if m.tab == appsTab {
		count = fmt.Sprintf("%d apps ", len(m.apps))
	}
	gap := l.w - ansi.StringWidth(s) - len(count)
	if gap < 1 {
		return s
	}
	return s + strings.Repeat(" ", gap) + dimStyle.Render(count)
}

func (m Model) searchPane(l layout) []string {
	return box(l.search, "Search", "", []string{m.search.View()}, m.focus == searchPane)
}

func (m Model) tagsPane(l layout) []string {
	in := l.tags.inner()
	rows := make([]string, l.tags.h-2)
	at := make([]int, len(rows))
	for i, c := range l.tagCells {
		row := c.y - in.y
		label := tagLabel(i)
		switch {
		case m.focus == tagsPane && i == m.tagCursor:
			label = selectedFocus.Render(label)
		case m.tags[Tags[i]]:
			label = onStyle.Render(label)
		default:
			label = dimStyle.Render(label)
		}
		rows[row] += strings.Repeat(" ", c.x-in.x-at[row]) + label
		at[row] = c.x - in.x + c.w
	}
	var on []string
	for _, t := range Tags {
		if m.tags[t] {
			on = append(on, t)
		}
	}
	note := ""
	if len(on) > 0 {
		note = fmt.Sprintf(" %d on ", len(on))
	}
	return box(l.tags, "Tags", note, rows, m.focus == tagsPane)
}

func (m Model) listPane(l layout) []string {
	in := l.list.inner()
	focused := m.focus == listPane
	var body []string
	idW := (in.w - 2) / 2
	for i := m.list.offset; i < min(len(m.themes), m.list.offset+in.h); i++ {
		th := m.themes[i]
		mark := " "
		if !th.Bundled {
			mark = "↓"
		}
		line := pad(mark+" "+pad(th.ID, idW-1)+" "+th.Name, in.w)
		switch {
		case i == m.list.cursor && focused:
			line = selectedFocus.Render(line)
		case i == m.list.cursor:
			line = selectedBlur.Render(line)
		}
		body = append(body, line)
	}
	if len(m.themes) == 0 {
		body = append(body, dimStyle.Render("  no themes found"))
	}
	title := "Themes"
	if m.similar != "" {
		title = "Closest to " + m.similar + " (esc back)"
	}
	note := ""
	if len(m.themes) > 0 {
		note = fmt.Sprintf(" %d of %d ", m.list.cursor+1, len(m.themes))
	}
	return box(l.list, title, note, body, focused)
}

func (m Model) previewPane(l layout) []string {
	iw := l.preview.w - 2
	th, ok := m.selected()
	if !ok {
		return box(l.preview, "Preview", "", []string{dimStyle.Render(" No theme selected.")}, false)
	}
	body := []string{" " + boldStyle.Render(th.Name), ""}
	field := func(k, v string) {
		if v != "" {
			body = append(body, " "+dimStyle.Render(fmt.Sprintf("%-8s", k))+v)
		}
	}
	field("id", th.ID)
	field("family", strings.Join(nonEmpty(th.Family, th.Variant), " · "))
	field("author", th.Author)
	field("licence", th.License)
	field("source", th.SourceID)
	field("tags", strings.Join(th.Tags, ", "))
	body = append(body, "")
	if p, err := m.b.Palette(th.ID); err == nil {
		for _, s := range swatches(p, iw-2) {
			body = append(body, " "+s)
		}
	} else {
		msg := fmt.Sprintf("%s is not bundled (licence %s); its colors are downloaded when you apply it.", th.Name, th.License)
		body = append(body, wrapLines(msg, iw-2, " ")...)
	}
	return box(l.preview, "Preview", "", body, false)
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func wrapLines(s string, w int, indent string) []string {
	var out []string
	for _, line := range strings.Split(ansi.Wrap(s, max(1, w), ""), "\n") {
		out = append(out, indent+line)
	}
	return out
}

func (m Model) appsPane(l layout) []string {
	in := l.apps.inner()
	var body []string
	for i := m.appList.offset; i < min(len(m.apps), m.appList.offset+in.h); i++ {
		a := m.apps[i]
		line := pad(fmt.Sprintf(" %-12s tier %d  %s", a.ID, a.Tier, a.Owner), in.w)
		if i == m.appList.cursor {
			line = selectedFocus.Render(line)
		}
		body = append(body, line)
	}
	if len(m.apps) == 0 {
		body = append(body, dimStyle.Render("  no supported apps found"))
	}
	note := ""
	if len(m.apps) > 0 {
		note = fmt.Sprintf(" %d of %d ", m.appList.cursor+1, len(m.apps))
	}
	return box(l.apps, "Apps", note, body, true)
}

func (m Model) detailPane(l layout) []string {
	if m.appList.cursor >= len(m.apps) {
		return box(l.detail, "Details", "", nil, false)
	}
	a := m.apps[m.appList.cursor]
	iw := l.detail.w - 2
	body := []string{
		" " + boldStyle.Render(a.ID), "",
		" " + dimStyle.Render(fmt.Sprintf("%-8s", "tier")) + fmt.Sprint(a.Tier),
		" " + dimStyle.Render(fmt.Sprintf("%-8s", "owner")) + a.Owner,
		" " + dimStyle.Render("state"),
	}
	body = append(body, wrapLines(a.Status, iw-4, "   ")...)
	return box(l.detail, "Details", "", body, false)
}

func (m Model) footer(l layout) string {
	var hints string
	switch {
	case m.dialog == confirmApply || m.dialog == confirmUndo:
		hints = "y confirm · n cancel · ↑↓ scroll"
	case m.dialog != noDialog:
		hints = "any key to close"
	case m.tab == appsTab:
		hints = "↑↓ move · [ ] tabs · u undo · ? help · q quit"
	case m.focus == searchPane:
		hints = "type to search · enter/esc done · tab next pane · ctrl+c quit"
	case m.focus == tagsPane:
		hints = "←→ move · space toggle · 1-9 tags · tab next pane · ? help · q quit"
	default:
		hints = "↑↓ move · enter apply · s similar · / search · 1-9 tags · tab pane · ? help · q quit"
	}
	s := " "
	if m.status != "" {
		st := boldStyle
		if strings.HasPrefix(m.status, "error:") {
			st = errorStyle
		}
		s += st.Render(m.status) + "  "
	}
	s += dimStyle.Render(hints)
	return ansi.Truncate(s, l.w, "…")
}

// dialogView is a dialog's drawn lines, its place on screen, and where its
// confirm and cancel buttons are.
type dialogView struct {
	lines []string
	r     rect
	ok    rect
	no    rect
}

// planRows is how many plan lines fit in the apply dialog.
func (m Model) planRows() int {
	return max(1, max(m.height, 12)-12)
}

func (m Model) dialogBox() dialogView {
	l := m.layout()
	maxW := min(l.w-4, 90)
	var title string
	var body []string
	okLabel, noLabel := "[ y Apply ]", "[ n Cancel ]"
	widest := func() int {
		w := 40
		for _, b := range body {
			w = max(w, ansi.StringWidth(b)+2)
		}
		if m.dialog == confirmApply {
			for _, p := range m.plan {
				w = max(w, ansi.StringWidth(p)+3)
			}
		}
		return min(w, maxW-2)
	}
	switch m.dialog {
	case confirmApply:
		th, _ := m.selected()
		title = "Apply " + th.Name + "?"
		end := min(len(m.plan), m.planOff+m.planRows())
		for _, p := range m.plan[m.planOff:end] {
			body = append(body, " "+p)
		}
		if len(m.plan) > m.planRows() {
			body = append(body, dimStyle.Render(fmt.Sprintf(" lines %d-%d of %d, ↑↓ to scroll", m.planOff+1, end, len(m.plan))))
		}
		body = append(body, "")
		body = append(body, wrapLines("Files other tools own are left alone here; use pult apply to confirm them.", widest()-2, " ")...)
	case confirmUndo:
		title = "Undo"
		okLabel = "[ y Undo ]"
		body = []string{" Roll back the last apply?"}
	case messageDialog:
		title = "Message"
		if strings.HasPrefix(m.message, "error:") {
			title = "Error"
		}
		okLabel, noLabel = "[ OK ]", ""
		body = wrapLines(m.message, maxW-4, " ")
	case helpDialog:
		title = "Keys"
		okLabel, noLabel = "[ OK ]", ""
		body = helpLines()
	}
	iw := widest()
	if room := l.h - 4; len(body) > room {
		body = append(body[:max(0, room-1)], dimStyle.Render(" …"))
	}
	body = append(body, "")
	btnRow := len(body)
	buttons := okLabel
	if noLabel != "" {
		buttons += "  " + noLabel
	}
	bx := max(0, (iw-ansi.StringWidth(buttons))/2)
	body = append(body, strings.Repeat(" ", bx)+accentStyle.Render(okLabel))
	if noLabel != "" {
		body[btnRow] += "  " + boldStyle.Render(noLabel)
	}
	r := rect{w: iw + 2, h: len(body) + 2}
	r.x, r.y = max(0, (l.w-r.w)/2), max(0, (l.h-r.h)/2)
	d := dialogView{lines: box(r, title, "", body, true), r: r}
	d.ok = rect{r.x + 1 + bx, r.y + 1 + btnRow, len(okLabel), 1}
	if noLabel != "" {
		d.no = rect{d.ok.x + d.ok.w + 2, d.ok.y, len(noLabel), 1}
	}
	return d
}

func helpLines() []string {
	rows := [][2]string{
		{"[ ]", "switch tab"},
		{"tab", "next pane (search, tags, themes)"},
		{"/", "search by name, family or author"},
		{"1-9", "toggle a tag"},
		{"↑↓ j k", "move"},
		{"pgup pgdn", "page"},
		{"g G", "first, last"},
		{"enter", "show the plan and ask to apply"},
		{"s", "themes closest to the selected one"},
		{"esc", "back from the closest list"},
		{"u", "roll back the last apply"},
		{"q", "quit"},
		{"", ""},
		{"click", "a tab, pane, tag or row"},
		{"click again", "on the selected theme to apply it"},
		{"wheel", "scroll a list"},
	}
	var out []string
	for _, r := range rows {
		out = append(out, " "+accentStyle.Render(fmt.Sprintf("%-12s", r[0]))+r[1])
	}
	return out
}

// swatches shows ANSI 0-15, the 14 accents and text on the background,
// with blocks sized to fit width w.
func swatches(p color.Palette, w int) []string {
	cw := max(1, min(3, w/14))
	cell := strings.Repeat(" ", cw)
	block := func(r color.Role) string {
		return lipgloss.NewStyle().Background(lipgloss.Color(p[r].Hex())).Render(cell)
	}
	var out []string
	for row := 0; row < 2; row++ {
		var b strings.Builder
		for _, r := range color.ANSIRoles[row*8 : row*8+8] {
			b.WriteString(block(r))
		}
		out = append(out, b.String())
	}
	var b strings.Builder
	for _, r := range color.CatppuccinRoles[:14] {
		b.WriteString(block(r))
	}
	out = append(out, b.String(), "")
	sample := lipgloss.NewStyle().Background(lipgloss.Color(p[color.Background].Hex()))
	line := sample.Foreground(lipgloss.Color(p[color.Foreground].Hex())).Render(" text ")
	for _, r := range []color.Role{color.Red, color.Green, color.Yellow, color.Blue, color.Mauve, color.Teal} {
		line += sample.Foreground(lipgloss.Color(p[r].Hex())).Render(" " + string(r) + " ")
	}
	return append(out, line)
}
