# syntax=docker/dockerfile:1

# Both build stages run on the build machine's own architecture (BUILDPLATFORM): the UI is the
# same files everywhere and Go cross-compiles, so a multi-arch build never runs under emulation.

# 1. Web UI (static files embedded into the binary)
FROM --platform=$BUILDPLATFORM oven/bun:1.4-alpine AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# 2. Server: pure Go, no cgo, so the result is one static file for the target platform
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/build ./web/build
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags "-s -w -X github.com/LowBloat/LowLex/internal/server.Version=${VERSION}" -o /out/lowlex ./cmd/lowlex \
 && mkdir -p /out/data

# 3. Runtime: CA certificates (for translation providers), time zones and a non-root user
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/lowlex /usr/local/bin/lowlex
COPY --from=server --chown=nonroot:nonroot /out/data /data
ENV LOWLEX_HOST=0.0.0.0 \
    LOWLEX_PORT=4747 \
    LOWLEX_DATA_DIR=/data
VOLUME /data
EXPOSE 4747
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/usr/local/bin/lowlex", "health"]
ENTRYPOINT ["/usr/local/bin/lowlex"]
CMD ["serve"]
