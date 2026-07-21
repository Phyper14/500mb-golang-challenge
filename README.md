# 500MB Club Challenge — Go implementation

Reference/competitive implementation for **[The 500MB Club Challenge](https://github.com/The-500MB-Club/the_500mb_club_challenge)**: a telemetry ingestion + query API for a fleet of delivery/mobility devices, running as **3 API replicas + 1 round-robin load balancer + Redis**, with the entire stack capped at **2 CPUs / 500 MB RAM**.

Built with **Go's standard library only** for the HTTP layer (`net/http` + Go 1.22+ method+path routing — no framework), a **Redis sorted-set** storage engine with a **binary point codec** (no JSON on the hot path), and **zero external HTTP dependencies at runtime** beyond `go-redis`.

## Why this design

The challenge's efficiency (32%) and capacity (27%) dimensions reward using the smallest possible slice of the 2 CPU / 500 MB budget while sustaining the highest RPS within SLOs. Every design decision here optimizes for that:

| Decision | Rationale |
|---|---|
| `net/http` stdlib router, no framework | Zero extra allocations/indirection per request; smallest possible binary and RSS baseline. |
| Static binary, `distroless/static` runtime image | No libc, no shell, ~12 MB image; nothing to patch, nothing to exploit. |
| Redis sorted set (`ZADD`/`ZRANGE`) per device | O(log N) writes, O(log N + M) range reads; a single data structure serves both the time-window query and the anomaly's "last 256 points" read. |
| Binary point codec (fixed 53 bytes/point) instead of JSON | No `encoding/json` reflection overhead on the write path — the single largest share of traffic per the steady-state mix (60% `POST /telemetry`). |
| Bounded per-device history (`MAX_POINTS_PER_DEVICE`, default 5000) | Keeps Redis memory bounded under `capacity`/`endurance` load without an unbounded working set. |
| `go.uber.org/zap` for logging | Structured, allocation-conscious logging; avoids `fmt`-based logging overhead under load. |
| Dedicated Prometheus registry, route-pattern labels | `/metrics` cardinality is bounded by the fixed route set (never by device id), keeping scrape cost flat regardless of traffic. |

## Contract

Implements the full [OpenAPI contract](https://github.com/The-500MB-Club/the_500mb_club_challenge/blob/master/openapi.yaml) of the challenge:

- `GET  /healthz` — liveness, never touches storage.
- `GET  /readyz` — readiness, pings Redis.
- `GET  /metrics` — Prometheus exposition.
- `POST /devices/{id}/telemetry` — single point ingest.
- `POST /devices/{id}/telemetry/batch` — 1–100 point batch ingest.
- `GET  /devices/{id}/telemetry?from=&to=&limit=&cursor=` — windowed query, cursor pagination.
- `GET  /devices/{id}/anomaly` — z-score of acceleration magnitude over the last 256 points, **recomputed on every call** (no caching, per contract).

Every response carries `X-Instance-Id`, graceful shutdown drains in-flight requests within 10s of `SIGTERM`.

## Project layout

```
cmd/api/                    entry point: config/logger/store/server wiring, graceful shutdown
internal/domain/            pure business rules: point validation, z-score anomaly detection
internal/storage/           storage.Store interface (engine-agnostic contract)
internal/storage/rediskv/   Redis implementation: binary codec + sorted-set store
internal/storagetest/       hand-written in-memory fake of storage.Store, for handler unit tests
internal/httpapi/           HTTP handlers + Prometheus instrumentation middleware
internal/metrics/           Prometheus registry, HTTP metric definitions
internal/config/            environment-variable configuration loader
deploy/nginx/                nginx.conf — strict round-robin load balancer
docker-compose.yml           full stack: 3 API + nginx + redis, 2 CPU / 500 MB aggregate
test/                         official k6 smoke.js / test.js scenarios (vendored from the challenge repo)
scripts/setup-branches.sh    splits `main` (full source) from `implementation` (compose + me.json only)
```

## Running locally

Requirements: Go 1.25+, Docker with Buildx, `staticcheck` (for `make lint`).

```bash
make test          # unit + integration tests (miniredis, no Docker needed)
make test-cover     # same, with coverage breakdown
make lint            # go vet + staticcheck
make compose-up      # builds the image and starts the full stack on :8080
make smoke           # runs the official k6 smoke test against it
make test-load       # runs the official k6 steady-state (100 RPS/1min) scenario
make compose-down    # tears the stack down
```

`make test-race` requires `cgo`/a C compiler and is intended for CI, where it's run automatically.

### Verified locally (dev machine, not the Pi)

- `smoke.js`: **100% checks passing** (45/45), round-robin distribution confirmed even across replicas (10/10/10 on 30 probes).
- `test.js` (steady, 100 RPS / 1min, realistic mix): **0% `http_req_failed`**; p99 per operation — `post` 0.95 ms, `batch` 1.78 ms, `range` 1.19 ms, `anomaly` 1.0 ms (targets are 8/25/15/25 ms).
- Aggregate RSS with the full stack under that load: **~38 MB** (3× API ~7 MB, nginx ~4 MB, Redis ~12 MB) — well inside the 500 MB budget, with headroom for `capacity`/`endurance` scenarios.
- `docker-compose.yml` passes the challenge's own `scripts/harden_compose.py` + `scripts/validate_compose.py` gate with **0 FAIL, 0 WARN** (2.00/2.0 CPU, 380/500 MiB declared).

Numbers on the actual Raspberry Pi 5 benchmark hardware will differ; this is a development-machine sanity check, not the official score.

## Configuration (environment variables)

| Variable | Default | Description |
|---|---|---|
| `LISTEN_ADDR` | `:8000` | HTTP bind address. |
| `INSTANCE_ID` | OS hostname | Value returned in `X-Instance-Id`. |
| `REDIS_ADDR` | `localhost:6379` | Redis `host:port`. |
| `REDIS_POOL_SIZE` | `10` | Max Redis connections per process. |
| `MAX_POINTS_PER_DEVICE` | `5000` | Retention cap per device (oldest points trimmed first). |
| `SHUTDOWN_TIMEOUT` | `10s` | Grace period for in-flight requests on `SIGTERM`. |

## Submitting to the challenge

This repository is laid out to satisfy [docs/pt-br/submitting.md](https://github.com/The-500MB-Club/the_500mb_club_challenge/blob/master/docs/pt-br/submitting.md) directly:

1. `git init`, commit everything on `main`.
2. Run `./scripts/setup-branches.sh` — it derives an `implementation` branch containing **only** `docker-compose.yml`, `deploy/`, and `me.json`, per the rule that the validator only clones that branch.
3. Push both branches, publish the image (`git tag vX.Y.Z && git push --tags` triggers `.github/workflows/release.yml`, which builds and pushes a **native arm64** manifest to GHCR — no QEMU emulation at runtime, only at cross-compile time).
4. Update `docker-compose.yml`'s `image:` to point at the published tag before pushing `implementation`.
5. Fork `The-500MB-Club/the_500mb_club_challenge`, add `submissions/<your-github-username>.json`, open the PR.

## License

MIT — see [LICENSE](LICENSE).
