FROM debian:bookworm-slim AS deps

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
    unzip \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --break-system-packages --no-cache-dir "piper-tts[http] @ https://github.com/OHF-Voice/piper1-gpl/releases/download/v1.4.2/piper_tts-1.4.2-cp39-abi3-manylinux_2_17_aarch64.manylinux2014_aarch64.manylinux_2_28_aarch64.whl"

RUN wget -q https://github.com/alphacep/vosk-api/releases/download/v0.3.45/vosk-linux-aarch64-0.3.45.zip \
    && unzip -q vosk-linux-aarch64-0.3.45.zip \
    && cp vosk-linux-aarch64-0.3.45/libvosk.so /usr/local/lib/ \
    && cp vosk-linux-aarch64-0.3.45/vosk_api.h /usr/local/include/ \
    && ldconfig \
    && rm -rf vosk-linux-aarch64-0.3.45*

FROM golang:1.25-bookworm AS builder

COPY --from=deps /usr/local/lib/libvosk.so /usr/local/lib/
COPY --from=deps /usr/local/include/vosk_api.h /usr/local/include/
RUN ldconfig

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-I/usr/local/include"
ENV CGO_LDFLAGS="-L/usr/local/lib -lvosk"

RUN go build -ldflags="-s -w" -o assistant ./cmd/assistant/

FROM deps AS final

COPY --from=builder /app/assistant /usr/local/bin/assistant
RUN mkdir -p /opt/vosk-model

WORKDIR /app

ENV ALSA_DEVICE=plughw:CARD=Bar,DEV=0
ENV GSM_PORT=/dev/ttyUSB0

ENV VOSK_MODEL_PATH=/opt/vosk-model
ENV VOSK_MODEL_NAME=vosk-model-small-fr-0.22

ENV PIPER_BIN=/opt/piper/piper
ENV PIPER_MODEL=/opt/piper-voices/fr_FR-siwis-medium.onnx

ENV SERVER_PIPER_LANG=fr
ENV SERVER_PIPER_VOICE=fr_FR/siwis/medium
ENV SERVER_PIPER_MODEL_NAME=fr_FR-siwis-medium

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENTRYPOINT ["entrypoint.sh"]