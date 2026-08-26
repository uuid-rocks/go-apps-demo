#!/usr/bin/env bash
# Builds every generated app in series, printing per-app timing and
# Go cache sizes before/after. Intended to run on CI with GOCACHE and
# GOMODCACHE pointed at a persistent (sticky) disk.
set -euo pipefail

APPS_DIR="${APPS_DIR:-apps}"
GOCACHE_DIR="$(go env GOCACHE)"
GOMODCACHE_DIR="$(go env GOMODCACHE)"

du_h() { [ -d "$1" ] && du -sh "$1" 2>/dev/null | cut -f1 || echo "0"; }

echo "GOCACHE=$GOCACHE_DIR ($(du_h "$GOCACHE_DIR"))"
echo "GOMODCACHE=$GOMODCACHE_DIR ($(du_h "$GOMODCACHE_DIR"))"
echo

total_start=$(date +%s)
count=0
for dir in "$APPS_DIR"/*/; do
  name=$(basename "$dir")
  start=$(date +%s.%N)
  (cd "$dir" && go build -o /dev/null ./... && go vet ./... && go test -count=1 ./... >/dev/null)
  end=$(date +%s.%N)
  printf '%-10s %6.2fs\n' "$name" "$(echo "$end - $start" | bc)"
  count=$((count + 1))
done
total_end=$(date +%s)

echo
echo "built $count apps in $((total_end - total_start))s"
echo "GOCACHE    after: $(du_h "$GOCACHE_DIR")"
echo "GOMODCACHE after: $(du_h "$GOMODCACHE_DIR")"
