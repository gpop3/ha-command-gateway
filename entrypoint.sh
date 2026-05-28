#!/bin/sh
set -e

python3 -m piper.http_server \
    --model "$PIPER_MODEL" \
    --port 5000 &
PIPER_PID=$!

trap "kill $PIPER_PID" EXIT

exec assistant