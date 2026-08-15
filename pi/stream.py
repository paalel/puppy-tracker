#!/usr/bin/env python3
"""
Push HLS video+audio from a Tapo camera to the puppy tracker server.
Also detects puppy presence via fur colour using the camera's sub-stream.

Setup:
  pip install requests
  sudo apt install python3-opencv   # for presence detection (optional)

Usage:
  export CAMERA_TOKEN="your_token_here"
  export PUPPY_SERVER="https://your-app.fly.dev"
  export TAPO_URL="rtsp://user@email.com:password@192.168.x.x:554/stream1"
  export CAMERA_ID="pen"   # or "crate"
  python3 stream.py
"""
import os
import subprocess
import sys
import threading
import time

import requests

TOKEN     = os.environ.get("CAMERA_TOKEN", "")
SERVER    = os.environ.get("PUPPY_SERVER", "").rstrip("/")
TAPO_URL  = os.environ.get("TAPO_URL", "")     # stream1 (main, high-res)
CAMERA_ID = os.environ.get("CAMERA_ID", "pen")

if not TOKEN or not SERVER or not TAPO_URL:
    print("Set CAMERA_TOKEN, PUPPY_SERVER and TAPO_URL environment variables.", flush=True)
    sys.exit(1)

# Sub-stream URL for presence detection (lighter, low-res).
TAPO_URL2 = TAPO_URL.replace("stream1", "stream2")

# Toller fur HSV range (OpenCV scale: H 0-179, S/V 0-255).
_HUE_LOW  = 12
_HUE_HIGH = 20
_SAT_LOW  = 130
_VAL_LOW  = 25
_MIN_FRAC = 0.05  # 5 % of pixels must match to count as present
_MAX_FRAC = 0.20  # above 20 % = whole room lit warm — false positive
_DETECT_INTERVAL = 10  # seconds between detection runs

try:
    import cv2
    import numpy as np
    _DETECTION_AVAILABLE = True
    print("Presence detection enabled.", flush=True)
except ImportError:
    _DETECTION_AVAILABLE = False
    print("python3-opencv not found — presence detection disabled.", flush=True)


def _detect_presence(jpeg_bytes):
    """Return (frac, present) or (None, None) on error."""
    if not _DETECTION_AVAILABLE:
        return None, None
    try:
        arr = np.frombuffer(jpeg_bytes, np.uint8)
        img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
        if img is None:
            return None, None
        hsv = cv2.cvtColor(img, cv2.COLOR_BGR2HSV)
        lower = np.array([_HUE_LOW,  _SAT_LOW, _VAL_LOW])
        upper = np.array([_HUE_HIGH, 255,       255     ])
        mask  = cv2.inRange(hsv, lower, upper)
        frac  = np.count_nonzero(mask) / mask.size
        print(f"Detection: {frac:.3%} matching pixels (threshold {_MIN_FRAC:.3%}–{_MAX_FRAC:.3%})", flush=True)
        return frac, _MIN_FRAC <= frac <= _MAX_FRAC
    except Exception as exc:
        print(f"Detection error: {exc}", flush=True)
        return None, None


def _post_presence(present):
    try:
        requests.post(
            f"{SERVER}/camera/{CAMERA_ID}/presence",
            json={"present": present},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=5,
        )
        print(f"Presence: {'in' if present else 'not in'} {CAMERA_ID}", flush=True)
    except Exception as exc:
        print(f"Presence update failed: {exc}", flush=True)


def detection_loop():
    """Read stream2 at 1 fps for presence detection, post every cycle."""
    while True:
        proc = subprocess.Popen(
            [
                "ffmpeg",
                "-rtsp_transport", "tcp",
                "-i", TAPO_URL2,
                "-vf", "fps=1",          # 1 frame/s from sub-stream is enough
                "-f", "mjpeg",
                "-q:v", "10",
                "pipe:1",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
        )
        buf = b""
        last_detect = 0.0
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
                    buf   = buf[end + 2:]

                    now = time.monotonic()
                    if now - last_detect >= _DETECT_INTERVAL:
                        last_detect = now
                        _, present = _detect_presence(frame)
                        if present is not None:
                            threading.Thread(
                                target=_post_presence, args=(present,), daemon=True
                            ).start()
        finally:
            proc.terminate()
        print("Detection FFmpeg exited, restarting in 5 s…", flush=True)
        time.sleep(5)


def hls_push():
    """Push HLS stream to server via FFmpeg; restart on failure."""
    playlist_url = f"{SERVER}/camera/{CAMERA_ID}/hls/stream.m3u8"
    while True:
        print(f"Connecting to {SERVER} (camera: {CAMERA_ID}) …", flush=True)
        proc = subprocess.Popen(
            [
                "ffmpeg",
                "-fflags", "+genpts",    # regenerate timestamps to fix non-monotonic DTS
                "-rtsp_transport", "tcp",
                "-i", TAPO_URL,
                "-avoid_negative_ts", "make_zero",
                "-c:v", "copy",          # pass-through H.264 — no re-encode
                "-af", "aresample=async=1",  # fill audio timestamp gaps with silence
                "-c:a", "aac",
                "-b:a", "32k",
                "-f", "hls",
                "-hls_time", "2",
                "-hls_list_size", "5",
                "-hls_flags", "independent_segments",
                "-hls_segment_filename", f"{SERVER}/camera/{CAMERA_ID}/hls/seg%d.ts",
                "-method", "PUT",
                "-headers", f"Authorization: Bearer {TOKEN}\r\n",
                playlist_url,
            ],
            stderr=subprocess.PIPE,
        )
        for line in proc.stderr:
            text = line.decode("utf-8", errors="replace").rstrip()
            if text:
                print(text, flush=True)
        proc.wait()
        print(f"FFmpeg HLS exited (code {proc.returncode}), restarting in 5 s…", flush=True)
        time.sleep(5)


def main():
    if _DETECTION_AVAILABLE and TAPO_URL2 != TAPO_URL:
        t = threading.Thread(target=detection_loop, daemon=True)
        t.start()
    hls_push()


if __name__ == "__main__":
    main()
