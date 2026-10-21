# Add only a static health probe; keep the official image's non-root USER,
# entrypoint, editor and bundled libraries unchanged.
FROM golang:1.27.3-bookworm AS health
WORKDIR /src
COPY scripts/stack-office-health/main.go ./main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /health main.go
# Immutable vendor index selects native amd64 or arm64, including the previously
# tested ARM64 child. Never pin a single-platform child for distribution.
FROM collabora/code@sha256:bb8f3c9c5c9016045f25fccb5f0649be94469ebc7c7cc379f3576e470cccf33d
COPY --from=health /health /usr/local/bin/thura-office-health
HEALTHCHECK --interval=10s --timeout=6s --start-period=120s --retries=6 CMD ["/usr/local/bin/thura-office-health"]
