#!/usr/bin/env bash
# Pushes a commit to main, waits for its workflow run to finish (via gh), waits
# DELAY seconds, then pushes the next one — COUNT times. Simulates back-to-back PRs
# with no idle time between builds. Ctrl-C to stop early.
#
#   ./scripts/churn.sh [count]
set -euo pipefail

# Harness round 2: comment-only change.
COUNT="${1:-${COUNT:-100}}"
DELAY="${DELAY:-10}" # seconds to wait after a run finishes before pushing the next
BRANCH="${BRANCH:-main}"
WORKFLOW="${WORKFLOW:-build.yml}"

find_run() { # prints run id for a commit sha, or nothing
  gh run list --workflow "$WORKFLOW" --commit "$1" --limit 1 --json databaseId -q '.[0].databaseId'
}

echo "pushing $COUNT commits to $BRANCH, waiting for each run to finish"
for ((i = 1; i <= COUNT; i++)); do
  date -u +%Y-%m-%dT%H:%M:%SZ > .churn
  git add .churn
  git commit -q -m "churn: trigger build $i/$COUNT"
  git push -q origin "$BRANCH"
  sha=$(git rev-parse HEAD)
  echo "[$(date +%H:%M:%S)] pushed $i/$COUNT ($sha)"

  run_id=""
  for _ in $(seq 1 30); do
    run_id=$(find_run "$sha")
    [[ -n "$run_id" ]] && break
    sleep 2
  done
  if [[ -z "$run_id" ]]; then
    echo "  no run appeared for $sha after 60s; continuing"
    continue
  fi

  start=$(date +%s)
  gh run watch "$run_id" --exit-status >/dev/null 2>&1 && status=success || status=failure
  echo "  run $run_id $status in $(( $(date +%s) - start ))s"
  ((i < COUNT)) && sleep "$DELAY"
done
echo "done"
