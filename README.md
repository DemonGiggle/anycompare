# anycompare

Read-only TUI directory comparison for Linux (`amd64` + `arm64`), inspired by
**Beyond Compare**, **Meld**, **WinMerge** / **KDiff3** folder-compare views,
with file diffs rendered **vimdiff-style** (synchronized side-by-side panes).

```
anycompare [flags] <source-dir> <dest-dir>
```

## What you get

- **Browser first:** typing `anycompare <source> <dest>` opens the folder
  browser filtered to differences (`=` identical rows hidden);
  `f` cycles filters (differences/all/different/only-left/only-right).
  `enter` opens the selected file in a vimdiff-style side-by-side diff.
- **Browser:** synchronized `SOURCE` and `DEST` panes, each with filenames
  and sizes, plus a centre status gutter. Files missing from one side leave
  that pane's row blank; `›` marks the selection even without colors.
  Gutter symbols:
  `=` identical, `≠` different, `<` only-left, `>` only-right
  (same idea as Beyond Compare / Meld folder comparison).
- **ENTER opens a vimdiff-style diff:** side-by-side panes scrolling together,
  change background plus intra-line highlight of the changed segment,
  hunk header (`@@ -a,b +c,d @@`), `]c` / `[c` (also `n`/`N`, `]`/`[`) hunk jumps,
  `d` toggles side-by-side vs unified (`diff -u`), `w` ignores whitespace,
  `+`/`-` changes context, `b` forces binary-as-text, `esc` returns to the
  browser with cursor/filter preserved.
- **Fast default comparison:** type → size → mtime (second granularity).
  Same-size but different-mtime files are flagged `≠`; opening the diff
  verifies content and clears the flag when the bytes match (or press
  `H` to verify by SHA-256, or scan with `--hash` for definitive results).
  CRLF-vs-LF pairs report a line-ending notice instead of a silent match.
- **Single static binary:** `CGO_ENABLED=0`, no runtime beyond the binary.

## Install

Prebuilt `linux-amd64` / `linux-arm64` tarballs live on GitHub Releases
(see `.goreleaser.yaml`), or build from source:

```sh
go install github.com/anycompare/anycompare/cmd/anycompare@latest
# or
make build          # bin/anycompare
make build-all      # dist/anycompare-linux-amd64, dist/anycompare-linux-arm64
```

## Usage

```sh
anycompare testdata/left testdata/right
anycompare --no-mtime ~/photos ~/backup/photos
anycompare --hash --ignore '*.log' --ignore '.git/*' ./a ./b
anycompare --summary --no-mtime ./a ./b   # plain text, no TUI (CI-friendly)
```

Browser keys: `j/k` or arrows navigate, `enter` diff, `f` filter
(all/different/only-left/only-right), `s` sort (name/size/mtime),
`H` hash-verify selection, `R` rescan, `Tab` pane focus, `?` help, `q` quit.

Diff keys: `j/k` scroll, `PgUp/PgDn` page, `g/G` top/bottom,
`n`/`N` or `]`/`[` or `]c`/`[c` next/prev hunk, `d` view mode,
`w` whitespace, `+`/`-` context, `b` binary-as-text, `esc`/`q` back.

## Notes / limits

- Read-only: no copy/sync/merge (unlike Beyond Compare). No action keys exist.
- Symlinks are compared by target string, never followed.
- Directories compare as containers; open a file to diff contents.
- Sockets/devices are listed as skipped; files over 10 MiB are truncated
  with a notice; diffs over ~20k lines use a prefix/suffix fallback.
- Timestamps truncate to seconds (copy tools often lose sub-second precision).

## Development

```sh
go vet ./...
go test ./...
go run ./cmd/anycompare --summary testdata/left testdata/right
make build-all
```
