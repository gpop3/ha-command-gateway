#!/bin/sh
set -e

if [ "$NO_PIPER" != "true" ] && [ "$NO_PIPER" != "1" ]; then
    echo "🚀 Lancement de Piper..."
    python3 -m piper.http_server \
        --model "$PIPER_MODEL" \
        --port 5000 &
    PIPER_PID=$!

    trap "kill $PIPER_PID" EXIT
else
    echo "ℹ️ Piper est désactivé"
fi

exec assistant