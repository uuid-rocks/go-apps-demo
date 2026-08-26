# go-apps-demo

Load test for [Blacksmith Sticky Disk](https://docs.blacksmith.sh/) cache churn:
simulates many developers opening PRs against many Go services.

- `tools/gen` — generates N self-contained Go app modules (`apps/app-NNN`) with a
  seeded-random mix of real third-party deps and generated internal packages.
  Changing `-seed` changes every app's source, simulating a new round of PRs.
- `scripts/build-all.sh` — builds/vets/tests every app in series with timing and
  `GOCACHE`/`GOMODCACHE` size reporting.
- `.github/workflows/build.yml` — mounts a sticky disk at `/mnt/go-cache`, generates
  apps with seed = run number, and builds them all.

Local:

```sh
mise install
just gen 10 1   # generate 10 apps with seed 1
just build      # build them all in series
just all 100 42 # or both at once
```
