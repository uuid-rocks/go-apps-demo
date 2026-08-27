#!/usr/bin/env bash
# Builds every generated app, PARALLEL apps at a time (default: 4),
# printing per-app timing and Go cache sizes before/after. Intended to run on
# CI with GOCACHE and GOMODCACHE pointed at a persistent (sticky) disk.
set -euo pipefail

APPS_DIR="${APPS_DIR:-apps}"
PARALLEL="${PARALLEL:-4}"
GOCACHE_DIR="$(go env GOCACHE)"
GOMODCACHE_DIR="$(go env GOMODCACHE)"

du_h() { [ -d "$1" ] && du -sh "$1" 2>/dev/null | cut -f1 || echo "0"; }

echo "GOCACHE=$GOCACHE_DIR ($(du_h "$GOCACHE_DIR"))"
echo "GOMODCACHE=$GOMODCACHE_DIR ($(du_h "$GOMODCACHE_DIR"))"
echo "building $(ls -d "$APPS_DIR"/*/ | wc -l | tr -d ' ') apps, $PARALLEL at a time"
echo

build_one() {
  local dir=$1 name start end
  name=$(basename "$dir")
  start=$(date +%s.%N)
  (cd "$dir" && go build -o /dev/null ./... && go vet ./... && go test -count=1 ./... >/dev/null)
  end=$(date +%s.%N)
  printf '%-10s %6.2fs\n' "$name" "$(echo "$end - $start" | bc)"
}
export -f build_one

# TIMEFORMAT gives real/user/sys so CPU utilisation is visible in the log.
TIMEFORMAT=$'\nwall %Rs  user %Us  sys %Ss'
time {
  ls -d "$APPS_DIR"/*/ | xargs -P "$PARALLEL" -I{} bash -c 'build_one "$@"' _ {}
}

echo
echo "GOCACHE    after: $(du_h "$GOCACHE_DIR")"
echo "GOMODCACHE after: $(du_h "$GOMODCACHE_DIR")"
