package diff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeIdentical(t *testing.T) {
	fd := Compute([]string{"a", "b"}, []string{"a", "b"}, false, 3)
	if fd.Added != 0 || fd.Removed != 0 || fd.Equal != 2 {
		t.Fatalf("identical stats %+v", fd)
	}
	if len(fd.Hunks) != 0 {
		t.Fatalf("identical should have no hunks, got %d", len(fd.Hunks))
	}
}

func TestComputeInsertAndHunks(t *testing.T) {
	fd := Compute([]string{"a", "b", "c"}, []string{"a", "X", "b", "c"}, false, 1)
	if len(fd.Hunks) != 1 {
		t.Fatalf("want 1 hunk, got %d", len(fd.Hunks))
	}
	if fd.Added != 1 {
		t.Fatalf("added=%d want 1", fd.Added)
	}
}

func TestComputeChangePairs(t *testing.T) {
	fd := Compute([]string{"hello world", "same"}, []string{"hello there", "same"}, false, 3)
	if len(fd.Rows) != 2 {
		t.Fatalf("rows=%d want 2", len(fd.Rows))
	}
	if fd.Rows[0].Kind != RowChange {
		t.Fatalf("row0 kind=%v want change", fd.Rows[0].Kind)
	}
	pre, _, _ := HighlightPair("hello world", "hello there")
	if pre != 6 {
		t.Fatalf("prefix=%d want 6", pre)
	}
}

func TestComputeIgnoreWhitespace(t *testing.T) {
	a := []string{"a  b", "c"}
	b := []string{"a b", "c"}
	plain := Compute(a, b, false, 3)
	if len(plain.Hunks) == 0 {
		t.Fatal("without ignore-ws should differ")
	}
	ignored := Compute(a, b, true, 3)
	if len(ignored.Hunks) != 0 {
		t.Fatal("with ignore-ws should be identical")
	}
}

func TestSplitLinesCRLF(t *testing.T) {
	lines := SplitLines("a\r\nb\r\n")
	if len(lines) != 3 { // trailing newline yields final empty element
		t.Fatalf("lines=%q", lines)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadPairSameBytes(t *testing.T) {
	l := writeTempFile(t, "a\nb\n")
	r := writeTempFile(t, "a\nb\n")
	fd, err := LoadPair("f.txt", l, r, false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Hunks) != 0 {
		t.Fatalf("identical files should have no hunks, got %d", len(fd.Hunks))
	}
	if !fd.SameBytes {
		t.Fatal("identical files should report SameBytes")
	}
	if fd.LineEndingOnly {
		t.Fatal("identical files should not report LineEndingOnly")
	}
}

func TestLoadPairLineEndingOnly(t *testing.T) {
	l := writeTempFile(t, "a\nb\n")
	r := writeTempFile(t, "a\r\nb\r\n")
	fd, err := LoadPair("f.txt", l, r, false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Hunks) != 0 {
		t.Fatalf("CRLF pair shows no line hunks, got %d", len(fd.Hunks))
	}
	if fd.SameBytes {
		t.Fatal("CRLF pair must not report SameBytes")
	}
	if !fd.LineEndingOnly {
		t.Fatal("CRLF pair should report LineEndingOnly")
	}
}

func TestLoadPairRealDiff(t *testing.T) {
	l := writeTempFile(t, "a\n")
	r := writeTempFile(t, "b\n")
	fd, err := LoadPair("f.txt", l, r, false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Hunks) == 0 {
		t.Fatal("changed files should have hunks")
	}
	if fd.SameBytes || fd.LineEndingOnly {
		t.Fatalf("changed files must not report SameBytes/LineEndingOnly: %+v", fd)
	}
}
