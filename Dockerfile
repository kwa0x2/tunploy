# syntax=docker/dockerfile:1

# Build stages run on the host's platform and cross-compile, so an arm64
# laptop builds an amd64 image without emulating Node or Go.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS go
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/web/dist/ internal/web/dist/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/tunploy ./cmd/tunploy

# Runs as root on purpose: the Docker socket is root-owned and its group id
# differs on every host, so a non-root user would need per-host setup.
FROM gcr.io/distroless/static-debian12
COPY --from=go /out/tunploy /usr/local/bin/tunploy
ENV TUNPLOY_LISTEN=:3000 \
    TUNPLOY_DATA_DIR=/var/lib/tunploy
EXPOSE 3000
VOLUME /var/lib/tunploy
ENTRYPOINT ["/usr/local/bin/tunploy"]
