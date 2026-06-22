# Puppy Routine Tracker 🦆

A mobile-first web app for tracking a puppy's daily sleep/wake cycles, meals, and routine. Built for two people to share in real time — both owners see live updates via HTMX polling.

**Stack:** Go (stdlib), SQLite (`modernc.org/sqlite`), HTMX, Tailwind CDN.

---

## Local development

```bash
go run .
```

Opens on `http://localhost:8080`. The database is created as `./puppy.db` on first run and seeded with a default 7-session daily routine.

---

## Deploying to Fly.io

### First-time setup

```bash
# Install the Fly CLI if you haven't already
brew install flyctl

# Log in
fly auth login

# Create the app (only needed once)
fly apps create puppy-routine-tracker

# Create a persistent volume for the SQLite database (1 GB is plenty)
fly volumes create puppy_data --size 1 --region arn

# Deploy
fly deploy
```

### Subsequent deploys

```bash
fly deploy
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `DATABASE_PATH` | `./puppy.db` | Path to the SQLite database file. Set to `/data/puppy.db` in `fly.toml` so it lands on the persistent volume. |

---

## Project layout

```
main.go          — entry point, routes, server startup
db.go            — database types, migrations, all SQL
plan.go          — schedule logic, session templates, seeding
handlers.go      — HTTP handlers, template rendering
migrations/      — SQL migration files (run in order on startup)
templates/       — HTML templates (embedded into the binary at build time)
```

## Data model

- **`puppy_state`** — single row tracking current phase (ACTIVE / SLEEPING)
- **`sessions`** — one row per awake cycle: wake time, sleep time, comment, sleep ease, overtired flag
- **`meals`** — one row per meal per day (breakfast / lunch / dinner)
- **`config`** — key/value store for puppy name, awake window, nap target
- **`routine_sessions`** — the configurable daily routine (label + activities per cycle)
