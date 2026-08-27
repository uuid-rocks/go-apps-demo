#!/usr/bin/env bash
# Pushes a commit to main every INTERVAL seconds, COUNT times, to trigger the
# build workflow — simulating a steady stream of PRs. Ctrl-C to stop early.
#
#   ./scripts/churn.sh [count] [interval]
#   COUNT=200 INTERVAL=15 ./scripts/churn.sh
set -euo pipefail

COUNT="${1:-${COUNT:-100}}"
INTERVAL="${2:-${INTERVAL:-30}}"
BRANCH="${BRANCH:-main}"

echo "pushing $COUNT commits to $BRANCH, one every ${INTERVAL}s"
for ((i = 1; i <= COUNT; i++)); do
  date -u +%Y-%m-%dT%H:%M:%SZ > .churn
  git add .churn
  git commit -q -m "churn: trigger build $i/$COUNT"
  git push -q origin "$BRANCH"
  echo "[$(date +%H:%M:%S)] pushed $i/$COUNT"
  ((i < COUNT)) && sleep "$INTERVAL"
done
echo "done"
