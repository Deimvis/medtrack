# medtrack

A personal medication diary. Configure your medications (name, doses per cycle,
cycle duration, interval between doses), then increment counters as you take each
dose. Status colours signal whether it's too early, on time, or overdue.
Diary state is per-session and lives in memory only — export to JSON to keep it.

## Run

```
make run          # serve on :8080
make test         # run the e2e tests
make run-docker   # serve on :8000 via docker compose
```
