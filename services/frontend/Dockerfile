# syntax=docker/dockerfile:1
# hellocalc-frontend. Build from the repository root:
#   docker build -f services/frontend/Dockerfile -t hellocalc-frontend:local .

# Build stage runs on the build host's native platform and cross-compiles,
# so multi-arch builds (linux/amd64, linux/arm64) need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=""
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY internal ./internal
COPY services/frontend ./services/frontend

RUN set -eu; \
    pkg=github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo; \
    ldflags="-s -w -X ${pkg}.Commit=${COMMIT} -X ${pkg}.BuildTime=${BUILD_TIME}"; \
    if [ -n "${VERSION}" ]; then ldflags="${ldflags} -X ${pkg}.Version=${VERSION}"; fi; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
      go build -trimpath -buildvcs=false -ldflags "${ldflags}" -o /out/app ./services/frontend

# Runtime stage: static distroless image (no shell, no package manager),
# running as the unprivileged "nonroot" user (UID/GID 65532).
FROM gcr.io/distroless/static-debian13:nonroot

ARG VERSION=""
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
LABEL org.opencontainers.image.title="hellocalc-frontend" \
      org.opencontainers.image.description="HelloCalc web UI; forwards /api/* to the backend service." \
      org.opencontainers.image.source="https://github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}"

COPY --from=build /out/app /app

ENV HOST=0.0.0.0 \
    PORT=8080 \
    LOG_LEVEL=info
USER 65532:65532
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app", "healthcheck"]

# Exec form: the service is PID 1 and receives SIGTERM/SIGINT directly.
ENTRYPOINT ["/app"]
