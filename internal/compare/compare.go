// Package compare implements the recursive directory comparison engine.
//
// Fast default: entries are compared by type, then size, then mtime.
// Content hashing (SHA-256) is opt-in per scan (Options.HashOnScan) or
// on demand via HashesEqual / VerifyEntry, mirroring Beyond Compare and
// Meld behaviour where timestamp+size is the fast path and full content
// comparison is explicit.
package compare

import (
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// State describes how one relative path compares across both trees.
type State string

const (
	StateIdentical State = "identical"
	StateDifferent State = "different"
	StateOnlyLeft  State = "only-left"
	StateOnlyRight State = "only-right"
	StateSkipped   State = "skipped"
	StateError     State = "error"
)

// Symbol returns the centre-gutter marker used by the browser view,
// inspired by Beyond Compare / Meld folder comparison.
func (s State) Symbol() string {
	switch s {
	case StateIdentical:
		return "="
	case StateDifferent:
		return "≠"
	case StateOnlyLeft:
		return "<"
	case StateOnlyRight:
		return ">"
	case StateSkipped:
		return "-"
	default:
		return "?"
	}
}

// Options controls Scan behaviour.
type Options struct {
	// RespectMtime, when true (default), requires equal size AND equal
	// mtime (truncated to seconds) for files to count as identical on
	// the fast path. When false (--no-mtime), size equality suffices.
	RespectMtime bool
	// HashOnScan forces SHA-256 content comparison for same-size files.
	HashOnScan bool
	// Ignore holds shell patterns (path.Match syntax) matched against
	// the slash-separated relative path and its basename. A pattern
	// ending in "/*" also matches the directory prefix itself.
	Ignore []string
}

// DefaultOptions returns the fast default: size+mtime, no hashing.
func DefaultOptions() Options {
	return Options{RespectMtime: true}
}

// FileMeta describes one side of an entry.
type FileMeta struct {
	AbsPath    string
	IsDir      bool
	IsSymlink  bool
	LinkTarget string
	Size       int64
	ModTime    time.Time
	Mode       fs.FileMode
}

// Entry is one row of the folder comparison.
type Entry struct {
	Rel   string
	Left  *FileMeta
	Right *FileMeta
	State State
	Note  string
}

// IsDir reports whether the entry is a directory on either side.
func (e Entry) IsDir() bool {
	return (e.Left != nil && e.Left.IsDir) || (e.Right != nil && e.Right.IsDir)
}

// NeedsContentCheck reports whether the fast path could only flag the
// entry via mtime: a regular file pair with equal sizes whose contents
// were never read. Only a content check (hash, diff open, auto-verify)
// can settle whether it truly differs.
func (e Entry) NeedsContentCheck() bool {
	if e.State != StateDifferent {
		return false
	}
	if e.Left == nil || e.Right == nil {
		return false
	}
	if e.Left.IsDir || e.Right.IsDir || e.Left.IsSymlink || e.Right.IsSymlink {
		return false
	}
	if e.Left.Size < 0 || e.Right.Size < 0 {
		return false
	}
	return e.Left.Size == e.Right.Size
}

// Summary aggregates counts over a scan.
type Summary struct {
	Total     int
	Identical int
	Different int
	OnlyLeft  int
	OnlyRight int
	Skipped   int
	Errors    int
}

// Scan recursively walks leftRoot and rightRoot and compares them.
// Roots must exist and be directories.
func Scan(leftRoot, rightRoot string, opts Options) ([]Entry, Summary, error) {
	leftAbs, err := filepath.Abs(leftRoot)
	if err != nil {
		return nil, Summary{}, err
	}
	rightAbs, err := filepath.Abs(rightRoot)
	if err != nil {
		return nil, Summary{}, err
	}
	for _, r := range []string{leftAbs, rightAbs} {
		fi, err := os.Stat(r)
		if err != nil {
			return nil, Summary{}, err
		}
		if !fi.IsDir() {
			return nil, Summary{}, &fs.PathError{Op: "scan", Path: r, Err: fs.ErrInvalid}
		}
	}

	leftMap, err := walk(leftAbs, opts)
	if err != nil {
		return nil, Summary{}, err
	}
	rightMap, err := walk(rightAbs, opts)
	if err != nil {
		return nil, Summary{}, err
	}

	union := make(map[string]struct{}, len(leftMap)+len(rightMap))
	for k := range leftMap {
		union[k] = struct{}{}
	}
	for k := range rightMap {
		union[k] = struct{}{}
	}

	entries := make([]Entry, 0, len(union))
	var sum Summary
	for rel := range union {
		l := leftMap[rel]
		r := rightMap[rel]
		e := classify(rel, l, r, opts)
		entries = append(entries, e)
		sum.Total++
		switch e.State {
		case StateIdentical:
			sum.Identical++
		case StateDifferent:
			sum.Different++
		case StateOnlyLeft:
			sum.OnlyLeft++
		case StateOnlyRight:
			sum.OnlyRight++
		case StateSkipped:
			sum.Skipped++
		case StateError:
			sum.Errors++
		}
	}
	SortByName(entries)
	return entries, sum, nil
}

func walk(root string, opts Options) (map[string]*FileMeta, error) {
	out := map[string]*FileMeta{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return err
			}
			out[rel] = nil // placeholder; classified as error below
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // skip root itself
		}
		rel = filepath.ToSlash(rel)
		if ignored(rel, opts.Ignore) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			out[rel] = nil
			return nil
		}
		// Skip sockets, fifos, devices: record as skipped.
		if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
			out[rel] = &FileMeta{AbsPath: p, Size: -1, ModTime: info.ModTime(), Mode: info.Mode()}
			return nil
		}
		meta := &FileMeta{
			AbsPath: p,
			IsDir:   d.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Mode:    info.Mode(),
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			meta.IsSymlink = true
			target, err := os.Readlink(p)
			if err == nil {
				meta.LinkTarget = target
			}
			// A symlink to a dir still reports via Lstat as symlink;
			// do not descend into it.
			if d.IsDir() {
				meta.IsDir = false
			}
		}
		// Do not descend into symlinked dirs (WalkDir does not follow anyway).
		out[rel] = meta
		return nil
	})
	return out, err
}

func classify(rel string, l, r *FileMeta, opts Options) Entry {
	e := Entry{Rel: rel, Left: l, Right: r}
	// I/O error placeholder.
	if (l == nil) == false && l.AbsPath == "" {
		e.State = StateError
		e.Note = "cannot stat left"
		return e
	}
	if r != nil && r.AbsPath == "" {
		e.State = StateError
		e.Note = "cannot stat right"
		return e
	}
	if l == nil {
		e.State = StateOnlyRight
		return e
	}
	if r == nil {
		e.State = StateOnlyLeft
		return e
	}
	// Non-regular special files.
	if l.Size < 0 || r.Size < 0 {
		e.State = StateSkipped
		e.Note = "special file (socket/device) skipped"
		return e
	}
	// Symlink handling: compare link targets, never follow.
	if l.IsSymlink || r.IsSymlink {
		if l.IsSymlink && r.IsSymlink {
			if l.LinkTarget == r.LinkTarget {
				e.State = StateIdentical
				e.Note = "symlink"
			} else {
				e.State = StateDifferent
				e.Note = "symlink targets differ"
			}
			return e
		}
		e.State = StateDifferent
		e.Note = "type mismatch (symlink vs file/dir)"
		return e
	}
	if l.IsDir || r.IsDir {
		if l.IsDir && r.IsDir {
			e.State = StateIdentical
			e.Note = "directory"
			return e
		}
		e.State = StateDifferent
		e.Note = "type mismatch (dir vs file)"
		return e
	}
	// Both regular files: fast path size first.
	if l.Size != r.Size {
		e.State = StateDifferent
		e.Note = "sizes differ"
		return e
	}
	if opts.HashOnScan {
		same, err := HashesEqual(l.AbsPath, r.AbsPath)
		if err != nil {
			e.State = StateError
			e.Note = "hash error: " + err.Error()
			return e
		}
		if same {
			e.State = StateIdentical
		} else {
			e.State = StateDifferent
			e.Note = "content differs (hash)"
		}
		return e
	}
	if opts.RespectMtime {
		if sameModTime(l.ModTime, r.ModTime) {
			e.State = StateIdentical
		} else {
			// Same size but mtime differs: flag as different like
			// Beyond Compare's timestamp+size rule, but hint that
			// content verification (H) can confirm.
			e.State = StateDifferent
			e.Note = "same size, mtime differs (H verifies content)"
		}
		return e
	}
	e.State = StateIdentical
	return e
}

func sameModTime(a, b time.Time) bool {
	// Truncate to seconds: FAT/ext timestamp granularity and copy
	// tools often lose sub-second precision.
	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}

// HashesEqual compares two regular files by SHA-256.
func HashesEqual(leftPath, rightPath string) (bool, error) {
	hl, err := FileHash(leftPath)
	if err != nil {
		return false, err
	}
	hr, err := FileHash(rightPath)
	if err != nil {
		return false, err
	}
	return hl == hr, nil
}

// FileHash returns the hex SHA-256 of a file.
func FileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return stringOrHex(h.Sum(nil)), nil
}

func stringOrHex(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out)
}

// VerifyEntry re-checks a same-size pair by content hash. It returns
// true when contents are identical. Non-file pairs return an error.
func VerifyEntry(e Entry) (bool, error) {
	if e.Left == nil || e.Right == nil {
		return false, io.ErrUnexpectedEOF
	}
	if e.Left.IsDir || e.Right.IsDir || e.Left.IsSymlink || e.Right.IsSymlink {
		return false, io.ErrUnexpectedEOF
	}
	return HashesEqual(e.Left.AbsPath, e.Right.AbsPath)
}

// ignored reports whether rel matches any ignore pattern.
func ignored(rel string, patterns []string) bool {
	base := path.Base(rel)
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if ok, _ := path.Match(pat, rel); ok {
			return true
		}
		if ok, _ := path.Match(pat, base); ok {
			return true
		}
		// "dir/*" also ignores "dir" itself.
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "/*")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
		}
		// Bare directory name ignores the whole subtree.
		if !strings.Contains(pat, "/") && !strings.Contains(pat, "*") {
			if rel == pat || strings.HasPrefix(rel, pat+"/") {
				return true
			}
		}
	}
	return false
}

// SortByName sorts entries by relative path.
func SortByName(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })
}

// IsBinaryPath sniffs the first 8 KiB for a NUL byte.
func IsBinaryPath(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	return IsBinary(buf[:n]), nil
}

// IsBinary reports whether data looks binary (contains NUL).
func IsBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
