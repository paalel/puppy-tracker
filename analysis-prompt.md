# Alma sleep analysis prompt

Use this prompt when the database has grown (aim for 100+ completed sessions before drawing firm conclusions).

---

## Context

June is a Nova Scotia Duck Tolling Retriever, born ~April 2026. She lives with Pål and Bodil in Norway (UTC+2). The app tracks her daily routine of 7 sessions: Tidlig morgen, Morgen, Lunsj, Formiddag, Middag, Kveld, Leggetid for Bodil og Pål.

Key schema columns relevant to sleep analysis:
- `woke_at`, `crate_at`, `slept_at` — UTC timestamps
- `sleep_ease` — 'easy', 'ok', 'hard'
- `overtired` — boolean (1 = yes)
- `training_quality` — 'sharp', 'ok', 'distracted'
- `toilet_pee`, `toilet_poop`, `toilet_accident` — booleans
- `comment` — free text observations
- `routine_session_id` → `routine_sessions.label` and `.position`

Derived metrics:
- Settle time = `slept_at - crate_at` in minutes
- Awake window = `crate_at - woke_at` in minutes

All times stored as UTC. Norway is UTC+2 (CEST summer). Soft midnight: sessions with `woke_at` before 04:00 local are attributed to the previous calendar day.

---

## Hypotheses to test (identified from first 40 sessions)

1. **Physical exercise → faster settle.** Sessions with running (fetch, long-line) have meaningfully shorter settle times than sessions with only training and sniffing. Hypothesis: settle time is lower when the awake window included physical running.

2. **Overtired = missed the crating window.** Zoomies and biting at the end of awake windows are cortisol-driven overtiredness signals, not over-arousal. By the time these appear, the ideal crating moment has passed. Hypothesis: overtired sessions have longer settle times and worse sleep ease than sessions crated before these signals appear.

3. **Distracted training quality predicts hard settle.** Both distracted-training sessions in the first 40 were hard. Hypothesis: when training quality is 'distracted', sleep ease is worse.

4. **Poop urgency disrupts settle.** Several sessions show stress or re-arousal mid-settle linked to needing to poop. Hypothesis: sessions where `toilet_poop = 1` and no poop was recorded before crating have worse sleep ease.

5. **Morgen is the hardest session.** Average settle at Morgen (37.5 min) is highest of all labels. Hypothesis: this persists because she is freshest after overnight sleep and needs more physical activity in that window specifically.

6. **Kong in the crate reduces settle time.** Not currently tracked in the app. If a kong-in-crate field is added, test whether sessions with kong have shorter settle times and better ease.

---

## Queries to run

```sql
-- Settle time and ease distribution
SELECT sleep_ease, COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle,
  ROUND(AVG(awake_mins), 1) as avg_awake
FROM (
  SELECT sleep_ease,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins,
    CAST((strftime('%s', crate_at) - strftime('%s', woke_at)) / 60 AS INTEGER) as awake_mins
  FROM sessions WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND sleep_ease != ''
) GROUP BY sleep_ease ORDER BY avg_settle;

-- By session label
SELECT rs.label, COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle,
  ROUND(AVG(awake_mins), 1) as avg_awake
FROM (
  SELECT routine_session_id,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins,
    CAST((strftime('%s', crate_at) - strftime('%s', woke_at)) / 60 AS INTEGER) as awake_mins
  FROM sessions WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL
) s JOIN routine_sessions rs ON rs.id = s.routine_session_id
GROUP BY rs.label ORDER BY rs.position;

-- Overtired effect
SELECT overtired, COUNT(*) as n, ROUND(AVG(settle_mins), 1) as avg_settle FROM (
  SELECT overtired,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins
  FROM sessions WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL
) GROUP BY overtired;

-- Training quality effect
SELECT training_quality, COUNT(*) as n, ROUND(AVG(settle_mins), 1) as avg_settle FROM (
  SELECT training_quality,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins
  FROM sessions WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND training_quality != ''
) GROUP BY training_quality ORDER BY avg_settle;

-- Read all comments for qualitative patterns
SELECT s.id, s.date, rs.label, sleep_ease, overtired, training_quality,
  CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins,
  CAST((strftime('%s', crate_at) - strftime('%s', woke_at)) / 60 AS INTEGER) as awake_mins,
  comment
FROM sessions s
LEFT JOIN routine_sessions rs ON rs.id = s.routine_session_id
WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND comment != ''
ORDER BY s.id;
```

---

## What to look for

- Do the easy/hard settle time gaps hold or shrink as n grows?
- Are there session labels that consistently improve or worsen over time (learning curve)?
- Do comments reveal new patterns not captured by the structured fields?
- Is there a time-of-day effect within the awake window (does crating earlier in the hour help)?
- Does settle time trend downward over weeks as she matures and learns the crate routine?

## Known data quality issues

- Saturday June 28 data may have timing errors (forgot to register in app)
- Session 35 (June 27, Morgen) has a 107-minute settle time — likely a data entry error, consider excluding outliers >90 min
- Session 30 (June 26, Lunsj) 66-minute settle — also likely an error
- Kong/slikkematte in crate vs pen is not tracked; add this field to get cleaner data on hypothesis 6
