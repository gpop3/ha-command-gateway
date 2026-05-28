#!/bin/sh
set -e

python3 -m piper.http_server \
    --model "$PIPER_MODEL" \
    --port 5000 &
PIPER_PID=$!

echo "Waiting for piper..."
until wget -qO- http://localhost:5000/health > /dev/null 2>&1; do
    sleep 0.5
done
echo "Piper ready."

trap "kill $PIPER_PID" EXIT

exec assistant