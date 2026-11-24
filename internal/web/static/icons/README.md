# Icons

18 line icons for the dashboard: 24px grid, 2px stroke, square caps, mitred corners, no fills.
Every file is stroked in `#737c88`, a grey that reads (3:1 or more) on both the dark and light themes.
To colour an icon by state, inline the SVG and set its `stroke` to `var(--running)`, `var(--idle)`, `var(--down)` or `var(--accent)`.

| File | Use for |
|------|---------|
| `mth-press.svg` | A machine or press |
| `mth-running.svg` | RUNNING state (beside the word) |
| `mth-idle.svg` | IDLE state (beside the word) |
| `mth-down.svg` | DOWN state, breakdown (beside the word) |
| `mth-alert.svg` | A warning that is not a breakdown |
| `mth-oee.svg` | OEE |
| `mth-availability.svg` | Availability (A) |
| `mth-performance.svg` | Performance (P) |
| `mth-quality.svg` | Quality (Q) |
| `mth-parts.svg` | Part counts |
| `mth-cycle.svg` | Cycle time |
| `mth-scrap.svg` | Scrap / rejected parts |
| `mth-material.svg` | Material or coil supply |
| `mth-conveyor.svg` | A line or conveyor |
| `mth-live.svg` | Live stream connected |
| `mth-disconnected.svg` | Live stream lost |
| `mth-shift.svg` | Shift or date range |
| `mth-temperature.svg` | Temperature |

Use at 16, 20 or 24px. A state icon never replaces the state word.
