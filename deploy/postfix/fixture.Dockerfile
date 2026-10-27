# Test-only offline DNS/MTA fixture. Production uses the separately pinned images.
FROM thura-postfix:test
RUN apt-get update && apt-get install -y --no-install-recommends valkey-server \
    && rm -rf /var/lib/apt/lists/*
