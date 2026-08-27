# go-apps-demo

Load test for [Blacksmith Sticky Disk](https://docs.blacksmith.sh/) cache churn:
simulates many developers opening PRs against many Go services.

- `tools/gen` — generates N self-contained Go app modules (`apps/app-NNN`). Each app
  picks 5–12 deps from a pool of ~45 real modules (each with several pinned versions,
  including heavy ones like aws-sdk-go, client-go, GCS, docker, terraform-plugin-sdk)
  and gets 12 internal packages × 6 files × 40 generated functions. Changing `-seed`
  changes every app's source, dep set, and dep versions, simulating a new round of
  PRs — churning both `GOMODCACHE` (downloads) and `GOCACHE` (compiled objects).
  Tune with `-pkgs`, `-files`, `-funcs`; `-only N` emits a single app.
- `scripts/build-all.sh` — builds/vets/tests every app in series with timing and
  `GOCACHE`/`GOMODCACHE` size reporting.
- `.github/workflows/build.yml` — one job per push: mounts the sticky disk at
  `/mnt/go-cache`, generates a fresh app (seed = run number), and builds it.
- `scripts/churn.sh` / `just churn` — pushes a commit to `main` every 30s (`INTERVAL`)
  so CI runs continuously, simulating a stream of PRs.

Local:

```sh
mise install
just gen 10 1   # generate 10 apps with seed 1
just build      # build them all in series
just all 100 42 # or both at once
```
