#!/usr/bin/env bash
# Harness test: formats and vets only the generator, with no network access.
# Exists to give the diff viewer an added shell file to render.
set -euo pipefail

unformatted="$(gofmt -l tools)"
if [[ -n "$unformatted" ]]; then
  echo "unformatted files:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go vet ./tools/...
echo "harness check ok"
