# --- Builder stage ---
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/barney ./cmd/barney

# --- Final stage: toolchain-ready agent runtime ---
# Debian (glibc) instead of Alpine: the Flutter/Dart SDK does not run on
# musl, and kits compose with Go + Flutter + Docker. The base image ships
# the Go toolchain; everything else is added below.
FROM golang:1.24-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash ca-certificates curl unzip xz-utils git libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

# gh from the official apt repo — Debian's own package lags behind.
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
      > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Docker CLI + compose plugin. The daemon is the host's, reached through
# the mounted /var/run/docker.sock (see docker-compose.yml).
RUN curl -fsSL https://download.docker.com/linux/debian/gpg \
      -o /usr/share/keyrings/docker.asc \
    && echo "deb [signed-by=/usr/share/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" \
      > /etc/apt/sources.list.d/docker.list \
    && apt-get update && apt-get install -y --no-install-recommends \
      docker-ce-cli docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*

# Flutter SDK (stable channel). Web artifacts are pre-cached; the Android
# toolchain (SDK/NDK/JDK) is deliberately not installed.
ENV FLUTTER_HOME=/opt/flutter
RUN git clone --depth 1 -b stable https://github.com/flutter/flutter.git "$FLUTTER_HOME" \
    && git config --global --add safe.directory "$FLUTTER_HOME" \
    && "$FLUTTER_HOME/bin/flutter" config --no-analytics \
    && "$FLUTTER_HOME/bin/flutter" precache --universal --web \
    && "$FLUTTER_HOME/bin/flutter" --version
ENV PATH="$FLUTTER_HOME/bin:${PATH}"

# opencode CLI.
RUN curl -fsSL https://opencode.ai/install | bash
ENV PATH="/root/.opencode/bin:${PATH}"

COPY --from=builder /bin/barney /usr/local/bin/barney

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/barney"]