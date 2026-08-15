#!/usr/bin/env python3
"""
Push HLS video+audio from a Tapo camera to the puppy tracker server.
Also detects puppy presence via fur colour using the camera's sub-stream.

FFmpeg writes HLS segments to a local temp directory; Python uploads them
to the server with retry so transient network drops don't lose segments.

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
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time

import requests

TOKEN     = os.environ.get("CAMERA_TOKEN", "")
SERVER    = os.environ.get("PUPPY_SERVER", "").rstrip("/")
TAPO_URL  = os.environ.get("TAPO_URL", "")
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
                "-vf", "fps=1",
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


# ---------------------------------------------------------------------------
# HLS push — local file buffer with retry upload
# ---------------------------------------------------------------------------

_SEG_RE = re.compile(r"^(seg\d+\.ts)\s*$", re.MULTILINE)
_FFMPEG_SILENCE_TIMEOUT = 15  # kill FFmpeg if no stderr output for this long


def _upload_segment(name, data):
    """Upload one segment — single attempt, no retry.
    Retrying stale video adds latency; better to skip and stay near live."""
    try:
        r = requests.put(
            f"{SERVER}/camera/{CAMERA_ID}/hls/{name}",
            data=data,
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=5,
        )
        return r.status_code in (200, 201, 204)
    except Exception as exc:
        print(f"Upload {name} failed (skipping): {exc}", flush=True)
        return False


def _upload_playlist(data, retries=3):
    """Upload the playlist with retry — it's tiny metadata, not time-sensitive."""
    url = f"{SERVER}/camera/{CAMERA_ID}/hls/stream.m3u8"
    for attempt in range(retries):
        try:
            r = requests.put(
                url,
                data=data,
                headers={"Authorization": f"Bearer {TOKEN}"},
                timeout=5,
            )
            if r.status_code in (200, 201, 204):
                return True
            print(f"Upload playlist: HTTP {r.status_code}", flush=True)
        except Exception as exc:
            print(f"Upload playlist attempt {attempt + 1} failed: {exc}", flush=True)
            if attempt < retries - 1:
                time.sleep(0.5)
    return False


def _hls_push_once(tmpdir):
    """Run one FFmpeg session writing to tmpdir, upload segments as they appear."""
    print(f"Connecting to {SERVER} (camera: {CAMERA_ID}) …", flush=True)
    proc = subprocess.Popen(
        [
            "ffmpeg",
            "-fflags", "+genpts",
            "-rtsp_transport", "tcp",
            "-i", TAPO_URL,
            "-avoid_negative_ts", "make_zero",
            "-vf", "scale=854:480",
            "-c:v", "libx264",
            "-preset", "veryfast",
            "-tune", "zerolatency",
            "-crf", "26",
            "-force_key_frames", "expr:gte(t,n_forced*2)",
            "-af", "aresample=async=1",
            "-c:a", "aac",
            "-b:a", "32k",
            "-f", "hls",
            "-hls_time", "1",
            "-hls_list_size", "10",
            "-hls_flags", "independent_segments+delete_segments",
            "-hls_segment_filename", "seg%d.ts",
            "stream.m3u8",
        ],
        cwd=tmpdir,
        stderr=subprocess.PIPE,
        start_new_session=True,  # own process group so we can kill it cleanly
    )

    last_output = [time.monotonic()]  # list so nested functions can mutate it
    stop = threading.Event()

    def _kill_ffmpeg():
        try:
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
        except ProcessLookupError:
            pass

    def _watchdog():
        while not stop.is_set():
            if time.monotonic() - last_output[0] > _FFMPEG_SILENCE_TIMEOUT:
                print(f"FFmpeg silent for {_FFMPEG_SILENCE_TIMEOUT}s — killing", flush=True)
                _kill_ffmpeg()
                return
            time.sleep(2)

    def _read_stderr():
        for line in proc.stderr:
            last_output[0] = time.monotonic()
            text = line.decode("utf-8", errors="replace").rstrip()
            if text:
                print(text, flush=True)

    threading.Thread(target=_watchdog, daemon=True).start()
    threading.Thread(target=_read_stderr, daemon=True).start()

    playlist_path = os.path.join(tmpdir, "stream.m3u8")
    uploaded_segs = set()
    last_m3u8 = None

    while proc.poll() is None:
        if os.path.exists(playlist_path):
            try:
                with open(playlist_path) as f:
                    m3u8_text = f.read()
            except OSError:
                time.sleep(0.2)
                continue

            # Upload new segments before updating the playlist so hls.js never
            # fetches a segment that hasn't arrived on the server yet.
            for seg in _SEG_RE.findall(m3u8_text):
                if seg in uploaded_segs:
                    continue
                seg_path = os.path.join(tmpdir, seg)
                if not os.path.exists(seg_path):
                    continue
                try:
                    with open(seg_path, "rb") as f:
                        data = f.read()
                except OSError as exc:
                    print(f"Read {seg}: {exc}", flush=True)
                    continue
                if _upload_segment(seg, data):
                    print(f"Uploaded {seg} ({len(data) // 1024}KB)", flush=True)
                uploaded_segs.add(seg)  # mark done regardless; move on to stay near live

            if m3u8_text != last_m3u8:
                if _upload_playlist(m3u8_text.encode()):
                    last_m3u8 = m3u8_text

        time.sleep(0.2)

    stop.set()
    proc.wait()
    print(f"FFmpeg exited (code {proc.returncode})", flush=True)


def hls_push():
    while True:
        tmpdir = tempfile.mkdtemp(prefix=f"hls_{CAMERA_ID}_")
        started = time.monotonic()
        try:
            _hls_push_once(tmpdir)
        finally:
            shutil.rmtree(tmpdir, ignore_errors=True)
        # If FFmpeg died almost immediately it likely couldn't connect to the
        # camera — wait longer to avoid hammering it with rapid retries.
        uptime = time.monotonic() - started
        delay = 2 if uptime > 5 else 10
        print(f"Restarting in {delay} s…", flush=True)
        time.sleep(delay)


def main():
    if _DETECTION_AVAILABLE and TAPO_URL2 != TAPO_URL:
        t = threading.Thread(target=detection_loop, daemon=True)
        t.start()
    hls_push()


if __name__ == "__main__":
    main()
