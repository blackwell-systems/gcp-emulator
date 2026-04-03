FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy dependency files for all modules
COPY go.mod go.sum ./

# Copy local module sources (replace directives point here)
COPY ../gcp-iam-emulator /gcp-iam-emulator
COPY ../gcp-secret-manager-emulator /gcp-secret-manager-emulator
COPY ../gcp-eventarc-emulator /gcp-eventarc-emulator

# Copy gcp-emulator source
COPY . .

RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -o gcp-emulator ./cmd/gcp-emulator

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && update-ca-certificates && \
    addgroup -S gcpmock && adduser -S gcpmock -G gcpmock

WORKDIR /app
COPY --from=builder /build/gcp-emulator .

USER gcpmock

EXPOSE 9090 8090

LABEL org.opencontainers.image.title="GCP Emulator" \
      org.opencontainers.image.description="Unified local emulator for GCP: Secret Manager, Eventarc, IAM" \
      org.opencontainers.image.url="https://github.com/blackwell-systems/gcp-emulator" \
      org.opencontainers.image.source="https://github.com/blackwell-systems/gcp-emulator" \
      org.opencontainers.image.vendor="Blackwell Systems"

ENTRYPOINT ["./gcp-emulator"]
