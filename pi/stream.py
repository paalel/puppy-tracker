#!/usr/bin/env python3
"""
Stream Camera Module 3 to the puppy tracker server.
Also detects puppy presence via fur color and POSTs status to /camera/presence.

Setup:
  pip install requests
  sudo apt install python3-opencv   # for presence detection (optional)

Usage:
  export CAMERA_TOKEN="your_token_here"
  export PUPPY_SERVER="https://your-app.fly.dev"
  python3 stream.py

To run on boot, add to crontab:
  @reboot CAMERA_TOKEN=... PUPPY_SERVER=... python3 /home/paalel/stream.py >> /home/paalel/stream.log 2>&1
"""
import os
import struct
import subprocess
import sys
import threading
import time

import requests

TOKEN = os.environ.get("CAMERA_TOKEN", "")
SERVER = os.environ.get("PUPPY_SERVER", "").rstrip("/")

if not TOKEN or not SERVER:
    print("Set CAMERA_TOKEN and PUPPY_SERVER environment variables.", flush=True)
    sys.exit(1)

# Toller fur HSV range (OpenCV scale: H 0-179, S/V 0-255).
_HUE_LOW  = 12
_HUE_HIGH = 20
_SAT_LOW  = 110
_VAL_LOW  = 25
_MIN_FRAC = 0.05  # 5 % of pixels must match to count as present
_MAX_FRAC = 0.20  # above 20 % means the whole room looks like her — false positive
_DETECT_INTERVAL = 10   # seconds between detection runs
_SAMPLE_INTERVAL = 60   # seconds between saving sample frames
_SAMPLE_DIR      = os.path.expanduser("~/samples")
_MAX_SAMPLES     = 200  # oldest deleted when exceeded

try:
    import cv2
    import numpy as np
    _DETECTION_AVAILABLE = True
    os.makedirs(_SAMPLE_DIR, exist_ok=True)
    print("Presence detection enabled.", flush=True)
except ImportError:
    _DETECTION_AVAILABLE = False
    print("python3-opencv not found — presence detection disabled.", flush=True)


def _save_sample(jpeg_bytes, frac):
    """Save a JPEG sample with the detection percentage in the filename."""
    try:
        stamp = time.strftime("%Y%m%d-%H%M%S", time.gmtime())
        path = os.path.join(_SAMPLE_DIR, f"{stamp}-{frac*100:.1f}pct.jpg")
        with open(path, "wb") as f:
            f.write(jpeg_bytes)
        files = sorted(os.listdir(_SAMPLE_DIR))
        for old in files[:-_MAX_SAMPLES]:
            os.remove(os.path.join(_SAMPLE_DIR, old))
    except Exception as exc:
        print(f"Sample save error: {exc}", flush=True)


def _detect_presence(jpeg_bytes):
    """Return (frac, present) tuple, or (None, None) on error."""
    if not _DETECTION_AVAILABLE:
        return None, None
    try:
        arr = np.frombuffer(jpeg_bytes, np.uint8)
        img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
        if img is None:
            return None
        hsv = cv2.cvtColor(img, cv2.COLOR_BGR2HSV)
        lower = np.array([_HUE_LOW,  _SAT_LOW, _VAL_LOW])
        upper = np.array([_HUE_HIGH, 255,       255     ])
        mask = cv2.inRange(hsv, lower, upper)
        frac = np.count_nonzero(mask) / mask.size
        print(f"Detection: {frac:.3%} matching pixels (threshold {_MIN_FRAC:.3%}–{_MAX_FRAC:.3%})", flush=True)
        return frac, _MIN_FRAC <= frac <= _MAX_FRAC
    except Exception as exc:
        print(f"Detection error: {exc}", flush=True)
        return None, None


def _post_presence(present):
    try:
        requests.post(
            f"{SERVER}/camera/presence",
            json={"present": present},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=5,
        )
        print(f"Presence: {'in pen' if present else 'not in pen'}", flush=True)
    except Exception as exc:
        print(f"Presence update failed: {exc}", flush=True)


def frames():
    """Yield length-prefixed JPEG frames from rpicam-vid; run presence detection on the side."""
    proc = subprocess.Popen(
        [
            "rpicam-vid",
            "--codec", "mjpeg",
            "--inline",
            "--mode", "2304:1296:12:P",  # full sensor readout → wider FoV
            "--width", "640",
            "--height", "480",
            "--framerate", "15",
            "--timeout", "0",
            "-o", "-",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    )
    buf = b""
    last_detect = 0.0
    last_sample = 0.0
    last_presence = None
    try:
        while True:
            chunk = proc.stdout.read(65536)
            if not chunk:
                break
            buf += chunk
            while True:
                start = buf.find(b"\xff\xd8")
                if start == -1:
                    break
                end = buf.find(b"\xff\xd9", start + 2)
                if end == -1:
                    break
                frame = buf[start : end + 2]
                buf = buf[end + 2:]

                now = time.monotonic()
                if now - last_detect >= _DETECT_INTERVAL:
                    last_detect = now
                    frac, present = _detect_presence(frame)
                    if present is not None:
                        last_presence = present
                        threading.Thread(
                            target=_post_presence, args=(present,), daemon=True
                        ).start()
                    if frac is not None and now - last_sample >= _SAMPLE_INTERVAL:
                        last_sample = now
                        threading.Thread(
                            target=_save_sample, args=(frame, frac), daemon=True
                        ).start()

                yield struct.pack(">I", len(frame)) + frame
    finally:
        proc.terminate()


def main():
    while True:
        try:
            print(f"Connecting to {SERVER} …", flush=True)
            resp = requests.put(
                f"{SERVER}/camera/push",
                headers={"Authorization": f"Bearer {TOKEN}"},
                data=frames(),
                stream=True,
                timeout=None,
            )
            print(f"Server closed connection: {resp.status_code}", flush=True)
        except Exception as exc:
            print(f"Error: {exc}", flush=True)
        print("Reconnecting in 5 s…", flush=True)
        time.sleep(5)


if __name__ == "__main__":
    main()
