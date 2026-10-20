# syntax=docker/dockerfile:1
# Builds thura into one static binary and runs it from a minimal image.
# The builder has Go and Node; `lidza build` builds the frontend, embeds it
# and compiles the binary, with the framework release and Go version the
# app's go.mod pins (lidza gen deploy rewrites them). A go.mod that points
# the framework at a local checkout (--lidza-dir) cannot build here: pin a
# published version first.

FROM golang:1.27-bookworm AS build
ENV CGO_ENABLED=0
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
 && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && rm -rf /var/lib/apt/lists/*
RUN go install github.com/agim/lidza/cmd/lidza@v0.1.123
# lidza build regenerates SQL-backed queries; sqlc requires CGO while building.
RUN CGO_ENABLED=1 go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY package.json package-lock.json* ./
RUN npm ci --no-fund --no-audit || npm install --no-fund --no-audit
COPY . .
RUN mkdir -p db mail admin config && lidza build --out /out/thura \
    && chmod -R a+rX db mail admin config

# Runtime: the binary and what it reads from disk: the migrations (applied
# with DB_MIGRATE=true or `lidza db migrate`), the mail templates (mail/),
# the admin theme (admin/, unless routes.go embeds it) and the sealed
# credentials (config/credentials.yml.enc; the master key comes from
# LIDZA_MASTER_KEY in the environment, never from the image). Per-request SSR (LIDZA_SSR=1) needs
# Node at runtime: use node:22-bookworm-slim here instead and copy dist/.server.
# The local storage provider writes to STORAGE_DIR, which this read-only
# image has no room for: use STORAGE_PROVIDER=s3, or mount a volume there.
FROM gcr.io/distroless/static-debian12:nonroot AS standalone
WORKDIR /app
COPY --from=build /out/thura /app/thura
COPY --from=build /src/db /app/db
COPY --from=build /src/mail /app/mail
COPY --from=build /src/admin /app/admin
COPY --from=build /src/config /app/config
# Plain HTTP on 3000 behind a proxy, or, with LIDZA_TLS_DOMAINS in the
# environment (deploy/production.env), HTTPS on 443 and the redirect on 80.
ENV LIDZA_ADDR=0.0.0.0:3000
EXPOSE 3000 80 443
ENTRYPOINT ["/app/thura"]

# Compact deployment: the same application binary plus the local SFU. The
# supervisor initializes the storage mount, then both services run as UID 65532.
FROM livekit/livekit-server@sha256:d0c04791bf63ca8dcea123571827d56ba9504bd57c8d5de60ee03b5408cc3154 AS meeting
FROM debian:trixie-slim AS compact
RUN apt-get update && apt-get install -y --no-install-recommends python3 ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=standalone /app /app
COPY --from=meeting /livekit-server /usr/local/bin/livekit-server
COPY deploy/compact/app.py /opt/thura/app.py
ENV LIDZA_ADDR=0.0.0.0:3000 GOMEMLIMIT=256MiB GOMAXPROCS=2
EXPOSE 3000 7880 7881 7882/udp
ENTRYPOINT ["python3", "/opt/thura/app.py"]
