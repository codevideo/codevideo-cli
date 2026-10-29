# codevideo-cli render worker image (multi-stage).
#
# Build stage compiles the Go CLI with the exact toolchain go.mod pins (1.26) —
# don't rely on the runtime image's packaged Go, which lags. Runtime stage is
# Alpine 3.22 with its packaged Chromium, Node and ffmpeg (the webm -> mp4 step
# shells out to it). Published to Docker Hub as
# fullstackcraft/codevideo-cli and pulled by the codevideo-api compose stack.

# ---- build: compile the static Go binary --------------------------------------
FROM golang:1.26-alpine AS build
RUN apk add --no-cache git
WORKDIR /src

# Go deps first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# CGO off -> a fully static binary that runs on the Alpine runtime base. VERSION
# is injected by release builds (consumed by main.go's `var version`).
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /out/codevideo

# ---- runtime: Chromium + Node/Puppeteer + ffmpeg ------------------------------
# Alpine 3.22 packages Chromium 142. puppeteer-core 24 installs puppeteer-stream's
# tab-capture extension through the CDP command Extensions.loadUnpacked, which the
# former zenika/alpine-chrome base (Alpine 3.19, Chromium 124) lacks: every
# realtime render failed at browser launch. Fonts match that base, including
# WenQuanYi Zen Hei for CJK text.
FROM alpine:3.22 AS runtime

# Chrome runs with --no-sandbox, so root is expected here (see the --no-sandbox
# note in recordVideoV3.js).
RUN apk add --no-cache \
      chromium chromium-swiftshader nss freetype harfbuzz ca-certificates \
      fontconfig font-freefont font-opensans font-noto-emoji font-wqy-zenhei \
      nodejs npm ffmpeg

WORKDIR /app

# The compiled worker. The Gatsby static site is go:embed'd into the binary, so
# nothing else is needed for the manifest/static servers.
COPY --from=build /out/codevideo /app/codevideo

# The Puppeteer runner + its Node deps. server.go execs
# `node <execDir>/puppeteer-runner/recordVideoV3.js`, so it must sit next to the
# binary. Chromium comes from the base image — no browser download here.
COPY puppeteer-runner/ /app/puppeteer-runner/
WORKDIR /app/puppeteer-runner
RUN npm install
WORKDIR /app

# Runtime config for the containerized serve worker:
#   CODEVIDEO_CHROME_PATH - resolveChromeExecutable() honors this first, so pin
#     the base image's Chromium instead of relying on PATH resolution.
#   CODEVIDEO_WORK_DIR    - the shared render queue the API bind-mounts in. Go's
#     WorkFolder()/VideoFolder() and the --manifest-path/--output-webm args
#     passed to the Node runner all derive from it, keeping both sides in sync.
ENV CODEVIDEO_CHROME_PATH=/usr/bin/chromium-browser \
    CODEVIDEO_WORK_DIR=/work/tmp/v3

# Filesystem-queue worker: no inbound port. The manifest (:7000) and static
# (:7001) servers it starts are reached only by its own Puppeteer over
# localhost, so nothing is published.
ENTRYPOINT ["/app/codevideo"]
CMD ["-m", "serve"]
