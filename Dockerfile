# ─────────────────────────────────────────────
# Stage 1 : Build
# ─────────────────────────────────────────────
FROM golang:1.25-bookworm AS builder

# Dépendances système pour CGO (Vosk)
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget unzip ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Télécharger libvosk + header pour aarch64
RUN wget -q https://github.com/alphacep/vosk-api/releases/download/v0.3.45/vosk-linux-aarch64-0.3.45.zip \
    && unzip -q vosk-linux-aarch64-0.3.45.zip \
    && cp vosk-linux-aarch64-0.3.45/libvosk.so /usr/local/lib/ \
    && cp vosk-linux-aarch64-0.3.45/vosk_api.h /usr/local/include/ \
    && ldconfig \
    && rm -rf vosk-linux-aarch64-0.3.45*

WORKDIR /app

# Copier les fichiers Go
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Compilation avec CGO activé
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-I/usr/local/include"
ENV CGO_LDFLAGS="-L/usr/local/lib -lvosk"

RUN go build -ldflags="-s -w" -o assistant ./cmd/assistant/

# ─────────────────────────────────────────────
# Stage 2 : Image finale (légère)
# ─────────────────────────────────────────────
FROM debian:bookworm-slim

# Dépendances runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Audio
    alsa-utils \
    sox \
    ffmpeg \
    # Libs C
    libstdc++6 \
    libatomic1 \
    ca-certificates \
    wget \
    # Python
    python3 \
    python3-pip \
    && rm -rf /var/lib/apt/lists/*

# Copier libvosk depuis le builder
COPY --from=builder /usr/local/lib/libvosk.so /usr/local/lib/
RUN ldconfig

# Copier le binaire compilé
COPY --from=builder /app/assistant /usr/local/bin/assistant

# Installer piper-tts[http] avec le wheel ARM64 Linux
RUN pip install --no-cache-dir \
    "piper-tts[http] @ https://github.com/OHF-Voice/piper1-gpl/releases/download/v1.4.2/piper_tts-1.4.2-cp39-abi3-manylinux_2_17_aarch64.manylinux2014_aarch64.manylinux_2_28_aarch64.whl"

# Voix française Piper
RUN mkdir -p /opt/piper-voices && \
    wget -q -O /opt/piper-voices/fr_FR-siwis-medium.onnx \
        https://huggingface.co/rhasspy/piper-voices/resolve/main/fr/fr_FR/siwis/medium/fr_FR-siwis-medium.onnx && \
    wget -q -O /opt/piper-voices/fr_FR-siwis-medium.onnx.json \
        https://huggingface.co/rhasspy/piper-voices/resolve/main/fr/fr_FR/siwis/medium/fr_FR-siwis-medium.onnx.json

# Dossier modèle Vosk (monté en volume)
RUN mkdir -p /opt/vosk-model

WORKDIR /app

# Variables d'environnement par défaut
ENV ALSA_DEVICE=plughw:CARD=Bar,DEV=0
ENV GSM_PORT=/dev/ttyUSB0
ENV VOSK_MODEL_PATH=/opt/vosk-model
ENV PIPER_BIN=/opt/piper/piper
ENV PIPER_MODEL=/opt/piper-voices/fr_FR-siwis-medium.onnx

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENTRYPOINT ["entrypoint.sh"]