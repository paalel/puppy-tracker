# Juni sleep analysis prompt

Use this prompt when the database has grown (aim for 100+ completed sessions before drawing firm conclusions).

---

## Context

June is a Nova Scotia Duck Tolling Retriever, born April 21st 2026. She lives with Pål and Bodil in Norway (UTC+2). The app tracks her daily routine of 7 sessions: Tidlig morgen, Morgen, Lunsj, Formiddag, Middag, Kveld, Leggetid for Bodil og Pål.

Key schema columns relevant to sleep analysis:
- `woke_at`, `crate_at`, `slept_at` — UTC timestamps
- `sleep_ease` — 'easy', 'ok', 'hard'
- `overtired` — boolean (1 = yes)
- `training_quality` — 'sharp', 'ok', 'distracted'
- `toilet_pee`, `toilet_poop`, `toilet_accident` — booleans
- `physical_activity`, `mental_activity`, `calm_winddown`, `environmental_activity` — booleans
- `comment` — free text observations
- `routine_session_id` → `routine_sessions.label` and `.position`
- `excluded` — boolean; always filter `COALESCE(excluded, 0) = 0` in analysis

Additional table: `night_toilets (occurred_at, toilet_pee, toilet_poop, toilet_accident)` — records toilets between last sleep and first wake of the next day.

Derived metrics:
- Settle time = `slept_at - crate_at` in minutes
- Awake window = `crate_at - woke_at` in minutes
- Sessions since last poop = count of completed sessions since the most recent `toilet_poop = 1`

All times stored as UTC. Norway is UTC+2 (CEST summer). Soft midnight: sessions with `woke_at` before 04:00 local are attributed to the previous calendar day.

---

## Hypotheses to test

1. **Physical exercise → faster settle.** Sessions with running (fetch, long-line) have meaningfully shorter settle times than sessions with only training and sniffing. `physical_activity = 1` is the proxy for this.

2. **Overtired = missed the crating window.** Zoomies and biting at the end of awake windows are cortisol-driven overtiredness signals, not over-arousal. By the time these appear, the ideal crating moment has passed. Hypothesis: overtired sessions have longer settle times and worse sleep ease than sessions crated before these signals appear.

3. **Distracted training quality predicts hard settle.** Hypothesis: when `training_quality = 'distracted'`, sleep ease is worse and settle time is longer.

4. **Poop urgency disrupts settle.** Sessions where many sessions have elapsed since the last poop show worse sleep ease and longer settle times — the urgency creates arousal or discomfort during the settle. Use the sessions-since-last-poop CTE (see queries) to bucket this. The base rate is ~28% per session overall; the hazard rises significantly after 4+ sessions without a poop.

5. **Morgen is the hardest session.** Average settle at Morgen was highest early on. Hypothesis: this persists because she is freshest after overnight sleep and needs more physical activity in that window specifically.

6. **Calm winddown reduces settle time.** `calm_winddown = 1` marks sessions where the pre-crate period was deliberately calm (no play, low arousal). Hypothesis: these sessions have shorter settle times and better ease.

7. **Mental activity without physical activity worsens settle.** A session with only training (`mental_activity = 1, physical_activity = 0`) may leave her mentally stimulated but physically not tired enough. Hypothesis: the combination matters more than either alone.

8. **Overnight poop urgency affects Tidlig morgen settle.** If no poop happened during the previous evening (Kveld/Leggetid) or overnight via `night_toilets`, the urgency score entering Tidlig morgen is elevated. Hypothesis: Tidlig morgen sessions with high sessions-since-poop have longer settle times.

---

## Queries to run

```sql
-- Settle time and ease distribution (exclude outliers and excluded sessions)
SELECT sleep_ease, COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle,
  ROUND(AVG(awake_mins), 1) as avg_awake
FROM (
  SELECT sleep_ease,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins,
    CAST((strftime('%s', crate_at) - strftime('%s', woke_at)) / 60 AS INTEGER) as awake_mins
  FROM sessions
  WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND sleep_ease != ''
    AND COALESCE(excluded, 0) = 0
) WHERE settle_mins BETWEEN 1 AND 90
GROUP BY sleep_ease ORDER BY avg_settle;

-- By session label
SELECT rs.label, COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle,
  ROUND(AVG(awake_mins), 1) as avg_awake
FROM (
  SELECT routine_session_id,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins,
    CAST((strftime('%s', crate_at) - strftime('%s', woke_at)) / 60 AS INTEGER) as awake_mins
  FROM sessions
  WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND COALESCE(excluded, 0) = 0
) s JOIN routine_sessions rs ON rs.id = s.routine_session_id
GROUP BY rs.label ORDER BY rs.position;

-- Overtired effect
SELECT overtired, COUNT(*) as n, ROUND(AVG(settle_mins), 1) as avg_settle FROM (
  SELECT overtired,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins
  FROM sessions
  WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND COALESCE(excluded, 0) = 0
) GROUP BY overtired;

-- Training quality effect
SELECT training_quality, COUNT(*) as n, ROUND(AVG(settle_mins), 1) as avg_settle FROM (
  SELECT training_quality,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins
  FROM sessions
  WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND training_quality != ''
    AND COALESCE(excluded, 0) = 0
) GROUP BY training_quality ORDER BY avg_settle;

-- Physical activity effect
SELECT physical_activity, COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle,
  ROUND(AVG(awake_mins), 1) as avg_awake
FROM (
  SELECT physical_activity,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins,
    CAST((strftime('%s', crate_at) - strftime('%s', woke_at)) / 60 AS INTEGER) as awake_mins
  FROM sessions
  WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND COALESCE(excluded, 0) = 0
) GROUP BY physical_activity;

-- Calm winddown effect
SELECT calm_winddown, COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle,
  GROUP_CONCAT(sleep_ease) as ease_distribution
FROM (
  SELECT calm_winddown, sleep_ease,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins
  FROM sessions
  WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND COALESCE(excluded, 0) = 0
) GROUP BY calm_winddown;

-- Activity combination effect (physical × mental)
SELECT physical_activity, mental_activity, COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle
FROM (
  SELECT physical_activity, mental_activity,
    CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins
  FROM sessions
  WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND COALESCE(excluded, 0) = 0
) GROUP BY physical_activity, mental_activity ORDER BY avg_settle;

-- Poop urgency effect on settle (sessions-since-last-poop buckets)
WITH session_order AS (
  SELECT s.id,
    CAST((strftime('%s', s.slept_at) - strftime('%s', s.crate_at)) / 60 AS INTEGER) as settle_mins,
    s.sleep_ease,
    s.toilet_poop,
    ROW_NUMBER() OVER (ORDER BY s.date, rs.position) AS rn
  FROM sessions s
  JOIN routine_sessions rs ON rs.id = s.routine_session_id
  WHERE s.slept_at IS NOT NULL AND s.crate_at IS NOT NULL AND COALESCE(s.excluded, 0) = 0
)
SELECT
  CASE
    WHEN sessions_since_poop <= 1 THEN '1 (fresh)'
    WHEN sessions_since_poop <= 3 THEN '2-3'
    WHEN sessions_since_poop <= 5 THEN '4-5'
    ELSE '6+'
  END as urgency_bucket,
  COUNT(*) as n,
  ROUND(AVG(settle_mins), 1) as avg_settle,
  ROUND(AVG(CASE WHEN sleep_ease = 'easy' THEN 1.0
               WHEN sleep_ease = 'ok'   THEN 0.5
               WHEN sleep_ease = 'hard' THEN 0.0 END), 2) as ease_score
FROM (
  SELECT s.settle_mins, s.sleep_ease,
    s.rn - (
      SELECT MAX(p.rn) FROM session_order p
      WHERE p.rn < s.rn AND p.toilet_poop = 1
    ) AS sessions_since_poop
  FROM session_order s
  WHERE EXISTS (
    SELECT 1 FROM session_order p WHERE p.rn < s.rn AND p.toilet_poop = 1
  )
)
GROUP BY urgency_bucket ORDER BY MIN(sessions_since_poop);

-- Read all comments for qualitative patterns
SELECT s.id, s.date, rs.label, sleep_ease, overtired, training_quality,
  CAST((strftime('%s', slept_at) - strftime('%s', crate_at)) / 60 AS INTEGER) as settle_mins,
  CAST((strftime('%s', crate_at) - strftime('%s', woke_at)) / 60 AS INTEGER) as awake_mins,
  comment
FROM sessions s
LEFT JOIN routine_sessions rs ON rs.id = s.routine_session_id
WHERE slept_at IS NOT NULL AND crate_at IS NOT NULL AND comment != ''
  AND COALESCE(excluded, 0) = 0
ORDER BY s.id;
```

---

## What to look for

- Do the easy/hard settle time gaps hold or shrink as n grows?
- Are there session labels that consistently improve or worsen over time (learning curve)?
- Do comments reveal new patterns not captured by the structured fields?
- Is there a time-of-day effect within the awake window (does crating earlier in the hour help)?
- Does settle time trend downward over weeks as she matures and learns the crate routine?
- Does `calm_winddown` have a detectable effect, or does physical activity dominate?
- Does poop urgency (sessions-since-poop) correlate with settle quality, or is it confounded by session label?

---

## Known data quality issues

- Session 35 (June 27, Morgen) has a 107-minute settle time — likely a data entry error; marked or excluded
- Session 30 (June 26, Lunsj) 66-minute settle — also likely an error
- Kong/slikkematte in crate vs pen is not tracked; add a field to get cleaner data on enrichment effects
- Night toilet records are sparse early on and may undercount overnight poops
