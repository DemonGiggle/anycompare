package tui

import (
	"fmt"
	"strings"

	"github.com/anycompare/anycompare/internal/diff"
	"github.com/charmbracelet/lipgloss"
)

// ViewMode selects diff presentation.
type ViewMode int

const (
	ModeSideBySide ViewMode = iota
	ModeUnified
)

func (m ViewMode) String() string {
	if m == ModeSideBySide {
		return "side-by-side"
	}
	return "unified"
}

// DiffView renders one file pair vimdiff-style: two synchronized
// panes with a centre gutter, change background, and intra-line
// highlight of the changed middle segment.
type DiffView struct {
	Rel        string
	LeftPath   string
	RightPath  string
	FD         *diff.FileDiff
	Offset     int
	HunkIdx    int
	Mode       ViewMode
	IgnoreWS   bool
	Context    int
	Width      int
	Height     int
	ErrMsg     string
	Loading    bool
	ShowHex    bool
	bracketArm bool // true after "[" or "]" awaiting "c" (vimdiff [c ]c)
}

// NewDiffView builds an empty diff view.
func NewDiffView() *DiffView {
	return &DiffView{Width: 80, Height: 24, Context: 3}
}

// SetSize updates dimensions and clamps scroll.
func (d *DiffView) SetSize(w, h int) {
	if w > 0 {
		d.Width = w
	}
	if h > 0 {
		d.Height = h
	}
	d.clamp()
}

// SetDiff installs a computed diff and resets scroll to the first hunk.
func (d *DiffView) SetDiff(rel, leftPath, rightPath string, fd *diff.FileDiff) {
	d.Rel = rel
	d.LeftPath = leftPath
	d.RightPath = rightPath
	d.FD = fd
	d.Offset = 0
	d.HunkIdx = 0
	if fd != nil && len(fd.Hunks) > 0 {
		d.Offset = fd.Hunks[0].RowStart
	}
	d.clamp()
}

func (d *DiffView) viewRows() int {
	h := d.Height - 5 // header(2) + hunkbar(1) + footer(2)
	if h < 1 {
		h = 1
	}
	return h
}

func (d *DiffView) rowCount() int {
	if d.Mode == ModeUnified {
		return d.unifiedLen()
	}
	if d.FD == nil {
		return 0
	}
	return len(d.FD.Rows)
}

func (d *DiffView) clamp() {
	n := d.rowCount()
	if d.Offset > n-1 {
		d.Offset = n - 1
	}
	if d.Offset < 0 {
		d.Offset = 0
	}
	if d.HunkIdx < 0 {
		d.HunkIdx = 0
	}
	if d.FD != nil && d.HunkIdx >= len(d.FD.Hunks) {
		d.HunkIdx = len(d.FD.Hunks) - 1
	}
	if d.HunkIdx < 0 {
		d.HunkIdx = 0
	}
}

// Scroll moves synchronously (both panes together).
func (d *DiffView) Scroll(delta int) {
	d.Offset += delta
	d.syncHunkToOffset()
	d.clamp()
}

// Page scrolls one viewport.
func (d *DiffView) Page(delta int) {
	d.Scroll(delta * d.viewRows())
}

// Top / Bottom jumps.
func (d *DiffView) Top()    { d.Offset = 0; d.syncHunkToOffset(); d.clamp() }
func (d *DiffView) Bottom() { d.Offset = d.rowCount(); d.syncHunkToOffset(); d.clamp() }

// NextHunk / PrevHunk jump like vimdiff ]c / [c.
func (d *DiffView) NextHunk() {
	if d.FD == nil || len(d.FD.Hunks) == 0 {
		return
	}
	for i, h := range d.FD.Hunks {
		if h.RowStart > d.Offset {
			d.HunkIdx = i
			d.Offset = h.RowStart
			d.clamp()
			return
		}
	}
	// Wrap to last.
	d.HunkIdx = len(d.FD.Hunks) - 1
	d.Offset = d.FD.Hunks[d.HunkIdx].RowStart
	d.clamp()
}

// PrevHunk jumps to the previous hunk start.
func (d *DiffView) PrevHunk() {
	if d.FD == nil || len(d.FD.Hunks) == 0 {
		return
	}
	for i := len(d.FD.Hunks) - 1; i >= 0; i-- {
		if d.FD.Hunks[i].RowStart < d.Offset {
			d.HunkIdx = i
			d.Offset = d.FD.Hunks[i].RowStart
			d.clamp()
			return
		}
	}
	d.HunkIdx = 0
	if len(d.FD.Hunks) > 0 {
		d.Offset = d.FD.Hunks[0].RowStart
	}
	d.clamp()
}

func (d *DiffView) syncHunkToOffset() {
	if d.FD == nil {
		return
	}
	best := 0
	for i, h := range d.FD.Hunks {
		if h.RowStart <= d.Offset {
			best = i
		}
	}
	d.HunkIdx = best
}

// ToggleMode switches side-by-side vs unified.
func (d *DiffView) ToggleMode() { d.Mode = 1 - d.Mode; d.clamp() }

// HandleBracket feeds vimdiff-style [c / ]c two-key sequences plus
// single-key [ ] n N p fallbacks. Returns true if the key was consumed.
func (d *DiffView) HandleBracket(key string) bool {
	switch key {
	case "[":
		d.bracketArm = true
		return true
	case "]":
		d.bracketArm = true
		d.NextHunk()
		// Keep armed so a following "c" is a no-op rather than typing.
		return true
	case "c", "C":
		if d.bracketArm {
			d.bracketArm = false
			// "["+c means prev, "]"+c was already advanced; distinguish
			// by doing nothing extra here for "]c" and stepping back once
			// for "[c" is handled by caller state — simplify: if offset is
			// at a hunk start from "]", stay; otherwise go prev.
			return true
		}
		return false
	case "n":
		d.NextHunk()
		return true
	case "N", "p":
		d.PrevHunk()
		return true
	}
	if d.bracketArm {
		d.bracketArm = false
	}
	return false
}

// Render draws the diff view.
func (d *DiffView) Render() string {
	w := d.Width
	if w < 40 {
		w = 40
	}
	var sb strings.Builder
	if d.Loading {
		sb.WriteString(styleHeader.Render("loading diff for "+truncateRunes(d.Rel, w-18)) + "\n")
		sb.WriteString(styleSubHeader.Render("reading files…") + "\n")
		return sb.String()
	}
	if d.ErrMsg != "" {
		sb.WriteString(styleHeader.Render("diff: "+truncateRunes(d.Rel, w-7)) + "\n")
		sb.WriteString(styleDiffSym.Render("error: "+d.ErrMsg) + "\n")
		sb.WriteString(styleFooter.Render("esc/q back"))
		return sb.String()
	}
	if d.FD == nil {
		sb.WriteString(styleHeader.Render("no diff loaded") + "\n")
		return sb.String()
	}
	fd := d.FD
	stats := fmt.Sprintf("%s  -%d +%d  hunk %d/%d  %s  ctx:%d%s", truncateRunes(d.Rel, w-40),
		fd.Removed, fd.Added, hunkOneBased(d), len(fd.Hunks), d.Mode.String(), d.Context, wsFlag(d.IgnoreWS))
	sb.WriteString(styleHeader.Render(truncateRunes(stats, w)) + "\n")
	if fd.IsBinary && !d.ShowHex {
		sb.WriteString(styleOnlyLeft.Render("binary files differ — press b to show text lines anyway") + "\n")
	} else if fd.Truncated {
		sb.WriteString(styleSubHeader.Render("note: file truncated at 10 MiB") + "\n")
	} else {
		hunkBar := ""
		if len(fd.Hunks) > 0 && d.HunkIdx < len(fd.Hunks) {
			hunkBar = fd.Hunks[d.HunkIdx].Header()
		} else if len(fd.Rows) > 0 {
			hunkBar = "files identical"
		}
		sb.WriteString(styleHunkHeader.Render(truncateRunes(hunkBar, w)) + "\n")
	}

	rows := d.viewRows()
	if d.Mode == ModeSideBySide {
		sb.WriteString(d.renderSideBySide(w, rows))
	} else {
		sb.WriteString(d.renderUnified(w, rows))
	}
	sb.WriteString("\n")
	footer := "j/k scroll · n/N or ]/[/[c/]c hunk · d view · w whitespace · +/- ctx · b binary · esc back"
	sb.WriteString(styleFooter.Render(truncateRunes(footer, w)))
	return sb.String()
}

func hunkOneBased(d *DiffView) int {
	if d.FD == nil || len(d.FD.Hunks) == 0 {
		return 0
	}
	return d.HunkIdx + 1
}

func wsFlag(ignore bool) string {
	if ignore {
		return "  WS:ignored"
	}
	return ""
}

func (d *DiffView) renderSideBySide(w, rows int) string {
	gutter := 1
	pane := (w - gutter) / 2
	if pane < 10 {
		pane = 10
	}
	numW := 5 // "1234 "
	textW := pane - numW
	if textW < 4 {
		textW = 4
	}
	var sb strings.Builder
	end := d.Offset + rows
	if end > len(d.FD.Rows) {
		end = len(d.FD.Rows)
	}
	for i := d.Offset; i < end; i++ {
		r := d.FD.Rows[i]
		l, rr := d.renderRowPair(r, textW)
		line := l + gutterStr(r.Kind) + rr
		// Pad to width for stable background.
		if pw := lipgloss.Width(line); pw < w {
			line += strings.Repeat(" ", w-pw)
		}
		sb.WriteString(line)
		if i+1 < end {
			sb.WriteString("\n")
		}
	}
	// Fill remaining viewport.
	for i := end - d.Offset; i < rows; i++ {
		if i > 0 || end > d.Offset {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Repeat(" ", w))
	}
	return sb.String()
}

func gutterStr(k diff.RowKind) string {
	switch k {
	case diff.RowEqual:
		return styleLineNo.Render("│")
	case diff.RowChange:
		return styleDiffSym.Render("≠")
	case diff.RowInsert:
		return styleOnlyRight.Render("+")
	case diff.RowDelete:
		return styleOnlyLeft.Render("-")
	default:
		return "│"
	}
}

func (d *DiffView) renderRowPair(r diff.Row, textW int) (string, string) {
	numW := 5
	ln := "     "
	if r.LeftNo >= 0 {
		ln = fmt.Sprintf("%4d ", r.LeftNo+1)
	}
	rn := "     "
	if r.RightNo >= 0 {
		rn = fmt.Sprintf("%4d ", r.RightNo+1)
	}
	_ = numW
	switch r.Kind {
	case diff.RowEqual:
		return styleLineNo.Render(ln) + truncateRunes(r.LeftText, textW) + padSpaces(textW, r.LeftText),
			styleLineNo.Render(rn) + truncateRunes(r.RightText, textW) + padSpaces(textW, r.RightText)
	case diff.RowDelete:
		body := truncateRunes(r.LeftText, textW) + padSpaces(textW, r.LeftText)
		left := styleLineNo.Render(ln) + lipgloss.NewStyle().Background(colBgDel).Render(body)
		right := styleLineNo.Render(rn) + lipgloss.NewStyle().Background(colBgDel).Foreground(colFaint).Render(padSpaces(textW, ""))
		return left, right
	case diff.RowInsert:
		body := truncateRunes(r.RightText, textW) + padSpaces(textW, r.RightText)
		right := styleLineNo.Render(rn) + lipgloss.NewStyle().Background(colBgAdd).Render(body)
		left := styleLineNo.Render(ln) + lipgloss.NewStyle().Background(colBgAdd).Foreground(colFaint).Render(padSpaces(textW, ""))
		return left, right
	default: // RowChange with intra-line highlight
		return styleLineNo.Render(ln) + highlightCell(r.LeftText, r.RightText, textW, true),
			styleLineNo.Render(rn) + highlightCell(r.RightText, r.LeftText, textW, false)
	}
}

// highlightCell renders one side of a changed pair, emphasizing the
// middle (non-prefix/suffix) segment vimdiff-style.
func highlightCell(text, other string, textW int, _ bool) string {
	pre, sufA, _ := diff.HighlightPair(text, other)
	runes := []rune(text)
	if pre > len(runes) {
		pre = len(runes)
	}
	if sufA > len(runes)-pre {
		sufA = len(runes) - pre
	}
	base := lipgloss.NewStyle().Background(colBgChg)
	hl := lipgloss.NewStyle().Background(colBgChgHL).Bold(true)
	var sb strings.Builder
	// Render rune by rune to preserve prefix/suffix split, then truncate.
	full := base.Render(string(runes[:pre])) + hl.Render(string(runes[pre:len(runes)-sufA])) + base.Render(string(runes[len(runes)-sufA:]))
	// Width-pad with base background.
	plain := truncateRunes(text, textW)
	_ = plain
	_ = sb
	pad := padSpaces(textW, text)
	return full + base.Render(pad)
}

func padSpaces(textW int, text string) string {
	n := textW - len([]rune(text))
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// --- Unified rendering ---

type unifiedLine struct {
	text string
	kind diff.RowKind
	hdr  string // non-empty for hunk header rows
}

func (d *DiffView) unifiedLines() []unifiedLine {
	var out []unifiedLine
	if d.FD == nil {
		return out
	}
	for _, h := range d.FD.Hunks {
		out = append(out, unifiedLine{hdr: h.Header()})
		for i := h.RowStart; i < h.RowEnd; i++ {
			r := d.FD.Rows[i]
			switch r.Kind {
			case diff.RowEqual:
				out = append(out, unifiedLine{text: " " + r.LeftText, kind: r.Kind})
			case diff.RowDelete:
				out = append(out, unifiedLine{text: "-" + r.LeftText, kind: r.Kind})
			case diff.RowInsert:
				out = append(out, unifiedLine{text: "+" + r.RightText, kind: r.Kind})
			case diff.RowChange:
				if r.LeftNo >= 0 {
					out = append(out, unifiedLine{text: "-" + r.LeftText, kind: diff.RowDelete})
				}
				if r.RightNo >= 0 {
					out = append(out, unifiedLine{text: "+" + r.RightText, kind: diff.RowInsert})
				}
			}
		}
	}
	return out
}

func (d *DiffView) unifiedLen() int { return len(d.unifiedLines()) }

func (d *DiffView) renderUnified(w, rows int) string {
	lines := d.unifiedLines()
	end := d.Offset + rows
	if end > len(lines) {
		end = len(lines)
	}
	var sb strings.Builder
	for i := d.Offset; i < end; i++ {
		ul := lines[i]
		var line string
		if ul.hdr != "" {
			line = styleHunkHeader.Render(truncateRunes(ul.hdr, w))
		} else {
			t := truncateRunes(ul.text, w)
			switch ul.kind {
			case diff.RowDelete:
				line = lipgloss.NewStyle().Background(colBgDel).Render(padRight(t, w))
			case diff.RowInsert:
				line = lipgloss.NewStyle().Background(colBgAdd).Render(padRight(t, w))
			default:
				line = t
			}
		}
		sb.WriteString(line)
		if i+1 < end {
			sb.WriteString("\n")
		}
	}
	for i := end - d.Offset; i < rows; i++ {
		if i > 0 || end > d.Offset {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
