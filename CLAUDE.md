# medtrack

## General description

medtrack is a Go + HTMX web app for keeping a personal medication diary. Each
medication has a name, an optional doses-per-cycle range, an optional cycle
duration (default 1 day), an optional total-cycle target, and an optional
X–Y hour interval between doses. The diary view is an interactive table
(card layout on mobile) where the user clicks a green ↑ button to log a dose
and a "+1 cycle" button to advance to the next cycle. Each row is tinted by
status (early/on-time/late/done) based on the configured interval.

State is per-session in memory; download JSON to persist, upload to restore.

## Layout

- `cmd/server/main.go` — entry point
- `internal/models/` — domain types
- `internal/store/` — per-session in-memory store + session manager
- `internal/handlers/` — HTTP handlers + cookie-based session middleware
- `internal/templates/` — html/template files (server-rendered with HTMX swaps)
- `static/` — CSS, favicon
- `tests/server/` — end-to-end tests using `httptest`
