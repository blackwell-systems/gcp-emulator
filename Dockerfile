# Build context must be the parent directory of all emulator repos.
# docker build -f gcp-emulator/Dockerfile -t gcp-emulator .
#
# In CI, the release workflow checks out all sibling repos and sets
# the build context to the parent directory automatically.

FROM golang:1.24-alpine AS builder

WORKDIR /build/gcp-emulator

# Copy all module sources (paths relative to parent-dir build context)
COPY gcp-iam-emulator/             /build/gcp-iam-emulator/
COPY gcp-secret-manager-emulator/  /build/gcp-secret-manager-emulator/
COPY gcp-eventarc-emulator/        /build/gcp-eventarc-emulator/
COPY gcp-emulator/                 /build/gcp-emulator/

RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -o gcp-emulator ./cmd/gcp-emulator

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && update-ca-certificates && \
    addgroup -S gcpmock && adduser -S gcpmock -G gcpmock

WORKDIR /app
COPY --from=builder /build/gcp-emulator/gcp-emulator .

USER gcpmock

EXPOSE 9090 8090

LABEL org.opencontainers.image.title="GCP Emulator" \
      org.opencontainers.image.description="Unified local emulator for GCP: Secret Manager, Eventarc, IAM" \
      org.opencontainers.image.url="https://github.com/blackwell-systems/gcp-emulator" \
      org.opencontainers.image.source="https://github.com/blackwell-systems/gcp-emulator" \
      org.opencontainers.image.vendor="Blackwell Systems"

ENTRYPOINT ["./gcp-emulator"]
