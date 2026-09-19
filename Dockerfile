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

# --- Final stage: minimal agent runtime ---
# Debian (glibc), not Alpine: the Flutter/Dart SDK does not run on musl.
# No dev toolchains are preinstalled — agents install what a task needs
# (see base's agent-workflow.md §13) and installs persist across events
# until the container is recreated, so the box warms up with use. Only
# what every run needs from the first command is baked in: git, gh, the
# docker CLI (daemon via the mounted host socket), and opencode.
FROM debian:12-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash ca-certificates curl git unzip xz-utils libstdc++6 \
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

# opencode CLI.
RUN curl -fsSL https://opencode.ai/install | bash
ENV PATH="/root/.opencode/bin:${PATH}"

COPY --from=builder /bin/barney /usr/local/bin/barney

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/barney"]