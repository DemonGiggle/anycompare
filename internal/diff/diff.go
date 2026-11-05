// Package diff computes line-oriented diffs for side-by-side
// vimdiff-style rendering. It uses Myers O(ND) on (optionally
// whitespace-normalized) lines with a graceful fallback for very
// large inputs, plus prefix/suffix intra-line highlighting.
package diff

import (
	"io"
	"os"
	"strings"

	"github.com/DemonGiggle/anycompare/internal/compare"
)

const (
	// MaxFileBytes caps in-memory file loads; larger files are
	// truncated with Truncated set so the TUI can warn.
	MaxFileBytes = 10 << 20
	// MaxLinesMyers caps Myers diff input; above this the middle is
	// shown as one change block instead of running quadratic work.
	MaxLinesMyers = 20000
)

// RowKind classifies one aligned display row.
type RowKind int

const (
	RowEqual RowKind = iota
	RowDelete
	RowInsert
	RowChange
	RowGap // filler/omitted marker, not used in Rows itself
)

// Row is one aligned side-by-side line. Line numbers are 0-based;
// -1 means "no line on this side" (gap).
type Row struct {
	Kind      RowKind
	LeftNo    int
	RightNo   int
	LeftText  string
	RightText string
}

// Hunk is a change block with surrounding context, like `diff -u`.
type Hunk struct {
	LeftStart  int // 0-based start in left file
	LeftCount  int
	RightStart int
	RightCount int
	RowStart   int // index into FileDiff.Rows
	RowEnd     int // exclusive
}

// FileDiff is the computed result for a file pair.
type FileDiff struct {
	LeftLines  []string
	RightLines []string
	Rows       []Row
	Hunks      []Hunk
	Added      int
	Removed    int
	Equal      int
	IsBinary   bool
	Truncated  bool
	Rel        string
	// SameBytes reports that both files were read in full and are
	// byte-for-byte identical. Only LoadPair sets it; Compute leaves
	// it false (unknown).
	SameBytes bool
	// LineEndingOnly reports that the pair differs only in line
	// endings (CRLF vs LF), which SplitLines normalizes away: the
	// aligned rows show no hunks even though the bytes differ.
	// Only LoadPair sets it; Compute leaves it false.
	LineEndingOnly bool
}

// Header returns a `diff -u`-style hunk header (1-based).
func (h Hunk) Header() string {
	return " @@ -" + itoa(h.LeftStart+1) + "," + itoa(h.LeftCount) +
		" +" + itoa(h.RightStart+1) + "," + itoa(h.RightCount) + " @@"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// normalizeLine collapses whitespace for comparison when ignoring it.
func normalizeLine(s string, ignoreWS bool) string {
	if !ignoreWS {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}

// SplitLines splits text into lines, normalizing CRLF.
func SplitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// Compute aligns left/right lines and groups hunks with context lines.
func Compute(leftLines, rightLines []string, ignoreWS bool, context int) *FileDiff {
	if context < 0 {
		context = 0
	}
	fd := &FileDiff{LeftLines: leftLines, RightLines: rightLines}
	normA := make([]string, len(leftLines))
	for i, l := range leftLines {
		normA[i] = normalizeLine(l, ignoreWS)
	}
	normB := make([]string, len(rightLines))
	for i, l := range rightLines {
		normB[i] = normalizeLine(l, ignoreWS)
	}

	var script []op
	if len(normA)+len(normB) > MaxLinesMyers {
		script = fallbackScript(normA, normB)
	} else {
		script = myers(normA, normB)
	}
	fd.Rows = buildRows(leftLines, rightLines, script)
	for i := range fd.Rows {
		switch fd.Rows[i].Kind {
		case RowEqual:
			fd.Equal++
		case RowInsert:
			fd.Added++
		case RowDelete:
			fd.Removed++
		case RowChange:
			fd.Added++
			fd.Removed++
		}
	}
	fd.Hunks = buildHunks(fd.Rows, context)
	return fd
}

type opKind int

const (
	opEqual opKind = iota
	opDel
	opIns
)

type op struct {
	kind       opKind
	aIdx, bIdx int
}

// fallbackScript aligns common prefix/suffix and marks the middle changed.
func fallbackScript(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var script []op
	for i := 0; i < pre; i++ {
		script = append(script, op{opEqual, i, i})
	}
	for i := pre; i < len(a)-suf; i++ {
		script = append(script, op{opDel, i, -1})
	}
	for j := pre; j < len(b)-suf; j++ {
		script = append(script, op{opIns, -1, j})
	}
	for k := 0; k < suf; k++ {
		script = append(script, op{opEqual, len(a) - suf + k, len(b) - suf + k})
	}
	return script
}

// myers computes the shortest edit script.
func myers(a, b []string) []op {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	// Fast paths.
	if n == 0 {
		s := make([]op, m)
		for j := range b {
			s[j] = op{opIns, -1, j}
		}
		return s
	}
	if m == 0 {
		s := make([]op, n)
		for i := range a {
			s[i] = op{opDel, i, -1}
		}
		return s
	}
	max := n + m
	v := map[int]int{1: 0}
	var trace []map[int]int
	found := false
outer:
	for d := 0; d <= max; d++ {
		cur := make(map[int]int, 2*d+1)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1] < v[k+1]) {
				x = v[k+1]
			} else {
				x = v[k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[k] = x
			cur[k] = x
			if x >= n && y >= m {
				trace = append(trace, cur)
				found = true
				break outer
			}
		}
		trace = append(trace, cur)
		// Safety valve: should not happen given max bound, but guard.
		if d > MaxLinesMyers*2 {
			return fallbackScript(a, b)
		}
	}
	if !found {
		return fallbackScript(a, b)
	}
	// Backtrack.
	var rev []op
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		prev := trace[d-1]
		k := x - y
		var prevK int
		if k == -d || (k != d && prev[k-1] < prev[k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := prev[prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, op{opEqual, x - 1, y - 1})
			x--
			y--
		}
		if x == prevX {
			rev = append(rev, op{opIns, -1, y - 1})
			y--
		} else {
			rev = append(rev, op{opDel, x - 1, -1})
			x--
		}
	}
	for x > 0 && y > 0 {
		rev = append(rev, op{opEqual, x - 1, y - 1})
		x--
		y--
	}
	// Reverse.
	script := make([]op, len(rev))
	for i, o := range rev {
		script[len(rev)-1-i] = o
	}
	return script
}

// buildRows merges Del/Ins runs into Change pairs with gaps padded.
func buildRows(a, b []string, script []op) []Row {
	var rows []Row
	i := 0
	for i < len(script) {
		if script[i].kind == opEqual {
			rows = append(rows, Row{Kind: RowEqual, LeftNo: script[i].aIdx, RightNo: script[i].bIdx, LeftText: a[script[i].aIdx], RightText: b[script[i].bIdx]})
			i++
			continue
		}
		var dels, inss []op
		for i < len(script) && script[i].kind != opEqual {
			if script[i].kind == opDel {
				dels = append(dels, script[i])
			} else {
				inss = append(inss, script[i])
			}
			i++
		}
		pairs := len(dels)
		if len(inss) > pairs {
			pairs = len(inss)
		}
		for k := 0; k < pairs; k++ {
			r := Row{Kind: RowChange, LeftNo: -1, RightNo: -1}
			if k < len(dels) {
				r.LeftNo = dels[k].aIdx
				r.LeftText = a[dels[k].aIdx]
			}
			if k < len(inss) {
				r.RightNo = inss[k].bIdx
				r.RightText = b[inss[k].bIdx]
			}
			if r.LeftNo == -1 {
				r.Kind = RowInsert
			} else if r.RightNo == -1 {
				r.Kind = RowDelete
			}
			rows = append(rows, r)
		}
	}
	return rows
}

func buildHunks(rows []Row, context int) []Hunk {
	changed := make([]bool, len(rows))
	for i, r := range rows {
		changed[i] = r.Kind != RowEqual
	}
	// Expand each change by context, merge overlaps.
	type span struct{ s, e int }
	var spans []span
	for i := range rows {
		if !changed[i] {
			continue
		}
		s := i - context
		if s < 0 {
			s = 0
		}
		e := i + context + 1
		if e > len(rows) {
			e = len(rows)
		}
		if len(spans) > 0 && s <= spans[len(spans)-1].e {
			if e > spans[len(spans)-1].e {
				spans[len(spans)-1].e = e
			}
		} else {
			spans = append(spans, span{s, e})
		}
	}
	var hunks []Hunk
	for _, sp := range spans {
		h := Hunk{RowStart: sp.s, RowEnd: sp.e, LeftStart: -1, RightStart: -1}
		for i := sp.s; i < sp.e; i++ {
			r := rows[i]
			if r.LeftNo >= 0 {
				if h.LeftStart < 0 {
					h.LeftStart = r.LeftNo
				}
				h.LeftCount++
			}
			if r.RightNo >= 0 {
				if h.RightStart < 0 {
					h.RightStart = r.RightNo
				}
				h.RightCount++
			}
		}
		if h.LeftStart < 0 {
			h.LeftStart = 0
		}
		if h.RightStart < 0 {
			h.RightStart = 0
		}
		hunks = append(hunks, h)
	}
	return hunks
}

// HighlightPair finds the common prefix/suffix (rune-wise) between two
// changed lines so the TUI can emphasize the middle, vimdiff-style.
// Returns prefix rune count and suffix rune counts.
func HighlightPair(a, b string) (prefix, suffixA, suffixB int) {
	ar := []rune(a)
	br := []rune(b)
	for prefix < len(ar) && prefix < len(br) && ar[prefix] == br[prefix] {
		prefix++
	}
	for suffixA < len(ar)-prefix && suffixB < len(br)-prefix &&
		ar[len(ar)-1-suffixA] == br[len(br)-1-suffixB] {
		suffixA++
		suffixB++
	}
	return prefix, suffixA, suffixB
}

// LoadPair reads two files (with size cap + binary sniff) and diffs them.
func LoadPair(rel, leftPath, rightPath string, ignoreWS bool, context int) (*FileDiff, error) {
	lt, lb, ltrunc, err := readCapped(leftPath)
	if err != nil {
		return nil, err
	}
	rt, rb, rtrunc, err := readCapped(rightPath)
	if err != nil {
		return nil, err
	}
	binary := compare.IsBinary(lb) || compare.IsBinary(rb)
	fd := Compute(SplitLines(lt), SplitLines(rt), ignoreWS, context)
	fd.IsBinary = binary
	fd.Truncated = ltrunc || rtrunc
	fd.Rel = rel
	// Relate the line view back to the raw bytes (only meaningful
	// when both sides were read in full): same bytes prove the
	// browser's fast-path ≠ was a false positive, while equal
	// lines over differing bytes mean a line-ending-only pair the
	// line view cannot display.
	if !fd.Truncated {
		fd.SameBytes = lt == rt
		if !fd.SameBytes {
			normL := strings.ReplaceAll(lt, "\r\n", "\n")
			normR := strings.ReplaceAll(rt, "\r\n", "\n")
			fd.LineEndingOnly = normL == normR
		}
	}
	_ = rb
	return fd, nil
}

func readCapped(p string) (text string, head []byte, truncated bool, err error) {
	f, err := os.Open(p)
	if err != nil {
		return "", nil, false, err
	}
	defer f.Close()
	buf := make([]byte, MaxFileBytes+1)
	// ReadFull (not a single Read, which may return short counts) so
	// the text, Truncated, and SameBytes signals describe the whole file.
	n, err := io.ReadFull(f, buf)
	switch err {
	case nil:
		// Buffer filled: the file is larger than the cap.
		truncated = true
		n = MaxFileBytes
	case io.EOF:
		// Empty file.
		return "", buf[:0], false, nil
	case io.ErrUnexpectedEOF:
		// File smaller than the cap: n holds the full content.
		err = nil
	default:
		return "", nil, false, err
	}
	head = buf[:min(n, 8192)]
	text = string(buf[:n])
	return text, head, truncated, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
