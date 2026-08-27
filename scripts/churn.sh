#!/usr/bin/env bash
# Pushes an empty-ish commit to main every INTERVAL seconds to trigger the
# build workflow, simulating a steady stream of PRs. Ctrl-C to stop.
set -euo pipefail

INTERVAL="${INTERVAL:-30}"
BRANCH="${BRANCH:-main}"
i=0
while true; do
  i=$((i + 1))
  date -u +%Y-%m-%dT%H:%M:%SZ > .churn
  git add .churn
  git commit -q -m "churn: trigger build #$i"
  git push -q origin "$BRANCH"
  echo "[$(date +%H:%M:%S)] pushed churn commit #$i"
  sleep "$INTERVAL"
done
