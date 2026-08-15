#!/bin/bash
# Started by crontab @reboot. Waits for the network, then launches both camera streams.

sleep 15  # give the network time to come up

set -a
source /home/paalel/.camera-env
set +a

CAMERA_ID=pen TAPO_URL="$TAPO_PEN_URL" \
  nohup python3 /home/paalel/stream.py >> /home/paalel/stream-pen.log 2>&1 < /dev/null &

CAMERA_ID=crate TAPO_URL="$TAPO_CRATE_URL" \
  nohup python3 /home/paalel/stream-crate.py >> /home/paalel/stream-crate.log 2>&1 < /dev/null &

echo "Camera streams started at $(date)" >> /home/paalel/startup.log
