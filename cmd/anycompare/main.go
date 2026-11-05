// Command anycompare is a vimdiff-style TUI directory comparison tool.
//
// Usage:
//
//	anycompare [flags] <source-dir> <dest-dir>
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/anycompare/anycompare/internal/compare"
	"github.com/anycompare/anycompare/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

type ignoreList []string

func (l *ignoreList) String() string     { return strings.Join(*l, ",") }
func (l *ignoreList) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	var (
		noMtime  = flag.Bool("no-mtime", false, "compare by size only (ignore mtime on the fast path)")
		hash     = flag.Bool("hash", false, "hash file contents during the initial scan (slower, definitive)")
		context  = flag.Int("context", 3, "unified context lines around each change hunk")
		summary  = flag.Bool("summary", false, "print a plain-text comparison summary and exit (no TUI)")
		showVer  = flag.Bool("version", false, "print version and exit")
		ignoreWs = flag.Bool("ignore-ws", false, "ignore whitespace in file diffs initially (toggle with w)")
		browser  = flag.Bool("browser", false, "start in the folder browser instead of jumping to the first diff")
	)
	var ignores ignoreList
	flag.Var(&ignores, "ignore", "shell pattern to ignore (repeatable, e.g. -ignore '*.log' -ignore '.git/*')")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: anycompare [flags] <source-dir> <dest-dir>\n\n")
		fmt.Fprintf(os.Stderr, "vimdiff-style TUI folder comparison (read-only).\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nKeys (browser): j/k navigate, enter open diff, f filter, s sort, H hash-verify, R rescan, ? help, q quit\n")
		fmt.Fprintf(os.Stderr, "Keys (diff): j/k scroll, n/N or ]/[ or ]c/[c hunk, d view, w whitespace, +/- context, b binary, esc back\n")
	}
	flag.Parse()

	if *showVer {
		fmt.Println("anycompare " + version)
		return
	}
	args := flag.Args()
	if len(args) != 2 {
		flag.Usage()
		os.Exit(2)
	}
	left, right := args[0], args[1]
	for _, r := range []string{left, right} {
		fi, err := os.Stat(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "anycompare: %s: %v\n", r, err)
			os.Exit(1)
		}
		if !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "anycompare: %s: not a directory\n", r)
			os.Exit(1)
		}
	}

	compareOpts := compare.Options{
		RespectMtime: !*noMtime,
		HashOnScan:   *hash,
		Ignore:       []string(ignores),
	}
	if *context < 0 {
		*context = 0
	}
	if *context > 10 {
		*context = 10
	}

	if *summary {
		entries, sum, err := compare.Scan(left, right, compareOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "anycompare: scan: %v\n", err)
			os.Exit(1)
		}
		for _, e := range entries {
			fmt.Printf("%s  %s\n", e.State.Symbol(), e.Rel)
		}
		fmt.Printf("total=%d identical=%d different=%d only-left=%d only-right=%d\n",
			sum.Total, sum.Identical, sum.Different, sum.OnlyLeft, sum.OnlyRight)
		return
	}

	app := tui.NewApp(left, right, compareOpts, *context)
	// Seed the initial whitespace preference before the first diff loads.
	app.SetIgnoreWS(*ignoreWs)
	app.SetAutoOpen(!*browser)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "anycompare: %v\n", err)
		os.Exit(1)
	}
}
