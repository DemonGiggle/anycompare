# anycompare

[![ci](https://github.com/DemonGiggle/anycompare/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/DemonGiggle/anycompare/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/DemonGiggle/anycompare)](https://github.com/DemonGiggle/anycompare/releases)
[![go](https://img.shields.io/github/go-mod/go-version/DemonGiggle/anycompare)](https://go.dev/)
[![license](https://img.shields.io/github/license/DemonGiggle/anycompare)](LICENSE)

**Spot every difference between two directories — side by side, from your terminal.**

`anycompare` is a read-only TUI directory comparison for Linux
(`amd64` + `arm64`). Think Beyond Compare's folder view meets vimdiff: a
synchronized `SOURCE` / `DEST` browser for the big picture, and one keystroke
to drop into a synchronized side-by-side diff of any file. No setup, no
runtime, no risk to your files — a single static binary that only ever reads.

```sh
anycompare [flags] <source-dir> <dest-dir>
```

## Why anycompare

- **Answers “what changed?” in seconds.** The browser opens filtered to
  differences, and `enter` jumps straight into the diff. Backups, deploys,
  photo libraries — if it fits in two trees, you can audit it.
- **Fast without lying.** A type → size → mtime scan paints instantly, then
  a background content check quietly settles the ambiguous rows. Press `H`
  for an on-demand SHA-256 verdict on any file, or `--hash` for a fully
  definitive scan.
- **Diffs that read like vimdiff.** Synchronized scrolling, intra-line
  highlight of the changed segment, numbered hunk headers
  (`[1/2] @@ -a,b +c,d @@`) with `n`/`N` jumps, side-by-side ↔ unified
  (`d`), whitespace toggle (`w`), adjustable context (`+`/`-`).
- **Safe by design.** Read-only means there are no copy/sync/merge keys to
  fat-finger. Symlinks are compared, never followed; sockets and devices
  are skipped, not probed.
- **One static binary.** `CGO_ENABLED=0`, nothing to install, happy over SSH.

## Tour

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
  numbered hunk headers (`[1/2] @@ -a,b +c,d @@`),
  `]c` / `[c` (also `n`/`N`, `]`/`[`) hunk jumps,
  `d` toggles side-by-side vs unified (`diff -u`), `w` ignores whitespace,
  `+`/`-` changes context, `b` forces binary-as-text, `esc` returns to the
  browser with cursor/filter preserved.
- **Fast default comparison:** type → size → mtime (second granularity).
  Same-size but different-mtime files flag `≠` on the fast scan, then a
  background content check settles them (or press `H` to verify by
  SHA-256 immediately, or scan with `--hash` for definitive results).
  CRLF-vs-LF pairs report a line-ending notice instead of a silent match.
- **Single static binary:** `CGO_ENABLED=0`, no runtime beyond the binary.

## Install

Prebuilt `linux-amd64` / `linux-arm64` tarballs live on GitHub Releases
(see `.goreleaser.yaml`), or build from source:

```sh
go install github.com/DemonGiggle/anycompare/cmd/anycompare@latest
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
