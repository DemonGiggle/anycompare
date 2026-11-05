package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	colFaint   = lipgloss.Color("245")
	colGreen   = lipgloss.Color("42")
	colRed     = lipgloss.Color("196")
	colYellow  = lipgloss.Color("220")
	colBlue    = lipgloss.Color("75")
	colPink    = lipgloss.Color("218")
	colBgSel   = lipgloss.Color("238")
	colBgChg   = lipgloss.Color("52")
	colBgChgHL = lipgloss.Color("124")
	colBgAdd   = lipgloss.Color("22")
	colBgDel   = lipgloss.Color("52")
)

var (
	styleHeader     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	styleSubHeader  = lipgloss.NewStyle().Foreground(colFaint)
	styleSelected   = lipgloss.NewStyle().Background(colBgSel).Bold(true)
	styleIdent      = lipgloss.NewStyle().Foreground(colFaint)
	styleDiffSym    = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	styleOnlyLeft   = lipgloss.NewStyle().Foreground(colYellow).Bold(true)
	styleOnlyRight  = lipgloss.NewStyle().Foreground(colBlue).Bold(true)
	styleFooter     = lipgloss.NewStyle().Foreground(colFaint)
	styleHunkHeader = lipgloss.NewStyle().Foreground(lipgloss.Color("159")).Bold(true)
	styleLineNo     = lipgloss.NewStyle().Foreground(colFaint)
)

// truncateRunes cuts s to at most n display runes (no ANSI awareness;
// callers pass plain text before styling).
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// padRight pads s with spaces to width n (in runes).
func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return s
	}
	return s + string(make([]rune, n-len(r)))
}
