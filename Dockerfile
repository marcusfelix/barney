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

# --- Final stage ---
FROM alpine:3.20

RUN apk add --no-cache \
    git \
    github-cli \
    bash \
    ca-certificates \
    curl \
    libstdc++ \
    libgcc

# Install the opencode CLI.
RUN curl -fsSL https://opencode.ai/install | bash
ENV PATH="/root/.opencode/bin:${PATH}"

COPY --from=builder /bin/barney /usr/local/bin/barney

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/barney"]
