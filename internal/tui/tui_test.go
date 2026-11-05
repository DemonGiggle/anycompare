package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anycompare/anycompare/internal/compare"
	"github.com/anycompare/anycompare/internal/diff"
)

func testEntries() []compare.Entry {
	pair := &compare.FileMeta{Size: 12}
	return []compare.Entry{
		{Rel: "same.txt", State: compare.StateIdentical, Left: pair, Right: pair},
		{Rel: "diff.txt", State: compare.StateDifferent, Left: pair, Right: pair},
		{Rel: "left-only.txt", State: compare.StateOnlyLeft, Left: pair},
		{Rel: "right-only.txt", State: compare.StateOnlyRight, Right: pair},
	}
}

func TestBrowserFilterCycle(t *testing.T) {
	b := NewBrowser("L", "R")
	b.SetSize(80, 20)
	b.SetEntries(testEntries())
	// Default filter shows differences only (identical hidden).
	if len(b.Visible) != 3 {
		t.Fatalf("default differences visible=%d want 3", len(b.Visible))
	}
	b.FilterNext() // all
	if len(b.Visible) != 4 {
		t.Fatalf("all filter: %+v", b.Visible)
	}
	b.FilterNext() // different
	if len(b.Visible) != 1 || b.Visible[0].Rel != "diff.txt" {
		t.Fatalf("different filter: %+v", b.Visible)
	}
	b.FilterNext() // only-left
	if len(b.Visible) != 1 || b.Visible[0].Rel != "left-only.txt" {
		t.Fatalf("only-left filter: %+v", b.Visible)
	}
	b.FilterNext() // only-right
	if len(b.Visible) != 1 || b.Visible[0].Rel != "right-only.txt" {
		t.Fatalf("only-right filter: %+v", b.Visible)
	}
	b.FilterNext() // back to differences
	if len(b.Visible) != 3 {
		t.Fatalf("differences filter: %+v", b.Visible)
	}
}

func TestBrowserRenderContainsGutter(t *testing.T) {
	b := NewBrowser("L", "R")
	b.SetSize(80, 20)
	b.SetEntries(testEntries())
	b.Filter = FilterAll
	b.Apply()
	out := b.Render()
	for _, want := range []string{"=", "≠", "left-only.txt", "right-only.txt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q in:\n%s", want, out)
		}
	}
}

func TestDiffHunkNavigation(t *testing.T) {
	fd := diff.Compute([]string{"a", "b", "c", "d", "e", "f", "g"}, []string{"a", "X", "c", "d", "e", "Y", "g"}, false, 1)
	if len(fd.Hunks) != 2 {
		t.Fatalf("hunks=%d want 2", len(fd.Hunks))
	}
	d := NewDiffView()
	d.SetSize(100, 20)
	d.SetDiff("f.txt", "L", "R", fd)
	if d.Offset != fd.Hunks[0].RowStart {
		t.Fatalf("initial offset=%d want %d", d.Offset, fd.Hunks[0].RowStart)
	}
	d.NextHunk()
	if d.Offset != fd.Hunks[1].RowStart {
		t.Fatalf("next offset=%d want %d", d.Offset, fd.Hunks[1].RowStart)
	}
	d.PrevHunk()
	if d.Offset != fd.Hunks[0].RowStart {
		t.Fatalf("prev offset=%d want %d", d.Offset, fd.Hunks[0].RowStart)
	}
}

func TestDiffRenderModes(t *testing.T) {
	fd := diff.Compute([]string{"hello world"}, []string{"hello there"}, false, 3)
	d := NewDiffView()
	d.SetSize(100, 20)
	d.SetDiff("f.txt", "L", "R", fd)
	sbs := d.Render()
	if !strings.Contains(sbs, "hello") {
		t.Fatalf("side-by-side missing content:\n%s", sbs)
	}
	d.ToggleMode()
	uni := d.Render()
	if !strings.Contains(uni, "-hello world") || !strings.Contains(uni, "+hello there") {
		t.Fatalf("unified missing -/+ lines:\n%s", uni)
	}
}

func TestAppOpenDiffGuards(t *testing.T) {
	app := NewApp("L", "R", compare.DefaultOptions(), 3)
	app.browser.SetEntries([]compare.Entry{
		{Rel: "only.txt", State: compare.StateOnlyLeft, Left: &compare.FileMeta{AbsPath: "/l/only.txt"}},
		{Rel: "dir", State: compare.StateIdentical, Left: &compare.FileMeta{IsDir: true}, Right: &compare.FileMeta{IsDir: true}},
	})
	if cmd := app.openDiff(app.browser.All[0]); cmd != nil {
		t.Fatal("only-left should not open a diff")
	}
	if app.Active() != ViewBrowser {
		t.Fatal("should stay in browser for one-sided entries")
	}
	if cmd := app.openDiff(app.browser.All[1]); cmd != nil {
		t.Fatal("directories should not open a diff")
	}
}

func TestAppStaysInBrowserAfterScan(t *testing.T) {
	app := NewApp("L", "R", compare.DefaultOptions(), 3)
	entries := []compare.Entry{
		{Rel: "same.txt", State: compare.StateIdentical,
			Left: &compare.FileMeta{AbsPath: "/l/same.txt"}, Right: &compare.FileMeta{AbsPath: "/r/same.txt"}},
		{Rel: "aaa-diff.txt", State: compare.StateDifferent,
			Left:  &compare.FileMeta{AbsPath: "/l/aaa-diff.txt", Size: 1},
			Right: &compare.FileMeta{AbsPath: "/r/aaa-diff.txt", Size: 2}},
		{Rel: "zzz-diff.txt", State: compare.StateDifferent,
			Left:  &compare.FileMeta{AbsPath: "/l/zzz-diff.txt", Size: 3},
			Right: &compare.FileMeta{AbsPath: "/r/zzz-diff.txt", Size: 4}},
	}
	m, cmd := app.Update(scanDoneMsg{entries: entries})
	app = m.(*App)
	if cmd != nil {
		t.Fatal("initial scan should stay in the browser (no auto-open)")
	}
	if app.Active() != ViewBrowser {
		t.Fatal("should stay in the browser after startup")
	}
	if len(app.Browser().Visible) != 2 {
		t.Fatalf("browser should list differences, got %+v", app.Browser().Visible)
	}
}

func TestAppStaysInBrowserWhenIdentical(t *testing.T) {
	app := NewApp("L", "R", compare.DefaultOptions(), 3)
	entries := []compare.Entry{
		{Rel: "same.txt", State: compare.StateIdentical,
			Left: &compare.FileMeta{AbsPath: "/l/same.txt"}, Right: &compare.FileMeta{AbsPath: "/r/same.txt"}},
	}
	m, cmd := app.Update(scanDoneMsg{entries: entries})
	app = m.(*App)
	if cmd != nil {
		t.Fatal("no differing files: should stay in browser")
	}
	if app.Active() != ViewBrowser {
		t.Fatal("should stay in browser when trees are identical")
	}
}

func TestAppDiffOpenMarksBrowserIdentical(t *testing.T) {
	app := NewApp("L", "R", compare.DefaultOptions(), 3)
	app.browser.SetEntries([]compare.Entry{
		{Rel: "f.txt", State: compare.StateDifferent,
			Left: &compare.FileMeta{AbsPath: "/l/f.txt"}, Right: &compare.FileMeta{AbsPath: "/r/f.txt"}},
	})
	// Same lines, and the loader verified the raw bytes are equal
	// (e.g. same size, mtime-only browser flag).
	fd := diff.Compute([]string{"a"}, []string{"a"}, false, 3)
	fd.SameBytes = true
	m, _ := app.Update(diffDoneMsg{rel: "f.txt", leftPath: "/l/f.txt", rightPath: "/r/f.txt", fd: fd})
	app = m.(*App)
	var got *compare.Entry
	for i := range app.Browser().All {
		if app.Browser().All[i].Rel == "f.txt" {
			got = &app.Browser().All[i]
		}
	}
	if got == nil || got.State != compare.StateIdentical {
		t.Fatalf("byte-identical diff should correct browser row, got %+v", got)
	}
	if app.Browser().StatusMsg != "f.txt: contents identical" {
		t.Fatalf("unexpected status %q", app.Browser().StatusMsg)
	}
}

func TestAppDiffOpenKeepsDifferentWhenBytesDiffer(t *testing.T) {
	app := NewApp("L", "R", compare.DefaultOptions(), 3)
	app.browser.SetEntries([]compare.Entry{
		{Rel: "f.txt", State: compare.StateDifferent,
			Left: &compare.FileMeta{AbsPath: "/l/f.txt"}, Right: &compare.FileMeta{AbsPath: "/r/f.txt"}},
	})
	fd := diff.Compute([]string{"a"}, []string{"b"}, false, 3)
	m, _ := app.Update(diffDoneMsg{rel: "f.txt", leftPath: "/l/f.txt", rightPath: "/r/f.txt", fd: fd})
	app = m.(*App)
	if got := app.Browser().All[0]; got.State != compare.StateDifferent {
		t.Fatalf("differing content must stay different, got %+v", got)
	}
}

func writeVerifyFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanAutoVerifiesAmbiguousRows(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	// Same bytes, so only the fast-path mtime flag marks it ≠.
	l := writeVerifyFile(t, left, "same.txt", "identical-bytes")
	r := writeVerifyFile(t, right, "same.txt", "identical-bytes")
	lChanged := writeVerifyFile(t, left, "changed.txt", "short")
	rChanged := writeVerifyFile(t, right, "changed.txt", "longer-different-content")
	entries := []compare.Entry{
		{Rel: "same.txt", State: compare.StateDifferent, Note: "same size, mtime differs (H verifies content)",
			Left: &compare.FileMeta{AbsPath: l, Size: 15}, Right: &compare.FileMeta{AbsPath: r, Size: 15}},
		{Rel: "changed.txt", State: compare.StateDifferent, Note: "sizes differ",
			Left:  &compare.FileMeta{AbsPath: lChanged, Size: 5},
			Right: &compare.FileMeta{AbsPath: rChanged, Size: 24}},
	}
	app := NewApp(left, right, compare.DefaultOptions(), 3)
	m, cmd := app.Update(scanDoneMsg{entries: entries})
	app = m.(*App)
	if cmd == nil {
		t.Fatal("ambiguous rows should kick off a background content check")
	}
	if got := app.Browser().StatusMsg; !strings.HasPrefix(got, "verifying contents") {
		t.Fatalf("unexpected status %q", got)
	}
	msg, ok := cmd().(verifyDoneMsg)
	if !ok {
		t.Fatalf("expected verifyDoneMsg, got %T", cmd())
	}
	m2, _ := app.Update(msg)
	app = m2.(*App)
	var same, changedEntry *compare.Entry
	for i := range app.Browser().All {
		switch app.Browser().All[i].Rel {
		case "same.txt":
			same = &app.Browser().All[i]
		case "changed.txt":
			changedEntry = &app.Browser().All[i]
		}
	}
	if same == nil || same.State != compare.StateIdentical {
		t.Fatalf("content check should clear the mtime-only flag, got %+v", same)
	}
	if changedEntry == nil || changedEntry.State != compare.StateDifferent {
		t.Fatalf("size-differing row must stay different, got %+v", changedEntry)
	}
	if got := app.Browser().StatusMsg; got != "content check: 1 identical, 0 differ" {
		t.Fatalf("unexpected status %q", got)
	}
}

func TestStaleScanAndVerifyDropped(t *testing.T) {
	app := NewApp("L", "R", compare.DefaultOptions(), 3)
	_ = app.scanCmd() // gen 1: a newer scan is in flight
	m, cmd := app.Update(scanDoneMsg{entries: []compare.Entry{
		{Rel: "old.txt", State: compare.StateDifferent,
			Left:  &compare.FileMeta{AbsPath: "/l/old.txt"},
			Right: &compare.FileMeta{AbsPath: "/r/old.txt"}},
	}})
	app = m.(*App)
	if cmd != nil {
		t.Fatal("stale scan must not kick off work")
	}
	if len(app.Browser().All) != 0 {
		t.Fatalf("stale scan must not touch rows: %+v", app.Browser().All)
	}
	m2, _ := app.Update(verifyDoneMsg{gen: 999, results: []contentResult{{rel: "x", identical: true}}})
	app = m2.(*App)
	if len(app.Browser().All) != 0 {
		t.Fatal("stale verify must not touch rows")
	}
}
