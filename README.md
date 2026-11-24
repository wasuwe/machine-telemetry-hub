# Machine Telemetry Hub

A live **OEE (Overall Equipment Effectiveness) dashboard** for a simulated press line, written in Go.

Every machine runs in its own goroutine and streams shot and state events into a hub.
The hub keeps live counters and pushes snapshots to the browser with Server-Sent Events.
There are no external dependencies: the whole app, dashboard included, is a single binary.

<!-- TODO: add a screenshot or GIF: docs/dashboard.gif -->

## Quick start

```bash
go run ./cmd/telemetryhub
# open http://localhost:8080
```

Options:

```bash
go run ./cmd/telemetryhub -machines 24 -addr :9000 -seed 42
```

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `:8080` | HTTP listen address |
| `-machines` | `12` | Number of simulated machines |
| `-seed` | random | Same seed → same machine behaviour |

With Docker:

```bash
docker build -t telemetryhub .
docker run --rm -p 8080:8080 telemetryhub
```

## Themes and icons

The dashboard has two themes, switched with the button in the header and remembered per browser:

- **Control room** (dark, default) for wall screens and control rooms.
- **Shop floor** (light) for bright halls, tablets at the machine, and print.

All colours are CSS custom properties in `internal/web/static/style.css`; the light theme overrides them under `:root[data-theme="light"]`.
An 18-icon line set for factory UIs lives in `internal/web/static/icons/` (see its README).

## How it works

```
 ┌───────────┐
 │ Press 01  │──┐
 ├───────────┤  │  events      ┌───────────┐  snapshot / 1s   ┌──────────────┐
 │ Press 02  │──┼────────────▶ │    Hub    │ ───────────────▶ │  Browser(s)  │
 ├───────────┤  │  (channel)   │ OEE state │    (SSE fan-out) │  dashboard   │
 │   ...     │──┘              └───────────┘                  └──────────────┘
 └───────────┘
 1 goroutine per machine
```

- **Simulator** (`internal/simulator`): each machine is a goroutine that cycles, makes good or scrap parts, and sometimes breaks down or waits for material.
- **Hub** (`internal/hub`): applies events, computes OEE on demand, and fans out snapshots. A slow browser never blocks the hub, because a subscriber that is still busy skips that update.
- **OEE** (`internal/oee`): pure functions, fully unit-tested.
- **Web** (`internal/web`): `net/http` with Go 1.22+ routing. Static files are embedded with `embed`.

Shutdown is graceful: `Ctrl+C` cancels one `context` that stops the machines, the hub, and the open SSE streams.

### OEE formula

| Factor | Formula |
|--------|---------|
| Availability | running time ÷ planned time |
| Performance | (ideal cycle time × total parts) ÷ running time |
| Quality | good parts ÷ total parts |
| **OEE** | Availability × Performance × Quality |

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Dashboard |
| GET | `/api/machines` | Current snapshot (JSON) |
| GET | `/events` | Live snapshots (Server-Sent Events) |
| GET | `/healthz` | Liveness probe |

## Project layout

```
cmd/telemetryhub/     main: flags, wiring, graceful shutdown
internal/telemetry/   event and machine types
internal/simulator/   fake machines (goroutine per machine)
internal/hub/         live state, OEE snapshots, pub/sub
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

## Ideas for next steps

- Real input: read from MQTT or OPC UA instead of the simulator
- Persist history to PostgreSQL / TimescaleDB and show trends
- Per-shift reset and downtime reason codes
