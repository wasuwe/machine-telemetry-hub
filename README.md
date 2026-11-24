# Machine Telemetry Hub

A live OEE dashboard for a press line, written in Go. The machines are simulated.

**Demo:** https://machine-telemetry-hub.vercel.app

<!-- TODO: add a screenshot or GIF: docs/dashboard.gif -->

## What it does

The app simulates 12 stamping presses (80T to 400T). Each one cycles, makes good parts or scrap,
and now and then breaks down or waits for material. The screen is laid out like a control-room
board and updates once a second:

- **Top bar:** plant OEE, how many presses are running, idle or down, total parts, good rate, and the time of the last update.
- **LINE tab, machine table:** one row per press in line order. State is a solid block with how long
  the press has been in it (DOWN blinks, like an andon light). Then OEE, Availability, Performance,
  Quality, parts, rejects (NG), and last cycle against ideal cycle; a cycle more than 20% over ideal is amber.
  The square at the end of the row lights up when that press's sensor sends a reading.
- **LINE tab, data path:** where the numbers come from. Each reading moves along the line
  PRESS → SENSOR → GATEWAY → DB as a small square (green good part, red reject or breakdown, amber idle).
  Once a second the screen reads the new rows back from the DB, and they appear in the log on the right.
- **API tab:** reference for the key-protected REST API, test keys, and a console docked at the bottom
  that sends real requests and prints the response like `curl -i`. Every common status code
  (200, 204, 400, 401, 403, 404, 405, 429, 500, 503) has a `run` link that produces it.

The app is a single binary with no external dependencies. The dashboard HTML, CSS and JS are embedded in it.

## What it's for

OEE is the usual number a factory uses to see how much of its planned time turns into good parts.
This project shows the plumbing behind a live OEE screen:

- a pattern to start from when you build a shop-floor monitor: replace the simulator with real
  machine data (PLC, MQTT, OPC UA) and keep the hub, the OEE math, and the dashboard;
- a small, readable example of goroutines, channels, `context` cancellation, and Server-Sent Events in Go;
- a way to explain OEE to someone: watch a machine go down and see its Availability drop.

It is not a production MES. It keeps no history, and all counters start from zero when the process starts.

## Themes and icons

The screen has two themes, switched with LIGHT / DARK in the top bar and remembered per browser:

- **Control room** (dark, default) for wall screens and control rooms.
- **Shop floor** (light) for bright halls, tablets at the machine, and print.

All colours are CSS custom properties in `internal/web/static/style.css`; the light theme overrides them under `:root[data-theme="light"]`.
An 18-icon line set for factory UIs lives in `internal/web/static/icons/` (see its README).

## How it works

```
 ┌───────────┐
 │ Press 01  │──┐
 ├───────────┤  │  events      ┌───────────┐  snapshot / 1s   ┌──────────────┐
 │ Press 02  │──┼──────┬─────▶ │    Hub    │ ───────────────▶ │  Browser(s)  │
 ├───────────┤  │      │       │ OEE state │    (SSE fan-out) │  dashboard   │
 │   ...     │──┘      │       └───────────┘                  └──────────────┘
 └───────────┘         │       ┌───────────┐  new rows / 1s           ▲
                       └─────▶ │   Store   │ ─────────────────────────┘
                               │ last 500  │  (GET /api/readings)
                               └───────────┘
 1 goroutine per machine
```

1. **Simulator** (`internal/simulator`): each machine runs in its own goroutine. After every cycle it
   sends a shot event (good or scrap) to a shared channel. It can also send state changes:
   RUNNING → DOWN (breakdown, 6–20 s) or RUNNING → IDLE (no material, 2–8 s).
   This is the part a real PLC gateway would replace.
2. **Hub** (`internal/hub`): the only goroutine that reads the channel. It updates each machine's part
   counters, current state, and running time. Once a second it computes OEE for every machine
   and sends the snapshot to every connected browser. If a browser is still busy with the previous
   snapshot, it skips this one, so one slow client can't hold up the hub.
3. **Store** (`internal/store`): every event is also written to a table that keeps the last 500
   readings in memory. It stands in for a real database (PostgreSQL, TimescaleDB, InfluxDB) so the
   demo has nothing to install. `/api/readings?after=<id>` returns the rows written since a given ID,
   which is what the data path animation reads every second.
4. **OEE** (`internal/oee`): plain functions with no state, covered by unit tests.
5. **Web** (`internal/web`): `net/http` with Go 1.22+ routing. `/events` streams the snapshots as
   Server-Sent Events. The browser uses `EventSource`, which reconnects on its own if the connection drops.

`Ctrl+C` cancels one `context`. That stops the machines, the hub, and the open SSE streams, then the HTTP server shuts down.

### OEE formula

| Factor | Formula |
|--------|---------|
| Availability | running time ÷ planned time |
| Performance | (ideal cycle time × total parts) ÷ running time |
| Quality | good parts ÷ total parts |
| **OEE** | Availability × Performance × Quality |

With the default settings, machines end up around 55–80% OEE.

## Run it locally

```bash
go run ./cmd/telemetryhub
# open http://localhost:8080
```

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | HTTP listen address |
| `-machines` | `12` | Number of simulated machines |
| `-seed` | random | Same seed → same machine behaviour |

```bash
go run ./cmd/telemetryhub -machines 24 -addr :9000 -seed 42
```

With Docker:

```bash
docker build -t telemetryhub .
docker run --rm -p 8080:8080 telemetryhub
```

## API

### Public API (`/api/v1`, needs a key)

The **API** tab in the dashboard (`/#api`) documents this API and has a console to send requests from the browser.

```bash
curl -i -H "Authorization: Bearer mth_test_full_7f3a9c2e"   https://machine-telemetry-hub.vercel.app/api/v1/machines
```

| Request | Scope | Returns |
|---------|-------|---------|
| `GET /api/v1/machines` | `machines:read` | All machines, plus plant totals in `meta.summary` |
| `GET /api/v1/machines/{id}` | `machines:read` | One machine (`M01`…`M12`) |
| `GET /api/v1/readings?after=<id>&limit=<n>` | `readings:read` | Stored readings, oldest first. Pass `meta.nextAfter` as `after` to get only new rows |
| `GET /api/v1/status/{code}` | none | Test endpoint: returns the code you ask for |

Demo keys (public on purpose; each one shows a different response):

| Key | Scopes | Limit | Shows |
|-----|--------|-------|-------|
| `mth_test_full_7f3a9c2e` | both | 120/min | 200 |
| `mth_test_readings_4b81d0aa` | `readings:read` | 120/min | 403 on `/machines` |
| `mth_test_limited_c29e5f13` | both | 5/min | 429 after 5 calls |
| `mth_test_revoked_0d6e8b71` | both | – | 401 `api_key_revoked` |

How it behaves:

- The key goes in `Authorization: Bearer <key>` or `X-API-Key: <key>`.
- Checks run in this order: method (405 with `Allow`), key (401 with `WWW-Authenticate`), scope (403),
  rate limit (429 with `Retry-After`), query parameters (400), resource (404).
- Errors are RFC 9457 problem details (`application/problem+json`) with an extra `code` field to switch on
  (`missing_api_key`, `insufficient_scope`, `rate_limited`, …) and a `requestId` that matches the `X-Request-Id` header.
- Every authenticated response has `RateLimit-Limit`, `RateLimit-Remaining` and `RateLimit-Reset`.
  The limit is a fixed one-minute window per key, kept in memory. Because the keys are public,
  everyone using the demo shares the same limits.
- CORS is open (`Access-Control-Allow-Origin: *`) and `OPTIONS` preflight returns 204.

### Internal endpoints (used by the dashboard, no key)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Dashboard |
| GET | `/api/machines` | Current snapshot (JSON) |
| GET | `/api/readings?after=<id>&limit=<n>` | Stored readings with ID > `after`, oldest first (default 50, max 200) |
| GET | `/api/demo-keys` | The demo keys shown in the API tab |
| GET | `/events` | Live snapshots (Server-Sent Events) |
| GET | `/healthz` | Liveness probe |

## Deployment (Vercel)

The demo runs on Vercel as a container, built from `Dockerfile.vercel`, which listens on port 80.

```bash
vercel deploy --prod
```

Vercel runs containers as serverless functions, not as long-running servers, so the demo behaves a bit differently from a local run:

- After about 5 minutes with no visitors the container stops. The next visitor starts a new simulation from zero.
- If Vercel runs more than one instance, two visitors may see different numbers.
- Each SSE connection is closed after 5 minutes. The dashboard shows "Reconnecting…" for a moment and then continues.

To keep the counters running all the time, deploy the normal `Dockerfile` on a host that keeps a process up, such as a VM, Fly.io, or Railway.

## Project layout

```
cmd/telemetryhub/     main: flags, wiring, graceful shutdown
internal/telemetry/   event and machine types
internal/simulator/   fake machines (goroutine per machine)
internal/hub/         live state, OEE snapshots, pub/sub
internal/store/       in-memory table of the last 500 readings (demo database)
internal/api/         public /api/v1: keys, scopes, rate limit, problem+json errors
internal/oee/         OEE calculation
internal/web/         HTTP handlers + embedded dashboard
```

## Development

```bash
go test ./...          # unit tests
go test -race ./...    # with race detector (needs cgo)
go vet ./...
```

CI runs gofmt, vet, race tests, build, and staticcheck on every push.

## Next steps

- Read real machine data from MQTT or OPC UA instead of the simulator
- Store history in PostgreSQL / TimescaleDB and show trends
- Reset counters per shift and record downtime reason codes
