package diff

import "testing"

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
