FROM rspamd/rspamd@sha256:dfb8176e560c178faa4e18ebf9c9b43d0d60c2d2fa03f748c34a27e5c3418dc4
USER root
RUN apt-get update && apt-get install -y --no-install-recommends valkey-server python3 unbound && rm -rf /var/lib/apt/lists/*
COPY config/ /etc/rspamd/local.d/
COPY unbound.conf /etc/unbound-evaluation.conf
COPY start.sh /usr/local/bin/thura-filter-test
USER 11333:11333
ENTRYPOINT ["/bin/sh", "/usr/local/bin/thura-filter-test"]
