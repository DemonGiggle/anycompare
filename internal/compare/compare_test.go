package compare

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(entries []Entry, rel string) *Entry {
	for i := range entries {
		if entries[i].Rel == rel {
			return &entries[i]
		}
	}
	return nil
}

func TestScanIdenticalDifferentOnlySides(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeFile(t, filepath.Join(left, "same.txt"), "hello")
	writeFile(t, filepath.Join(right, "same.txt"), "hello")
	writeFile(t, filepath.Join(left, "diff.txt"), "aaa")
	writeFile(t, filepath.Join(right, "diff.txt"), "bbb-different-size")
	writeFile(t, filepath.Join(left, "only-left.txt"), "x")
	writeFile(t, filepath.Join(right, "only-right.txt"), "y")
	writeFile(t, filepath.Join(left, "sub", "a.txt"), "1")
	writeFile(t, filepath.Join(right, "sub", "a.txt"), "1")

	// Force identical mtimes for same-size files so fast path is stable.
	now := time.Now().Truncate(time.Second)
	for _, p := range []string{filepath.Join(left, "same.txt"), filepath.Join(right, "same.txt"), filepath.Join(left, "sub", "a.txt"), filepath.Join(right, "sub", "a.txt")} {
		if err := os.Chtimes(p, now, now); err != nil {
			t.Fatal(err)
		}
	}

	entries, sum, err := Scan(left, right, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := find(entries, "same.txt"); got == nil || got.State != StateIdentical {
		t.Fatalf("same.txt = %+v, want identical", got)
	}
	if got := find(entries, "diff.txt"); got == nil || got.State != StateDifferent {
		t.Fatalf("diff.txt = %+v, want different", got)
	}
	if got := find(entries, "only-left.txt"); got == nil || got.State != StateOnlyLeft {
		t.Fatalf("only-left.txt = %+v", got)
	}
	if got := find(entries, "only-right.txt"); got == nil || got.State != StateOnlyRight {
		t.Fatalf("only-right.txt = %+v", got)
	}
	if sum.OnlyLeft != 1 || sum.OnlyRight != 1 || sum.Different < 1 || sum.Identical < 2 {
		t.Fatalf("unexpected summary %+v", sum)
	}
}

func TestScanMtimeRuleAndHashVerify(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeFile(t, filepath.Join(left, "f.txt"), "same-bytes")
	writeFile(t, filepath.Join(right, "f.txt"), "same-bytes")
	now := time.Now().Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(left, "f.txt"), now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(right, "f.txt"), now.Add(2*time.Hour), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	entries, _, err := Scan(left, right, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	got := find(entries, "f.txt")
	if got == nil || got.State != StateDifferent {
		t.Fatalf("mtime differs should flag different, got %+v", got)
	}
	same, err := VerifyEntry(*got)
	if err != nil || !same {
		t.Fatalf("hash verify should report identical content, same=%v err=%v", same, err)
	}

	entries2, _, err := Scan(left, right, Options{RespectMtime: false})
	if err != nil {
		t.Fatal(err)
	}
	if got2 := find(entries2, "f.txt"); got2 == nil || got2.State != StateIdentical {
		t.Fatalf("--no-mtime should be identical, got %+v", got2)
	}

	entries3, _, err := Scan(left, right, Options{RespectMtime: true, HashOnScan: true})
	if err != nil {
		t.Fatal(err)
	}
	if got3 := find(entries3, "f.txt"); got3 == nil || got3.State != StateIdentical {
		t.Fatalf("hash scan should resolve identical, got %+v", got3)
	}
}

func TestScanTypeMismatchAndSymlink(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	if err := os.MkdirAll(filepath.Join(left, "node"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(right, "node"), "file-not-dir")

	entries, _, err := Scan(left, right, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := find(entries, "node"); got == nil || got.State != StateDifferent {
		t.Fatalf("dir vs file should be different, got %+v", got)
	}

	writeFile(t, filepath.Join(left, "target.txt"), "t")
	if err := os.Symlink("target.txt", filepath.Join(left, "link")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if err := os.Symlink("other.txt", filepath.Join(right, "link")); err != nil {
		t.Fatal(err)
	}
	entries, _, err = Scan(left, right, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := find(entries, "link"); got == nil || got.State != StateDifferent {
		t.Fatalf("symlink targets differ should be different, got %+v", got)
	}
}

func TestScanIgnore(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	writeFile(t, filepath.Join(left, "keep.txt"), "a")
	writeFile(t, filepath.Join(right, "keep.txt"), "a")
	writeFile(t, filepath.Join(left, "skip.log"), "a")
	writeFile(t, filepath.Join(right, "skip.log"), "b")

	entries, _, err := Scan(left, right, Options{RespectMtime: false, Ignore: []string{"*.log"}})
	if err != nil {
		t.Fatal(err)
	}
	if find(entries, "skip.log") != nil {
		t.Fatal("skip.log should be ignored")
	}
	if find(entries, "keep.txt") == nil {
		t.Fatal("keep.txt should be present")
	}
}
