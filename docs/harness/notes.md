# Harness notes (round four)

Scratch notes for the [code]smith diff-viewer harness. Nothing here affects the
generator, the build scripts, or CI.

## What this PR exercises

| Diff shape          | Where                                              |
| ------------------- | -------------------------------------------------- |
| One-line comments   | 10 files from the first round                       |
| Added file (Go)     | `tools/gen/harness.go`                              |
| Added file (md)     | `docs/harness/notes.md`                             |
| Added file (sh)     | `scripts/harness/check.sh`                          |
| Deleted file        | `.churn`                                            |
| Multi-line hunks    | `README.md`, `Justfile`, `.gitignore`               |
| Pure rename         | `docs/harness-notes.md` -> `docs/harness/notes.md`  |
| Rename + rewrite    | `scripts/harness-check.sh` -> `scripts/harness/check.sh` |
| Symlink             | `docs/latest-notes.md`                              |
| Binary              | `testdata/harness/pixel.png`                        |
| CRLF endings        | `testdata/harness/crlf.txt`                         |
| No trailing newline | `testdata/harness/no-trailing-newline.txt`          |
| Empty file          | `testdata/harness/empty.txt`                        |
| Very long line      | `testdata/harness/long-line.json`                   |
| Unicode / RTL       | `testdata/harness/unicode.txt`                      |
| Large added file    | `testdata/harness/bigtable.go` (600+ lines)         |
| Deep path           | `testdata/harness/deeply/nested/path/level5/`       |
| Distant hunks       | `tools/gen/main.go`                                 |

Everything under `testdata/` is inert: the go tool ignores `testdata`
directories, so none of it is compiled, vetted, or shipped.

## Not a real feature

Delete the branch when the harness work is done.
