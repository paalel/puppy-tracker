# Puppy Tracker — "Simplified" mode design spec

A design brief for the **simplified** version of an existing dog-tracking web
app. Please iterate on the visual design of the screens described below.

## Context

The app helped us through the intense young-puppy phase (rigid nap schedule,
house-training, settling). Our dog is now ~5.5 months and largely self-regulates,
so we're adding a pared-down **"Simplified"** mode for daily life with a grown
dog. The existing detailed version (now called **"Classic"**) stays exactly as
is; a toggle in Settings switches between the two. **This brief is only about the
Simplified screens.**

It's a **mobile-first web app** (used on a phone, added to the home screen).
Single column, touch-friendly, no desktop layout needed. Everything should feel
fast and tappable — most actions are one or two taps.

### Existing visual language (please stay consistent)

**A screenshot of the current app is attached — treat it as the source of truth
for look and feel.** The notes below just describe it in words as backup; if
anything here conflicts with the screenshot, follow the screenshot.

- Warm, calm palette. Background is a soft off-white (`#F7F6F3`). Cards are white,
  `rounded-2xl`, thin stone border, subtle shadow.
- Text is stone/neutral grays. Small UPPERCASE tracked labels for section
  headers (e.g. "HOME ALONE"). Accent colours used sparingly and meaningfully.
- Buttons are pill/badge style: a neutral grey resting state, a coloured filled
  state when active/selected (e.g. green = good, amber = caution, rose = bad,
  sky = home-alone, indigo = neutral accent).
- Tailwind CSS. Emoji are used lightly as iconography (🏠 🐶 😴 💩).
- A fixed bottom nav bar already exists in the app.

## Screen to design

Only the **content of the "Today" main screen** below needs a design — i.e. the
column of cards. Keep the existing **top header** and **bottom nav bar** as they
are (see screenshot); design only the area between them. The settings toggle, the
stats/overview, and all other pages also reuse the app's current design and are
**not** part of this brief.

### "Today" — the main screen

A single scrolling column of four cards. This is where ~all daily interaction
happens. Design the resting state and the active/expanded states of each card.

#### Card A — Poop

Purpose: a quick health log of her poops for the day (regularity/consistency is a
lifelong health signal).

- Shows **today's poops** as a short list, each row a timestamp (e.g. "08:12").
- A prominent **＋** action adds a poop at the current time (one tap).
- Also allow logging an **accident** (indoor pee/poop mistake) — a secondary,
  less prominent add. Accidents should read as distinct from normal poops
  (e.g. a rose/"oops" treatment).
- Each logged entry can be removed (e.g. tap to delete, or swipe).
- A day count is nice ("2 today").

#### Card B — Sleep timer

Purpose: time a nap when we put her down (crate or sofa).

- **Idle state:** a single "Start sleep" button.
- **Running state:** a large live elapsed timer (mm:ss / h:mm) with a clear
  "Wake" / stop button. Should feel glanceable from across the room.
- On stop, the nap is logged with its **duration** (nothing else — no location,
  no note).
- Below, a short list of **today's naps** (start time + duration).

#### Card C — Home alone

Purpose: track time she's home alone and how it went. (This reuses the app's
existing home-alone feature — same behaviour as Classic.)

- **Idle state:** "Start home alone" button.
- **Active state:** a live elapsed timer, plus quick quality controls we can set
  live (while watching the camera) or afterwards:
  - **Where:** Cage / Pen / Roaming (pick one)
  - **Sleep:** Slept well / Some / Didn't sleep (pick one)
  - **Behaviour:** Calm / Unsettled / Stressed (pick one)
  - **Destruction:** a single toggle ("Destroyed something") — colour changes,
    label stays
  - a free-text note
  - An **"Not alone anymore"** button to end.
- Surface the two **records** somewhere on/near this card: **longest calm**
  (calm + nothing destroyed) and **longest overall**. These are motivational,
  like a high-score.

#### Card D — Outings

Purpose: log outings/activities and see how we spend her time (enrichment &
socialisation).

- Shows **today's outings** as a list. Each entry shows its time plus its
  **Where** and **What** tags.
- A **＋** opens an add sheet/expander with two tag groups, **both multi-select**:
  - **Where** (pick any): Voldsløkka · Bjølsenparken · Akerselva · Home · Cafe ·
    Friend's house · Other
  - **What** (pick any): Pee break · Long walk · Training · Play · Fetch ·
    Socialising · Met dogs
- Save logs the outing at the current time. Entries can be edited/removed.
- Keep the add interaction quick — selecting several chips and saving.

> The Where/What option lists are fixed for now (not user-editable in-app).

## Interaction principles

- One-tap logging for the common things (poop, start/stop sleep, start alone).
- Multi-tap only where it adds real value (outing tags).
- Glanceable running timers (sleep, home-alone).
- Nothing destructive without an easy undo/delete.
- Mobile only; single column; thumb-friendly tap targets.

## Out of scope

- No changes to the Classic screens.
- No walk durations, no configurable tag lists, no weight/meds/feeding (may come
  later).
