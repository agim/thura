# Add only a static health probe; keep the official image's non-root USER,
# entrypoint, editor and bundled libraries unchanged.
FROM golang:1.27-bookworm AS health
WORKDIR /src
COPY scripts/stack-office-health/main.go ./main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /health main.go
FROM collabora/code@sha256:b4cc44e40658d662275bb955c683a254c86148165c587f881711ad4924b4be6b
COPY --from=health /health /usr/local/bin/thura-office-health
