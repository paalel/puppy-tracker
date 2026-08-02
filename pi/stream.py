#!/usr/bin/env python3
"""
Stream Camera Module 3 to the puppy tracker server.

Setup:
  pip install requests

Usage:
  export CAMERA_TOKEN="your_token_here"
  export PUPPY_SERVER="https://your-app.fly.dev"
  python3 stream.py

To run on boot, add to crontab:
  @reboot CAMERA_TOKEN=... PUPPY_SERVER=... python3 /home/pi/stream.py >> /home/pi/stream.log 2>&1
"""
import os
import struct
import subprocess
import sys
import time

import requests

TOKEN = os.environ.get("CAMERA_TOKEN", "")
SERVER = os.environ.get("PUPPY_SERVER", "").rstrip("/")

if not TOKEN or not SERVER:
    print("Set CAMERA_TOKEN and PUPPY_SERVER environment variables.", flush=True)
    sys.exit(1)


def frames():
    """Yield length-prefixed JPEG frames read from libcamera-vid."""
    proc = subprocess.Popen(
        [
            "rpicam-vid",
            "--codec", "mjpeg",
            "--inline",           # embed SPS/PPS in every frame
            "--mode", "2304:1296:12:P",  # full sensor readout → wider FoV
            "--width", "640",
            "--height", "480",
            "--framerate", "15",
            "--timeout", "0",     # run indefinitely
            "-o", "-",            # write to stdout
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    )
    buf = b""
    try:
        while True:
            chunk = proc.stdout.read(65536)
            if not chunk:
                break
            buf += chunk
            # Extract complete JPEG frames by scanning for SOI/EOI markers.
            while True:
                start = buf.find(b"\xff\xd8")
                if start == -1:
                    break
                end = buf.find(b"\xff\xd9", start + 2)
                if end == -1:
                    break
                frame = buf[start : end + 2]
                buf = buf[end + 2 :]
                # Prefix each frame with its 4-byte big-endian length.
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
