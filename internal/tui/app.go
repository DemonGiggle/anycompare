package tui

import (
	"fmt"
	"strings"

	"github.com/DemonGiggle/anycompare/internal/compare"
	"github.com/DemonGiggle/anycompare/internal/diff"
	tea "github.com/charmbracelet/bubbletea"
)

// ViewState selects the active pane.
type ViewState int

const (
	ViewBrowser ViewState = iota
	ViewDiff
)

// Messages for async work.
type scanDoneMsg struct {
	entries []compare.Entry
	summary compare.Summary
	err     error
	gen     int
}

type hashDoneMsg struct {
	rel       string
	identical bool
	err       error
}

// verifyDoneMsg carries background content-check results for one scan
// generation; stale generations are dropped (a rescan superseded them).
type verifyDoneMsg struct {
	gen     int
	results []contentResult
}

type contentResult struct {
	rel       string
	identical bool
}

type diffDoneMsg struct {
	rel       string
	leftPath  string
	rightPath string
	fd        *diff.FileDiff
	err       error
	ignoreWS  bool
	context   int
}

// App is the root Bubble Tea model (Elm-style): it owns the browser
// and diff view and routes keys/messages.
type App struct {
	browser     *Browser
	diffview    *DiffView
	active      ViewState
	compareOpts compare.Options
	diffContext int
	ignoreWS    bool
	scanning    bool
	errMsg      string
	summary     compare.Summary
	width       int
	height      int
	// scanGen tags each scan (and its follow-up content check) so a
	// slow completion cannot overwrite a newer rescan's rows.
	scanGen int
}

// NewApp builds the root model.
func NewApp(leftRoot, rightRoot string, compareOpts compare.Options, context int) *App {
	if context < 0 {
		context = 3
	}
	return &App{
		browser:     NewBrowser(leftRoot, rightRoot),
		diffview:    NewDiffView(),
		active:      ViewBrowser,
		compareOpts: compareOpts,
		diffContext: context,
		width:       80,
		height:      24,
		scanning:    true,
	}
}

// Summary exposes the last scan summary (for tests).
func (a *App) Summary() compare.Summary { return a.summary }

// SetIgnoreWS seeds the initial whitespace-ignore preference.
func (a *App) SetIgnoreWS(ignore bool) { a.ignoreWS = ignore }

// Browser exposes the browser (for tests).
func (a *App) Browser() *Browser { return a.browser }

// Diff exposes the diff view (for tests).
func (a *App) Diff() *DiffView { return a.diffview }

// Active exposes the active view (for tests).
func (a *App) Active() ViewState { return a.active }

// Init starts the initial rescan.
func (a *App) Init() tea.Cmd {
	return a.scanCmd()
}

func (a *App) scanCmd() tea.Cmd {
	a.scanGen++
	gen := a.scanGen
	left, right, opts := a.browser.LeftRoot, a.browser.RightRoot, a.compareOpts
	return func() tea.Msg {
		entries, summary, err := compare.Scan(left, right, opts)
		return scanDoneMsg{entries: entries, summary: summary, err: err, gen: gen}
	}
}

func hashCmd(entry compare.Entry) tea.Cmd {
	return func() tea.Msg {
		same, err := compare.VerifyEntry(entry)
		return hashDoneMsg{rel: entry.Rel, identical: same, err: err}
	}
}

// pendingContentChecks collects the fast-path-ambiguous rows: same-size
// file pairs flagged Different on mtime alone.
func pendingContentChecks(entries []compare.Entry) []compare.Entry {
	var pending []compare.Entry
	for _, e := range entries {
		if e.NeedsContentCheck() {
			pending = append(pending, e)
		}
	}
	return pending
}

// verifyContentsCmd hash-checks the ambiguous rows sequentially in the
// background. Hash errors resolve nothing: the row keeps its fast-path
// flag and a rescan or H can retry it.
func verifyContentsCmd(gen int, pending []compare.Entry) tea.Cmd {
	snap := make([]compare.Entry, len(pending))
	copy(snap, pending)
	return func() tea.Msg {
		results := make([]contentResult, 0, len(snap))
		for _, e := range snap {
			if e.Left == nil || e.Right == nil {
				continue
			}
			same, err := compare.HashesEqual(e.Left.AbsPath, e.Right.AbsPath)
			if err != nil {
				continue
			}
			results = append(results, contentResult{rel: e.Rel, identical: same})
		}
		return verifyDoneMsg{gen: gen, results: results}
	}
}

func diffLoadCmd(rel, leftPath, rightPath string, ignoreWS bool, context int) tea.Cmd {
	return func() tea.Msg {
		fd, err := diff.LoadPair(rel, leftPath, rightPath, ignoreWS, context)
		return diffDoneMsg{rel: rel, leftPath: leftPath, rightPath: rightPath, fd: fd, err: err, ignoreWS: ignoreWS, context: context}
	}
}

// Update routes messages to the active view.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.browser.SetSize(msg.Width, msg.Height)
		a.diffview.SetSize(msg.Width, msg.Height)
		return a, nil
	case scanDoneMsg:
		if msg.gen != a.scanGen {
			return a, nil // stale: a newer rescan superseded this one
		}
		a.scanning = false
		if msg.err != nil {
			a.errMsg = msg.err.Error()
			a.browser.StatusMsg = "scan error: " + msg.err.Error()
			return a, nil
		}
		a.errMsg = ""
		a.summary = msg.summary
		// Preserve selection across rescans.
		sel := ""
		if cur := a.browser.CursorEntry(); cur != nil {
			sel = cur.Rel
		}
		a.browser.SetEntries(msg.entries)
		if sel != "" {
			for i := range a.browser.Visible {
				if a.browser.Visible[i].Rel == sel {
					a.browser.Cursor = i
					break
				}
			}
			a.browser.ensureVisible()
		}
		a.browser.StatusMsg = ""
		// The fast path flags same-size/mtime-differing files without
		// reading them, so an unchanged file can flash as ≠ until it is
		// opened. Settle those rows with one background content check
		// instead of waiting for the user to open each one.
		if pending := pendingContentChecks(msg.entries); len(pending) > 0 {
			a.browser.StatusMsg = fmt.Sprintf("verifying contents (%d files)…", len(pending))
			return a, verifyContentsCmd(msg.gen, pending)
		}
		return a, nil
	case hashDoneMsg:
		if msg.err != nil {
			a.browser.StatusMsg = "hash error: " + msg.err.Error()
			return a, nil
		}
		for _, e := range a.browser.All {
			if e.Rel == msg.rel {
				if msg.identical {
					e.State = compare.StateIdentical
					e.Note = "verified identical (hash)"
				} else {
					e.State = compare.StateDifferent
					e.Note = "content differs (hash)"
				}
				a.browser.UpdateEntry(e)
				if msg.identical {
					a.browser.StatusMsg = msg.rel + ": contents identical (SHA-256)"
				} else {
					a.browser.StatusMsg = msg.rel + ": contents differ (SHA-256)"
				}
				break
			}
		}
		return a, nil
	case diffDoneMsg:
		a.diffview.Loading = false
		if msg.err != nil {
			a.diffview.ErrMsg = msg.err.Error()
			return a, nil
		}
		a.diffview.IgnoreWS = msg.ignoreWS
		a.diffview.Context = msg.context
		a.diffview.SetDiff(msg.rel, msg.leftPath, msg.rightPath, msg.fd)
		a.diffview.SetSize(a.width, a.height)
		// Opening the diff already read both files in full: byte-identical
		// content proves a fast-path ≠ (e.g. mtime-only) wrong, so correct
		// the browser row instead of leaving the views in disagreement.
		if msg.fd != nil && msg.fd.SameBytes && !msg.fd.Truncated {
			a.markBrowserIdentical(msg.rel)
		}
		return a, nil
	case verifyDoneMsg:
		if msg.gen != a.scanGen {
			return a, nil // stale: a newer rescan superseded this check
		}
		var same, differ int
		for _, r := range msg.results {
			for _, e := range a.browser.All {
				if e.Rel != r.rel {
					continue
				}
				if e.State != compare.StateDifferent {
					break // already resolved (opened or H-verified)
				}
				if r.identical {
					e.State = compare.StateIdentical
					e.Note = "verified identical (content)"
					same++
				} else {
					e.State = compare.StateDifferent
					e.Note = "content differs (hash)"
					differ++
				}
				a.browser.UpdateEntry(e)
				break
			}
		}
		// Only replace our own transient message; a user key (f/s/H/R)
		// pressed mid-check owns the status line instead.
		if strings.HasPrefix(a.browser.StatusMsg, "verifying contents") {
			if same+differ == 0 {
				a.browser.StatusMsg = ""
			} else {
				a.browser.StatusMsg = fmt.Sprintf("content check: %d identical, %d differ", same, differ)
			}
		}
		return a, nil
	case tea.KeyMsg:
		return a.handleKey(msg)
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	// Global interrupt.
	if key == "ctrl+c" {
		return a, tea.Quit
	}
	if a.active == ViewDiff {
		return a.updateDiffKey(key)
	}
	return a.updateBrowserKey(key)
}

func (a *App) updateBrowserKey(key string) (tea.Model, tea.Cmd) {
	b := a.browser
	switch key {
	case "q", "ctrl+d":
		return a, tea.Quit
	case "up", "k":
		b.MoveCursor(-1)
	case "down", "j":
		b.MoveCursor(1)
	case "pgup":
		b.Page(-1)
	case "pgdown", " ":
		b.Page(1)
	case "g":
		b.Cursor = 0
		b.Offset = 0
	case "G":
		if len(b.Visible) > 0 {
			b.Cursor = len(b.Visible) - 1
			b.ensureVisible()
		}
	case "tab":
		b.FocusLeft = !b.FocusLeft
	case "f":
		b.StatusMsg = "filter: " + b.FilterNext()
	case "s":
		b.StatusMsg = "sort: " + b.SortNext()
	case "?":
		b.ShowHelp = !b.ShowHelp
	case "R":
		a.scanning = true
		b.StatusMsg = "rescanning…"
		return a, a.scanCmd()
	case "H":
		cur := b.CursorEntry()
		if cur == nil {
			b.StatusMsg = "nothing selected"
			return a, nil
		}
		if cur.Left == nil || cur.Right == nil || cur.IsDir() {
			b.StatusMsg = "hash-verify needs a file pair"
			return a, nil
		}
		b.StatusMsg = "hashing " + cur.Rel + "…"
		return a, hashCmd(*cur)
	case "enter":
		cur := b.CursorEntry()
		if cur == nil {
			return a, nil
		}
		cmd := a.openDiff(*cur)
		return a, cmd
	case "esc":
		b.ShowHelp = false
	}
	return a, nil
}

// markBrowserIdentical flips one browser row to identical after its
// content was verified byte-for-byte (e.g. by opening its diff),
// keeping the views consistent without an extra hash pass.
func (a *App) markBrowserIdentical(rel string) {
	for _, e := range a.browser.All {
		if e.Rel == rel {
			if e.State == compare.StateIdentical {
				return
			}
			e.State = compare.StateIdentical
			e.Note = "verified identical (content)"
			a.browser.UpdateEntry(e)
			a.browser.StatusMsg = rel + ": contents identical"
			return
		}
	}
}

// openDiff validates the selection and either shows a status message
// or switches to the diff view with an async load.
func (a *App) openDiff(e compare.Entry) tea.Cmd {
	b := a.browser
	switch {
	case e.State == compare.StateOnlyLeft || e.Left != nil && e.Right == nil:
		b.StatusMsg = e.Rel + " exists only on the left"
		return nil
	case e.State == compare.StateOnlyRight || e.Left == nil || e.Right == nil:
		b.StatusMsg = e.Rel + " exists only on the right"
		return nil
	case e.IsDir():
		b.StatusMsg = e.Rel + " is a directory (no content diff)"
		return nil
	case e.Left.IsSymlink || e.Right.IsSymlink:
		b.StatusMsg = fmt.Sprintf("%s symlink: L=%q R=%q", e.Rel, e.Left.LinkTarget, e.Right.LinkTarget)
		return nil
	}
	a.active = ViewDiff
	a.diffview.Loading = true
	a.diffview.Rel = e.Rel
	a.diffview.ErrMsg = ""
	a.diffview.Mode = ModeSideBySide
	a.diffview.IgnoreWS = a.ignoreWS
	a.diffview.Context = a.diffContext
	a.diffview.Offset = 0
	a.diffview.SetSize(a.width, a.height)
	return diffLoadCmd(e.Rel, e.Left.AbsPath, e.Right.AbsPath, a.ignoreWS, a.diffContext)
}

func (a *App) updateDiffKey(key string) (tea.Model, tea.Cmd) {
	d := a.diffview
	if d.HandleBracket(key) {
		return a, nil
	}
	switch key {
	case "q", "esc":
		a.active = ViewBrowser
		// Persist toggles back to browser-level defaults for next file.
		a.ignoreWS = d.IgnoreWS
		a.diffContext = d.Context
		return a, nil
	case "up", "k":
		d.Scroll(-1)
	case "down", "j":
		d.Scroll(1)
	case "pgup":
		d.Page(-1)
	case "pgdown", " ":
		d.Page(1)
	case "g":
		d.Top()
	case "G":
		d.Bottom()
	case "d", "D":
		d.ToggleMode()
	case "w", "W":
		d.IgnoreWS = !d.IgnoreWS
		a.ignoreWS = d.IgnoreWS
		a.recomputeDiff()
	case "+", "=", "zo":
		if d.Context < 10 {
			d.Context++
			a.diffContext = d.Context
			a.recomputeDiff()
		}
	case "-", "_", "zc":
		if d.Context > 0 {
			d.Context--
			a.diffContext = d.Context
			a.recomputeDiff()
		}
	case "b", "B":
		d.ShowHex = !d.ShowHex
	}
	return a, nil
}

// recomputeDiff rebuilds hunks from cached lines after a whitespace/
// context toggle without re-reading files.
func (a *App) recomputeDiff() {
	d := a.diffview
	if d.FD == nil {
		return
	}
	fd := diff.Compute(d.FD.LeftLines, d.FD.RightLines, d.IgnoreWS, d.Context)
	fd.Rel = d.FD.Rel
	fd.IsBinary = d.FD.IsBinary
	fd.Truncated = d.FD.Truncated
	// SameBytes/LineEndingOnly describe the raw bytes, which the
	// whitespace/context toggles do not change.
	fd.SameBytes = d.FD.SameBytes
	fd.LineEndingOnly = d.FD.LineEndingOnly
	sel := d.Offset
	d.SetDiff(d.Rel, d.LeftPath, d.RightPath, fd)
	// Keep the scroll position where possible.
	d.Offset = sel
	d.clamp()
	d.syncHunkToOffset()
}

// View renders the active view.
func (a *App) View() string {
	if a.scanning && len(a.browser.All) == 0 {
		return styleSubHeader.Render("scanning directories…")
	}
	if a.errMsg != "" && len(a.browser.All) == 0 {
		return styleDiffSym.Render("error: " + a.errMsg)
	}
	if a.active == ViewDiff {
		return a.diffview.Render()
	}
	return a.browser.Render()
}
