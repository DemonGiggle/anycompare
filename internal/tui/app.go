package tui

import (
	"fmt"

	"github.com/anycompare/anycompare/internal/compare"
	"github.com/anycompare/anycompare/internal/diff"
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
}

type hashDoneMsg struct {
	rel       string
	identical bool
	err       error
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
	// autoOpen triggers a jump straight into the side-by-side diff
	// of the first differing file after the initial scan, so typing
	// `anycompare <left> <right>` shows differences immediately.
	// Rescans (R) never auto-open.
	autoOpen bool
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
		autoOpen:    true,
	}
}

// Summary exposes the last scan summary (for tests).
func (a *App) Summary() compare.Summary { return a.summary }

// SetIgnoreWS seeds the initial whitespace-ignore preference.
func (a *App) SetIgnoreWS(ignore bool) { a.ignoreWS = ignore }

// SetAutoOpen enables (default) or disables the jump straight into the
// first differing file's side-by-side diff after the initial scan.
func (a *App) SetAutoOpen(open bool) { a.autoOpen = open }

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
	left, right, opts := a.browser.LeftRoot, a.browser.RightRoot, a.compareOpts
	return func() tea.Msg {
		entries, summary, err := compare.Scan(left, right, opts)
		return scanDoneMsg{entries: entries, summary: summary, err: err}
	}
}

func hashCmd(entry compare.Entry) tea.Cmd {
	return func() tea.Msg {
		same, err := compare.VerifyEntry(entry)
		return hashDoneMsg{rel: entry.Rel, identical: same, err: err}
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
		if a.autoOpen {
			a.autoOpen = false
			if cmd := a.autoOpenFirstDiff(); cmd != nil {
				return a, cmd
			}
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

// autoOpenFirstDiff jumps straight into the side-by-side diff of the
// first differing file pair after the initial scan. It prefers entries
// flagged different, then any diffable file pair in the (differences
// filtered) browser list. Directories, one-sided entries and symlinks
// are not diffable. Returns nil to stay in the browser when nothing
// diffable exists (e.g. trees are identical).
func (a *App) autoOpenFirstDiff() tea.Cmd {
	b := a.browser
	if len(b.Visible) == 0 {
		b.StatusMsg = "no differences"
		return nil
	}
	pick := -1
	for i := range b.Visible {
		if diffable(b.Visible[i]) && b.Visible[i].State == compare.StateDifferent {
			pick = i
			break
		}
	}
	if pick < 0 {
		for i := range b.Visible {
			if diffable(b.Visible[i]) {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		// Differences exist but none is a file pair (e.g. only
		// one-sided entries or directories): stay in the browser.
		return nil
	}
	b.Cursor = pick
	b.ensureVisible()
	return a.openDiff(b.Visible[pick])
}

// diffable reports whether an entry can be opened as a file diff.
func diffable(e compare.Entry) bool {
	if e.Left == nil || e.Right == nil || e.IsDir() {
		return false
	}
	if e.Left.IsSymlink || e.Right.IsSymlink {
		return false
	}
	return true
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
	// vimdiff [c ]c two-key sequences and fallbacks first.
	if key == "[" || key == "]" || key == "c" || key == "C" || key == "n" || key == "N" || key == "p" {
		if d.HandleBracket(key) {
			// "[" alone arms; "N"/"p"/"n"/"]" already navigated.
			if key == "c" || key == "C" {
				// Resolve pending "[c" as prev-hunk ("]c" already moved).
				// HandleBracket consumed it; determine direction by
				// checking nothing else — treat bare "c" after "[" as prev.
				d.PrevHunk()
			}
			return a, nil
		}
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
