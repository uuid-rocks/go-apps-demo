set shell := ["bash", "-euo", "pipefail", "-c"]

n := "1024"
seed := "1"

default:
    @just --list

# Generate N apps under apps/ (runs go mod tidy in each)
gen n=n seed=seed:
    go run ./tools/gen -n {{n}} -seed {{seed}} -out apps

# Generate without go mod tidy (offline, no go.sum)
gen-fast n=n seed=seed:
    go run ./tools/gen -n {{n}} -seed {{seed}} -out apps -tidy=false

# Build/vet/test every generated app in series with timing
build:
    ./scripts/build-all.sh

# Generate then build
all n=n seed=seed: (gen n seed) build

# Remove generated apps
clean:
    rm -rf apps

# Lint the generator itself
check:
    gofmt -l tools && go vet ./tools/...

# Generate a single app by index (what each CI matrix job does)
gen-one index n=n seed=seed:
    go run ./tools/gen -n {{n}} -seed {{seed}} -only {{index}} -out apps
