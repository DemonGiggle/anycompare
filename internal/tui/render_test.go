package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DemonGiggle/anycompare/internal/compare"
	"github.com/DemonGiggle/anycompare/internal/diff"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func assertFrame(t *testing.T, output string, width, height int) {
	t.Helper()
	plain := stripANSI(output)
	if strings.ContainsAny(plain, "\x00\x1b\r\t") {
		t.Fatalf("unexpanded terminal controls in %q", plain)
	}
	lines := strings.Split(plain, "\n")
	if len(lines) > height {
		t.Fatalf("frame height %d exceeds %d", len(lines), height)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d width %d exceeds %d: %q", i, got, width, line)
		}
	}
}

func TestBrowserHasTwoFilePanes(t *testing.T) {
	b := NewBrowser("/source", "/dest")
	b.SetEntries(testEntries())
	b.FilterNext()
	lines := strings.Split(stripANSI(b.Render()), "\n")
	for i, entry := range b.Visible {
		line := []rune(lines[3+i]) // fixture is ASCII except for the one-column gutter
		left, right := string(line[:38]), string(line[41:])
		if strings.Contains(left, entry.Rel) != (entry.Left != nil) {
			t.Errorf("wrong source cell for %s: %q", entry.Rel, left)
		}
		if strings.Contains(right, entry.Rel) != (entry.Right != nil) {
			t.Errorf("wrong destination cell for %s: %q", entry.Rel, right)
		}
		if entry.Left == nil && strings.TrimSpace(left) != "" {
			t.Errorf("missing source is not blank: %q", left)
		}
		if entry.Right == nil && strings.TrimSpace(right) != "" {
			t.Errorf("missing destination is not blank: %q", right)
		}
	}
	b.MoveCursor(1)
	lines = strings.Split(stripANSI(b.Render()), "\n")
	if strings.Contains(lines[3], "›") || !strings.Contains(lines[4], "›") {
		t.Fatal("cursor marker did not follow selection")
	}
}

func TestDisplayTextExpandsTabsByColumn(t *testing.T) {
	if got := displayText("界\tx\tend\x00\r"); got != "界  x   end^@^M" {
		t.Fatalf("unexpected terminal text: %q", got)
	}
}

func TestViewsFitTerminal(t *testing.T) {
	old := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(old)
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.ANSI256} {
		lipgloss.SetColorProfile(profile)
		for _, width := range []int{1, 23, 24, 39, 40, 80, 81, 120} {
			for _, height := range []int{4, 8, 24} {
				t.Run(fmt.Sprintf("%d/%dx%d", profile, width, height), func(t *testing.T) {
					text := "\t界界e\u0301👩‍💻\x1b[2J\x00" + strings.Repeat("long line ", 20)
					b := NewBrowser(text, text)
					b.SetSize(width, height)
					b.SetEntries([]compare.Entry{{Rel: text, State: compare.StateDifferent, Left: &compare.FileMeta{}, Right: &compare.FileMeta{}}})
					assertFrame(t, b.Render(), width, height)
					b.ShowHelp = true
					assertFrame(t, b.Render(), width, height)
					d := NewDiffView()
					d.SetSize(width, height)
					d.SetDiff(text, text, text, diff.Compute([]string{text}, []string{text + "changed"}, false, 1))
					// Include all row types, independently of the diff alignment algorithm.
					d.FD.Rows = []diff.Row{
						{Kind: diff.RowEqual, LeftNo: 0, RightNo: 0, LeftText: text, RightText: text},
						{Kind: diff.RowChange, LeftNo: 1, RightNo: 1, LeftText: text, RightText: text + "changed"},
						{Kind: diff.RowDelete, LeftNo: 2, RightNo: -1, LeftText: text},
						{Kind: diff.RowInsert, LeftNo: -1, RightNo: 2, RightText: text},
					}
					assertFrame(t, d.Render(), width, height)
					if width >= 24 && height >= 8 {
						for _, line := range strings.Split(d.renderSideBySide(width, 4), "\n") {
							if lipgloss.Width(line) != width {
								t.Fatalf("unstable pane width: %q", line)
							}
						}
					}
					d.ToggleMode()
					assertFrame(t, d.Render(), width, height)
				})
			}
		}
	}
}

func TestDiffHunkKeysAndModeCoordinates(t *testing.T) {
	left := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}
	right := append([]string(nil), left...)
	right[4], right[9] = "changed", "changed again"
	app := NewApp("L", "R", compare.DefaultOptions(), 0)
	d := app.Diff()
	d.SetDiff("file", "L", "R", diff.Compute(left, right, false, 0))
	for _, mode := range []ViewMode{ModeSideBySide, ModeUnified} {
		if d.Mode != mode {
			d.ToggleMode()
		}
		d.PrevHunk()
		start := d.Offset
		app.updateDiffKey("]")
		next := d.Offset
		app.updateDiffKey("c")
		if d.Offset != next || d.HunkIdx != 1 {
			t.Fatal("]c must jump exactly once to the second hunk")
		}
		if mode == ModeUnified && d.unifiedLines()[d.Offset].hdr != chunkLabel(d.FD.Hunks, 1) {
			t.Fatal("unified jump used a side-by-side row offset")
		}
		app.updateDiffKey("[")
		if d.HunkIdx != 0 {
			t.Fatal("single [ did not navigate")
		}
		app.updateDiffKey("c")
		if d.Offset != start {
			t.Fatal("[c jumped twice")
		}
		app.updateDiffKey("]")
		app.updateDiffKey("k")
		before := d.Offset
		app.updateDiffKey("c")
		if d.Offset != before {
			t.Fatal("unrelated key did not disarm bracket sequence")
		}
	}
	d.Mode = ModeSideBySide
	d.Offset = 9
	d.ToggleMode()
	if d.HunkIdx != 1 || d.unifiedLines()[d.Offset].row != 9 {
		t.Fatal("mode toggle lost current change")
	}
	d.ToggleMode()
	if d.Offset != 9 {
		t.Fatal("mode round trip lost source row")
	}
}

func TestBinaryContentRequiresToggle(t *testing.T) {
	d := NewDiffView()
	fd := diff.Compute([]string{"hidden\x00left"}, []string{"hidden\x00right"}, false, 0)
	fd.IsBinary = true
	d.SetDiff("binary", "L", "R", fd)
	if strings.Contains(d.Render(), "hidden") {
		t.Fatal("binary content shown before b toggle")
	}
	d.ShowHex = true
	if !strings.Contains(d.Render(), "hidden^@") {
		t.Fatal("binary-as-text missing visible controls")
	}
	assertFrame(t, d.Render(), 80, 24)
}

func TestDiffViewLineEndingBanner(t *testing.T) {
	d := NewDiffView()
	d.SetSize(80, 24)
	// No line hunks, but the loader saw the bytes differ only in
	// line endings: the view must not claim "files identical".
	fd := diff.Compute([]string{"a"}, []string{"a"}, false, 3)
	fd.LineEndingOnly = true
	d.SetDiff("crlf.txt", "L", "R", fd)
	out := d.Render()
	if !strings.Contains(out, "line endings differ") {
		t.Fatalf("missing line-ending banner in:\n%s", out)
	}
	if strings.Contains(out, "files identical") {
		t.Fatalf("must not claim identical for CRLF pair in:\n%s", out)
	}
	assertFrame(t, out, 80, 24)
}

func TestHunkChunkNotation(t *testing.T) {
	left := []string{"a", "b", "c", "d", "e", "f", "g"}
	right := []string{"a", "X", "c", "d", "e", "Y", "g"}
	d := NewDiffView()
	d.SetSize(100, 20)
	d.SetDiff("f.txt", "L", "R", diff.Compute(left, right, false, 1))
	if len(d.FD.Hunks) != 2 {
		t.Fatalf("hunks=%d want 2", len(d.FD.Hunks))
	}
	// Side-by-side hunk bar carries the chunk number.
	if out := d.Render(); !strings.Contains(out, "[1/2] @@") {
		t.Fatalf("missing [1/2] chunk notation in:\n%s", out)
	}
	d.NextHunk()
	if out := d.Render(); !strings.Contains(out, "[2/2] @@") {
		t.Fatalf("notation did not follow n jump in:\n%s", out)
	}
	d.PrevHunk()
	if out := d.Render(); !strings.Contains(out, "[1/2] @@") {
		t.Fatalf("notation did not follow N jump in:\n%s", out)
	}
	// Unified inline headers carry it too.
	d.ToggleMode()
	var hdrs []string
	for _, ul := range d.unifiedLines() {
		if ul.hdr != "" {
			hdrs = append(hdrs, ul.hdr)
		}
	}
	for _, want := range []string{"[1/2] @@", "[2/2] @@"} {
		seen := false
		for _, hdr := range hdrs {
			if strings.Contains(hdr, want) {
				seen = true
			}
		}
		if !seen {
			t.Fatalf("missing unified %q in %q", want, hdrs)
		}
	}
	assertFrame(t, d.Render(), 100, 20)
}

func TestBottomShowsLastPage(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprint(i)
	}
	d := NewDiffView()
	d.SetSize(80, 10)
	d.SetDiff("file", "L", "R", diff.Compute(lines, lines, false, 0))
	d.Bottom()
	if d.Offset != 94 {
		t.Fatalf("bottom offset = %d, want last full page at 94", d.Offset)
	}
}
