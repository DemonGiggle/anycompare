package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/DemonGiggle/anycompare/internal/compare"
)

// FilterKind selects which entries are visible.
type FilterKind int

const (
	// FilterChanged shows only non-identical rows (different,
	// only-left, only-right, errors). It is the default so the
	// browser opens focused on what needs attention.
	FilterChanged FilterKind = iota
	FilterAll
	FilterDifferent
	FilterOnlyLeft
	FilterOnlyRight
)

func (f FilterKind) String() string {
	switch f {
	case FilterChanged:
		return "differences"
	case FilterAll:
		return "all"
	case FilterDifferent:
		return "different"
	case FilterOnlyLeft:
		return "only-left"
	case FilterOnlyRight:
		return "only-right"
	default:
		return "all"
	}
}

// SortMode selects browser ordering.
type SortMode int

const (
	SortName SortMode = iota
	SortSize
	SortMtime
)

func (s SortMode) String() string {
	switch s {
	case SortName:
		return "name"
	case SortSize:
		return "size"
	case SortMtime:
		return "mtime"
	default:
		return "name"
	}
}

// Browser is the dual-pane folder comparison list. Both trees share
// one synchronized row per relative path (Meld / Beyond Compare
// folder-compare style); the centre gutter carries the status.
type Browser struct {
	LeftRoot  string
	RightRoot string
	All       []compare.Entry
	Visible   []compare.Entry
	Cursor    int
	Offset    int
	Filter    FilterKind
	Sort      SortMode
	StatusMsg string
	ShowHelp  bool
	Width     int
	Height    int
	FocusLeft bool // cosmetic pane focus toggled with Tab
}

// NewBrowser builds an empty browser for the two roots.
// It starts filtered to differences (non-identical rows).
func NewBrowser(leftRoot, rightRoot string) *Browser {
	return &Browser{LeftRoot: leftRoot, RightRoot: rightRoot, Width: 80, Height: 24, Filter: FilterChanged, FocusLeft: true}
}

// SetEntries replaces the full entry set and reapplies filter/sort.
func (b *Browser) SetEntries(entries []compare.Entry) {
	b.All = entries
	b.Apply()
}

// Apply reapplies filter+sort and clamps cursor/offset.
func (b *Browser) Apply() {
	var vis []compare.Entry
	for _, e := range b.All {
		switch b.Filter {
		case FilterChanged:
			if e.State != compare.StateIdentical {
				vis = append(vis, e)
			}
		case FilterAll:
			vis = append(vis, e)
		case FilterDifferent:
			if e.State == compare.StateDifferent || e.State == compare.StateError {
				vis = append(vis, e)
			}
		case FilterOnlyLeft:
			if e.State == compare.StateOnlyLeft {
				vis = append(vis, e)
			}
		case FilterOnlyRight:
			if e.State == compare.StateOnlyRight {
				vis = append(vis, e)
			}
		}
	}
	switch b.Sort {
	case SortName:
		sort.Slice(vis, func(i, j int) bool { return vis[i].Rel < vis[j].Rel })
	case SortSize:
		sort.Slice(vis, func(i, j int) bool {
			si := entrySize(vis[i])
			sj := entrySize(vis[j])
			if si == sj {
				return vis[i].Rel < vis[j].Rel
			}
			return si > sj
		})
	case SortMtime:
		sort.Slice(vis, func(i, j int) bool {
			ti := entryMtime(vis[i])
			tj := entryMtime(vis[j])
			if ti.Equal(tj) {
				return vis[i].Rel < vis[j].Rel
			}
			return ti.After(tj)
		})
	}
	b.Visible = vis
	if b.Cursor >= len(vis) {
		b.Cursor = len(vis) - 1
	}
	if b.Cursor < 0 {
		b.Cursor = 0
	}
	b.ensureVisible()
}

func entrySize(e compare.Entry) int64 {
	var s int64
	if e.Left != nil && !e.Left.IsDir && e.Left.Size > s {
		s = e.Left.Size
	}
	if e.Right != nil && !e.Right.IsDir && e.Right.Size > s {
		s = e.Right.Size
	}
	return s
}

func entryMtime(e compare.Entry) time.Time {
	var t time.Time
	if e.Left != nil && e.Left.ModTime.After(t) {
		t = e.Left.ModTime
	}
	if e.Right != nil && e.Right.ModTime.After(t) {
		t = e.Right.ModTime
	}
	return t
}

// SetSize updates viewport dimensions.
func (b *Browser) SetSize(w, h int) {
	if w > 0 {
		b.Width = w
	}
	if h > 0 {
		b.Height = h
	}
	b.ensureVisible()
}

// MoveCursor moves the selection, keeping it on screen.
func (b *Browser) MoveCursor(delta int) {
	if len(b.Visible) == 0 {
		return
	}
	b.Cursor += delta
	if b.Cursor < 0 {
		b.Cursor = 0
	}
	if b.Cursor >= len(b.Visible) {
		b.Cursor = len(b.Visible) - 1
	}
	b.ensureVisible()
}

// Page moves by one viewport height.
func (b *Browser) Page(delta int) {
	b.MoveCursor(delta * b.viewRows())
}

// CursorEntry returns the selected entry or nil.
func (b *Browser) CursorEntry() *compare.Entry {
	if len(b.Visible) == 0 || b.Cursor < 0 || b.Cursor >= len(b.Visible) {
		return nil
	}
	return &b.Visible[b.Cursor]
}

// FilterNext cycles the filter and returns its name.
// Order: differences (default) -> all -> different -> only-left -> only-right.
func (b *Browser) FilterNext() string {
	b.Filter = (b.Filter + 1) % 5
	b.Cursor = 0
	b.Offset = 0
	b.Apply()
	return b.Filter.String()
}

// SortNext cycles the sort mode.
func (b *Browser) SortNext() string {
	b.Sort = (b.Sort + 1) % 3
	b.Apply()
	return b.Sort.String()
}

func (b *Browser) viewRows() int {
	h := b.Height - 5 // header(3) + footer(2)
	if h < 1 {
		h = 1
	}
	return h
}

func (b *Browser) ensureVisible() {
	rows := b.viewRows()
	if b.Cursor < b.Offset {
		b.Offset = b.Cursor
	}
	if b.Cursor >= b.Offset+rows {
		b.Offset = b.Cursor - rows + 1
	}
	if b.Offset < 0 {
		b.Offset = 0
	}
}

// UpdateEntry replaces one entry by Rel path (e.g. after hash verify).
func (b *Browser) UpdateEntry(updated compare.Entry) {
	for i := range b.All {
		if b.All[i].Rel == updated.Rel {
			b.All[i] = updated
			break
		}
	}
	sel := ""
	if cur := b.CursorEntry(); cur != nil {
		sel = cur.Rel
	}
	b.Apply()
	for i := range b.Visible {
		if b.Visible[i].Rel == sel {
			b.Cursor = i
			break
		}
	}
	b.ensureVisible()
}

// Counts tallies states over the full set.
func (b *Browser) Counts() (identical, different, onlyLeft, onlyRight int) {
	for _, e := range b.All {
		switch e.State {
		case compare.StateIdentical:
			identical++
		case compare.StateDifferent, compare.StateError:
			different++
		case compare.StateOnlyLeft:
			onlyLeft++
		case compare.StateOnlyRight:
			onlyRight++
		}
	}
	return
}

func stripANSI(s string) string { return ansi.Strip(s) }

// Render draws the browser for the current size.
func (b *Browser) Render() string {
	w := b.Width
	if w < 24 || b.Height < 8 {
		return smallTerminal(w, b.Height)
	}
	leftW := (w - 3) / 2
	rightW := w - 3 - leftW
	var sb strings.Builder
	leftTitle := "SOURCE"
	rightTitle := "DEST"
	if b.FocusLeft {
		leftTitle = "▶ SOURCE"
	} else {
		rightTitle = "DEST ◀"
	}
	sb.WriteString(styleHeader.Render(fitCell(leftTitle, leftW)+" │ "+fitCell(rightTitle, rightW)) + "\n")
	sb.WriteString(styleSubHeader.Render(fitCell(displayText(b.LeftRoot), leftW)+" │ "+fitCell(displayText(b.RightRoot), rightW)) + "\n")
	sb.WriteString(styleSubHeader.Render(browserColumns(leftW)+" │ "+browserColumns(rightW)) + "\n")

	rows := b.viewRows()
	if b.ShowHelp {
		help := strings.Split(b.renderHelp(), "\n")
		for i := 0; i < rows; i++ {
			if i < len(help) {
				sb.WriteString(truncateRunes(help[i], w))
			}
			sb.WriteByte('\n')
		}
	} else {
		for row := 0; row < rows; row++ {
			i := b.Offset + row
			line := strings.Repeat(" ", leftW) + " │ " + strings.Repeat(" ", rightW)
			if i < len(b.Visible) {
				line = b.renderRow(b.Visible[i], leftW, rightW, i == b.Cursor)
				if i == b.Cursor {
					line = styleSelected.Render(line)
				}
			} else if row == 0 {
				line = fitCell("No matching entries — f changes filter", w)
			}
			sb.WriteString(line + "\n")
		}
	}
	identical, different, onlyLeft, onlyRight := b.Counts()
	status := fmt.Sprintf("= %d  ≠ %d  < %d  > %d   | filter:%s sort:%s | %d/%d",
		identical, different, onlyLeft, onlyRight,
		b.Filter.String(), b.Sort.String(), len(b.Visible), len(b.All))
	if b.StatusMsg != "" {
		status = b.StatusMsg + "  |  " + status
	}
	sb.WriteString(styleFooter.Render(truncateRunes(displayText(status), w)) + "\n")
	hints := "j/k navigate · enter diff · f filter · s sort · H hash-verify · R rescan · ? help · q quit"
	sb.WriteString(styleFooter.Render(truncateRunes(hints, w)))
	return sb.String()
}

// Each side owns its filename and metadata; a missing side stays blank.
func browserColumns(width int) string {
	if width < 24 {
		return fitCell("Name", width)
	}
	return fitCell("Name", width-9) + " " + fitCell("Size", 8)
}

func browserCell(rel string, meta *compare.FileMeta, width int) string {
	if meta == nil {
		return strings.Repeat(" ", width)
	}
	if meta.IsDir {
		rel += "/"
	}
	if width < 24 {
		return fitCell(displayText(rel), width)
	}
	return fitCell(displayText(rel), width-9) + " " + fitCell(formatSide(meta), 8)
}

func (b *Browser) renderRow(e compare.Entry, leftW, rightW int, selected bool) string {
	left := browserCell(e.Rel, e.Left, leftW)
	right := browserCell(e.Rel, e.Right, rightW)

	var sym string
	switch e.State {
	case compare.StateIdentical:
		sym = styleIdent.Render("=")
	case compare.StateDifferent, compare.StateError:
		sym = styleDiffSym.Render("≠")
	case compare.StateOnlyLeft:
		sym = styleOnlyLeft.Render("<")
	case compare.StateOnlyRight:
		sym = styleOnlyRight.Render(">")
	default:
		sym = "?"
	}
	cursor := " "
	if selected {
		cursor = "›"
	}
	return left + cursor + sym + " " + right
}

func (b *Browser) renderHelp() string {
	lines := []string{
		"Keys:",
		"  j/k, ↑/↓  move selection      enter  open vimdiff view",
		"  PgUp/PgDn  page               f      cycle filter",
		"  s          cycle sort         H      hash-verify selection",
		"  R          rescan disks       Tab    switch pane focus",
		"  ?          toggle this help   q      quit",
		"Comparison: fast path is size+mtime, then contents auto-verify; H forces SHA-256 now.",
	}
	return styleSubHeader.Render(strings.Join(lines, "\n"))
}

func formatSide(m *compare.FileMeta) string {
	if m == nil {
		return "—"
	}
	if m.IsDir {
		return "<dir>"
	}
	if m.IsSymlink {
		return "<link>"
	}
	return formatSize(m.Size)
}

func formatSize(n int64) string {
	switch {
	case n < 0:
		return "?"
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1fG", float64(n)/(1024*1024*1024))
	}
}
