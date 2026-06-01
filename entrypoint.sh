#!/bin/sh
set -e

MODEL_PATH="${PIPER_DIR}/${PIPER_SERVER_MODEL_NAME}.onnx"
CONFIG_PATH="${PIPER_DIR}/${PIPER_SERVER_MODEL_NAME}.onnx.json"

if [ -z "$(ls -A "$VOSK_MODEL_PATH" 2>/dev/null)" ]; then
    echo "📥 Le dossier Vosk est vide. Téléchargement du modèle ${VOSK_MODEL_NAME}..."

    wget -q -O /tmp/vosk-model.zip "https://alphacephei.com/vosk/models/${VOSK_MODEL_NAME}.zip"

    echo "📦 Extraction du modèle Vosk..."
    unzip -q /tmp/vosk-model.zip -d /tmp/

    mv /tmp/${VOSK_MODEL_NAME}/* "$VOSK_MODEL_PATH/"

    rm -rf /tmp/vosk-model.zip /tmp/${VOSK_MODEL_NAME}
    echo "✅ Modèle Vosk prêt !"
else
    echo "ℹ️ Un modèle Vosk est déjà présent dans ${VOSK_MODEL_PATH}."
fi

if [ "$NO_PIPER" != "true" ] && [ "$NO_PIPER" != "1" ]; then
    BASE_URL="https://huggingface.co/rhasspy/piper-voices/resolve/main/${PIPER_SERVER_LANG}/${PIPER_SERVER_VOICE}"
    if [ ! -f "$MODEL_PATH" ]; then
      echo "📥 Téléchargement du modèle Piper (${PIPER_SERVER_MODEL_NAME})..."
      wget -q -O "$MODEL_PATH" "${BASE_URL}/${PIPER_SERVER_MODEL_NAME}.onnx"
    fi

    if [ ! -f "$CONFIG_PATH" ]; then
      echo "📥 Téléchargement de la configuration du modèle..."
      wget -q -O "$CONFIG_PATH" "${BASE_URL}/${PIPER_SERVER_MODEL_NAME}.onnx.json"
    fi

    echo "🚀 Lancement de Piper avec le modèle : ${PIPER_SERVER_MODEL_NAME}"
    python3 -m piper.http_server \
        --model "$MODEL_PATH" \
        --port 5000 &
    PIPER_PID=$!

    trap "kill $PIPER_PID" EXIT
else
    echo "ℹ️ Piper est désactivé"
fi

exec assistant