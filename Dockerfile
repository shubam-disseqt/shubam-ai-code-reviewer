# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 disseqt
#
# Build a static zreview binary and ship it in a minimal runtime image.
# Multi-arch: pass --platform=linux/amd64,linux/arm64 to `docker buildx build`.
# The build stage is pinned to the BUILDPLATFORM so a native toolchain
# cross-compiles for TARGETARCH via GOOS/GOARCH.

ARG ZREVIEW_VERSION=dev

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG ZREVIEW_VERSION
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
      -ldflags "-s -w -X main.Version=${ZREVIEW_VERSION} -X main.GitCommit=${COMMIT} -X main.BuildDate=${DATE}" \
      -o /out/zreview ./cmd/zreview

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git \
 && addgroup -g 1000 -S zreview \
 && adduser  -u 1000 -S -G zreview -h /home/zreview zreview \
 && mkdir -p /repo && chown zreview:zreview /repo
COPY --from=build /out/zreview /usr/local/bin/zreview
USER 1000:1000
WORKDIR /repo
ENTRYPOINT ["zreview"]
