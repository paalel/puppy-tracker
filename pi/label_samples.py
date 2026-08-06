#!/usr/bin/env python3
"""
Auto-label sample frames using pen_sessions from the local DB.

Usage:
  python3 pi/label_samples.py

Expects:
  ./puppy.db   — local DB (run 'make db-pull' first)
  ./samples/   — sample frames (run 'make get-samples' first)

Output:
  Renames each file to include -in or -out suffix, e.g.:
    20260806-143022-38.2pct.jpg  →  20260806-143022-38.2pct-in.jpg
"""
import os
import sqlite3
from datetime import datetime, timezone

DB_PATH      = "./puppy.db"
SAMPLES_DIR  = "./samples"

db = sqlite3.connect(DB_PATH)
db.row_factory = sqlite3.Row

sessions = db.execute(
    "SELECT started_at, ended_at FROM pen_sessions ORDER BY started_at"
).fetchall()


def parse_dt(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(timezone.utc)


def in_pen_at(dt):
    for row in sessions:
        start = parse_dt(row["started_at"])
        end   = parse_dt(row["ended_at"]) if row["ended_at"] else None
        if start <= dt and (end is None or dt <= end):
            return True
    return False


labeled = unlabeled = 0
for fname in sorted(os.listdir(SAMPLES_DIR)):
    if not fname.endswith(".jpg"):
        continue
    if "-in.jpg" in fname or "-out.jpg" in fname:
        continue
    try:
        stamp = fname.split("-", 2)
        dt = datetime.strptime(f"{stamp[0]}-{stamp[1]}", "%Y%m%d-%H%M%S").replace(
            tzinfo=timezone.utc
        )
    except (ValueError, IndexError):
        print(f"Skipping (can't parse timestamp): {fname}")
        continue

    label  = "in" if in_pen_at(dt) else "out"
    base   = fname[:-4]  # strip .jpg
    new    = os.path.join(SAMPLES_DIR, f"{base}-{label}.jpg")
    old    = os.path.join(SAMPLES_DIR, fname)
    os.rename(old, new)
    print(f"{fname}  →  {label}")
    labeled += 1

print(f"\nLabeled {labeled} files. {unlabeled} skipped.")
