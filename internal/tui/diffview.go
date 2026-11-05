package tui

import (
	"fmt"
	"strconv"
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
	bracketArm bool // a bracket already navigated; consume a following c
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
	d.bracketArm = false
	if fd != nil && len(fd.Hunks) > 0 {
		d.Offset = d.hunkOffsets()[0]
	}
	d.clamp()
}

func (d *DiffView) viewRows() int {
	h := d.Height - 4 // title + hunk bar + pane labels + footer
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
	d.clamp()
	d.syncHunkToOffset()
}

// Page scrolls one viewport.
func (d *DiffView) Page(delta int) {
	d.Scroll(delta * d.viewRows())
}

// Top / Bottom jumps.
func (d *DiffView) Top() { d.Offset = 0; d.clamp(); d.syncHunkToOffset() }
func (d *DiffView) Bottom() {
	d.Offset = max(0, d.rowCount()-d.viewRows())
	d.clamp()
	d.syncHunkToOffset()
}

// NextHunk / PrevHunk jump like vimdiff ]c / [c.
func (d *DiffView) NextHunk() {
	if d.FD == nil || len(d.FD.Hunks) == 0 {
		return
	}
	for i, offset := range d.hunkOffsets() {
		if offset > d.Offset {
			d.HunkIdx = i
			d.Offset = offset
			d.clamp()
			return
		}
	}
	// Wrap to last.
	d.HunkIdx = len(d.FD.Hunks) - 1
	d.Offset = d.hunkOffsets()[d.HunkIdx]
	d.clamp()
}

// PrevHunk jumps to the previous hunk start.
func (d *DiffView) PrevHunk() {
	if d.FD == nil || len(d.FD.Hunks) == 0 {
		return
	}
	offsets := d.hunkOffsets()
	for i := len(offsets) - 1; i >= 0; i-- {
		if offsets[i] < d.Offset {
			d.HunkIdx = i
			d.Offset = offsets[i]
			d.clamp()
			return
		}
	}
	d.HunkIdx = 0
	if len(d.FD.Hunks) > 0 {
		d.Offset = d.hunkOffsets()[0]
	}
	d.clamp()
}

func (d *DiffView) syncHunkToOffset() {
	if d.FD == nil {
		return
	}
	best := 0
	for i, offset := range d.hunkOffsets() {
		if offset <= d.Offset {
			best = i
		}
	}
	d.HunkIdx = best
}

// hunkOffsets maps hunk starts into the active presentation's row space.
func (d *DiffView) hunkOffsets() []int {
	var offsets []int
	if d.FD == nil {
		return offsets
	}
	if d.Mode == ModeUnified {
		for i, line := range d.unifiedLines() {
			if line.hdr != "" {
				offsets = append(offsets, i)
			}
		}
	} else {
		for _, h := range d.FD.Hunks {
			offsets = append(offsets, h.RowStart)
		}
	}
	return offsets
}

// ToggleMode keeps the current source row visible when changing presentation.
func (d *DiffView) ToggleMode() {
	row := d.Offset
	if d.Mode == ModeUnified {
		lines := d.unifiedLines()
		if row < len(lines) {
			row = lines[row].row
		}
		d.Mode = ModeSideBySide
		d.Offset = row
	} else {
		d.Mode = ModeUnified
		d.Offset = 0
		for i, line := range d.unifiedLines() {
			d.Offset = i
			if line.row >= row {
				break
			}
		}
	}
	d.clamp()
	d.syncHunkToOffset()
}

// HandleBracket supports both single brackets and vim's [c / ]c, with
// exactly one jump per sequence. Any unrelated key disarms the sequence.
func (d *DiffView) HandleBracket(key string) bool {
	armed := d.bracketArm
	d.bracketArm = false
	switch key {
	case "[":
		d.PrevHunk()
		d.bracketArm = true
	case "]":
		d.NextHunk()
		d.bracketArm = true
	case "c", "C":
		return armed
	case "n":
		d.NextHunk()
	case "N", "p":
		d.PrevHunk()
	default:
		return false
	}
	return true
}

// Render draws the diff view.
func (d *DiffView) Render() string {
	w := d.Width
	if w < 24 || d.Height < 8 {
		return smallTerminal(w, d.Height)
	}
	var sb strings.Builder
	if d.Loading {
		sb.WriteString(styleHeader.Render("loading diff for "+truncateRunes(displayText(d.Rel), w-18)) + "\n")
		sb.WriteString(styleSubHeader.Render("reading files…") + "\n")
		return sb.String()
	}
	if d.ErrMsg != "" {
		sb.WriteString(styleHeader.Render("diff: "+truncateRunes(displayText(d.Rel), w-7)) + "\n")
		sb.WriteString(styleDiffSym.Render(truncateRunes("error: "+displayText(d.ErrMsg), w)) + "\n")
		sb.WriteString(styleFooter.Render("esc/q back"))
		return sb.String()
	}
	if d.FD == nil {
		sb.WriteString(styleHeader.Render("no diff loaded") + "\n")
		return sb.String()
	}
	fd := d.FD
	stats := fmt.Sprintf("%s  -%d +%d  hunk %d/%d  %s  ctx:%d%s", truncateRunes(displayText(d.Rel), w-40),
		fd.Removed, fd.Added, hunkOneBased(d), len(fd.Hunks), d.Mode.String(), d.Context, wsFlag(d.IgnoreWS))
	sb.WriteString(styleHeader.Render(truncateRunes(stats, w)) + "\n")
	if fd.IsBinary && !d.ShowHex {
		sb.WriteString(styleOnlyLeft.Render(truncateRunes("binary files — press b to show text lines", w)) + "\n")
	} else if fd.Truncated {
		sb.WriteString(styleSubHeader.Render(truncateRunes("note: file truncated at 10 MiB", w)) + "\n")
	} else {
		hunkBar := ""
		switch {
		case len(fd.Hunks) > 0 && d.HunkIdx < len(fd.Hunks):
			hunkBar = chunkLabel(fd.Hunks, d.HunkIdx)
		case fd.LineEndingOnly:
			// Bytes differ but the line view normalizes CRLF away,
			// so never claim "identical" here.
			hunkBar = "line endings differ (CRLF vs LF)"
		default:
			hunkBar = "files identical"
		}
		sb.WriteString(styleHunkHeader.Render(truncateRunes(hunkBar, w)) + "\n")
	}

	leftW := (w - 1) / 2
	rightW := w - 1 - leftW
	sb.WriteString(styleSubHeader.Render(fitCell("SOURCE "+displayText(d.LeftPath), leftW)+"│"+fitCell("DEST "+displayText(d.RightPath), rightW)) + "\n")
	rows := d.viewRows()
	if fd.IsBinary && !d.ShowHex {
		sb.WriteString(strings.Repeat("\n", rows-1))
	} else if d.Mode == ModeSideBySide {
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

// chunkLabel numbers one hunk for n/N navigation, e.g.
// "[2/5] @@ -12,3 +12,4 @@", so each jump lands on a labeled chunk.
func chunkLabel(hunks []diff.Hunk, idx int) string {
	if idx < 0 || idx >= len(hunks) {
		return ""
	}
	return fmt.Sprintf("[%d/%d]%s", idx+1, len(hunks), hunks[idx].Header())
}

func wsFlag(ignore bool) string {
	if ignore {
		return "  WS:ignored"
	}
	return ""
}

func (d *DiffView) renderSideBySide(w, rows int) string {
	leftW := (w - 1) / 2
	rightW := w - 1 - leftW
	numW := d.numberWidth()
	var sb strings.Builder
	end := d.Offset + rows
	if end > len(d.FD.Rows) {
		end = len(d.FD.Rows)
	}
	for i := d.Offset; i < end; i++ {
		r := d.FD.Rows[i]
		l, rr := d.renderRowPair(r, leftW-numW, rightW-numW, numW)
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

func (d *DiffView) numberWidth() int {
	return max(4, len(strconv.Itoa(max(len(d.FD.LeftLines), len(d.FD.RightLines))))) + 1
}

func (d *DiffView) renderRowPair(r diff.Row, leftW, rightW, numW int) (string, string) {
	lineNo := func(n int) string {
		if n < 0 {
			return strings.Repeat(" ", numW)
		}
		return styleLineNo.Render(fmt.Sprintf("%*d ", numW-1, n+1))
	}
	left, right := displayText(r.LeftText), displayText(r.RightText)
	l, rr := fitCell(left, leftW), fitCell(right, rightW)
	switch r.Kind {
	case diff.RowDelete:
		l = lipgloss.NewStyle().Background(colBgDel).Render(l)
	case diff.RowInsert:
		rr = lipgloss.NewStyle().Background(colBgAdd).Render(rr)
	case diff.RowChange:
		l = highlightCell(left, right, leftW)
		rr = highlightCell(right, left, rightW)
	}
	return lineNo(r.LeftNo) + l, lineNo(r.RightNo) + rr
}

// Clip the styled text, not only the equal/insert/delete cases, so even
// long changed lines cannot push the other pane out of alignment.
func highlightCell(text, other string, width int) string {
	pre, suffix, _ := diff.HighlightPair(text, other)
	runes := []rune(text)
	base := lipgloss.NewStyle().Background(colBgChg)
	highlight := lipgloss.NewStyle().Background(colBgChgHL).Bold(true)
	full := base.Render(string(runes[:pre])) +
		highlight.Render(string(runes[pre:len(runes)-suffix])) +
		base.Render(string(runes[len(runes)-suffix:]))
	clipped := truncateRunes(full, width)
	return clipped + base.Render(strings.Repeat(" ", max(0, width-lipgloss.Width(clipped))))
}

// --- Unified rendering ---

type unifiedLine struct {
	row  int // source row in FD.Rows, used when switching modes
	text string
	kind diff.RowKind
	hdr  string // non-empty for hunk header rows
}

func (d *DiffView) unifiedLines() []unifiedLine {
	var out []unifiedLine
	if d.FD == nil {
		return out
	}
	for hi, h := range d.FD.Hunks {
		out = append(out, unifiedLine{hdr: chunkLabel(d.FD.Hunks, hi), row: h.RowStart})
		for i := h.RowStart; i < h.RowEnd; i++ {
			r := d.FD.Rows[i]
			switch r.Kind {
			case diff.RowEqual:
				out = append(out, unifiedLine{row: i, text: " " + r.LeftText, kind: r.Kind})
			case diff.RowDelete:
				out = append(out, unifiedLine{row: i, text: "-" + r.LeftText, kind: r.Kind})
			case diff.RowInsert:
				out = append(out, unifiedLine{row: i, text: "+" + r.RightText, kind: r.Kind})
			case diff.RowChange:
				if r.LeftNo >= 0 {
					out = append(out, unifiedLine{row: i, text: "-" + r.LeftText, kind: diff.RowDelete})
				}
				if r.RightNo >= 0 {
					out = append(out, unifiedLine{row: i, text: "+" + r.RightText, kind: diff.RowInsert})
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
			t := truncateRunes(displayText(ul.text), w)
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
