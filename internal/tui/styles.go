package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// truncateRunes clips to terminal columns, preserving wide characters and ANSI styles.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

// padRight pads s with actual spaces to n terminal columns.
func padRight(s string, n int) string {
	w := ansi.StringWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

func fitCell(s string, width int) string {
	return padRight(truncateRunes(s, width), width)
}

// displayText keeps file contents and paths on one terminal row. Tabs use
// four-column stops; other control characters are shown visibly.
func displayText(s string) string {
	var out strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			out.WriteRune(r)
		case r < 32:
			out.WriteRune('^')
			out.WriteRune(r + 64)
		case unicode.IsControl(r):
			out.WriteRune('�')
		default:
			out.WriteRune(r)
		}
	}
	parts := strings.Split(out.String(), "\t")
	if len(parts) == 1 {
		return parts[0]
	}
	out.Reset()
	column := 0
	for i, part := range parts {
		if i > 0 {
			spaces := 4 - column%4
			out.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		}
		out.WriteString(part)
		column += ansi.StringWidth(part)
	}
	return out.String()
}

func smallTerminal(w, h int) string {
	return truncateRunes("Resize terminal (at least 24×8)", w)
}
