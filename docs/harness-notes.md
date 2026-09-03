# Harness notes

Scratch notes for the [code]smith diff-viewer harness. Nothing here affects the
generator, the build scripts, or CI.

## What this PR exercises

| Diff shape        | Where                                    |
| ----------------- | ---------------------------------------- |
| One-line comments | 10 files from the first round            |
| Added file (Go)   | `tools/gen/harness.go`                   |
| Added file (md)   | `docs/harness-notes.md`                  |
| Added file (sh)   | `scripts/harness-check.sh`               |
| Deleted file      | `.churn`                                 |
| Multi-line hunks  | `README.md`, `Justfile`, `.gitignore`    |

## Not a real feature

Delete the branch when the harness work is done.
