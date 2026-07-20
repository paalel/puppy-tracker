# Puppy Routine Tracker

A mobile-first web app for tracking a puppy's daily sleep/wake cycles and toilet habits. Built for two people to share — both owners see live updates via HTMX.

**Stack:** Go (stdlib), SQLite (`modernc.org/sqlite`), HTMX, Tailwind CDN. Deployed on Fly.io with a persistent volume for the database.

---

## Project layout

```
main.go           — entry point, server startup
templates.go      — template helpers and rendering
sessions/         — core session tracking: schedule, handlers, poop prediction
stats/            — sleep and toilet analytics
config/           — puppy settings (name, awake window, nap target, wind-down)
routine/          — daily routine template (labels and activities per session)
store/            — shared DB utilities, date/time helpers
templates/        — HTML templates (embedded into the binary at build time)
docs/             — model documentation
```

## Data model

- **`sessions`** — one row per awake cycle: wake, crate, and sleep times; toilet outcome; sleep ease; activity flags; training quality
- **`routine_sessions`** — the configurable daily routine (label + activities per session slot)
- **`config`** — key/value store for puppy name, awake window, nap target, wind-down duration
