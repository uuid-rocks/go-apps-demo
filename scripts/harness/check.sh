#!/usr/bin/env bash
# Harness test: formats and vets only the generator, with no network access.
# Exists to give the diff viewer a renamed-and-rewritten shell file to render.
set -euo pipefail

fail() {
  echo "harness check failed: $*" >&2
  exit 1
}

unformatted="$(gofmt -l tools)"
if [[ -n "$unformatted" ]]; then
  echo "$unformatted" >&2
  fail "unformatted files above"
fi

go vet ./tools/... || fail "go vet reported problems"

# Fixture files are inert on purpose; catch it if one grows a build tag or
# otherwise starts getting picked up by the go tool.
if go list ./... 2>/dev/null | grep -q '/testdata/'; then
  fail "testdata packages leaked into the build"
fi

for script in scripts/*.sh scripts/harness/*.sh; do
  bash -n "$script" || fail "syntax error in $script"
done

echo "harness check ok (round four)"
