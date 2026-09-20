# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 disseqt
#
# Multi-stage, multi-arch build for the sacr CLI.
# Build with: docker buildx build --platform linux/amd64,linux/arm64 -t sacr:dev .

ARG SACR_VERSION=dev

# ---- build stage --------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG SACR_VERSION
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN set -eux; \
    COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"; \
    DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
      go build -trimpath \
      -ldflags "-s -w -X main.Version=${SACR_VERSION} -X main.GitCommit=${COMMIT} -X main.BuildDate=${DATE}" \
      -o /out/sacr ./cmd/sacr

# ---- runtime stage ------------------------------------------------------
FROM alpine:3.19
RUN apk add --no-cache ca-certificates git \
 && addgroup -g 1000 -S sacr \
 && adduser  -u 1000 -S -G sacr -h /home/sacr sacr \
 && mkdir -p /workspace && chown sacr:sacr /workspace
COPY --from=build /out/sacr /usr/local/bin/sacr
USER 1000:1000
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/sacr"]
