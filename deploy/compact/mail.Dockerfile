FROM rspamd/rspamd@sha256:dfb8176e560c178faa4e18ebf9c9b43d0d60c2d2fa03f748c34a27e5c3418dc4
USER root
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends postfix python3 ca-certificates valkey-server clamav-daemon clamav-freshclam \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 thura-mail \
    && useradd --system --uid 10001 --gid thura-mail --no-create-home thura-mail
COPY deploy/postfix/relay.py deploy/postfix/configure.py deploy/postfix/managed.py deploy/postfix/supervise.py /opt/thura/
COPY deploy/postfix/rspamd/ /etc/rspamd/local.d/
COPY deploy/clamav/freshclam.conf /etc/clamav/freshclam.conf
COPY deploy/compact/clamd.conf /etc/clamav/clamd.conf
COPY deploy/compact/valkey.conf /etc/valkey/thura.conf
COPY deploy/compact/mail.py /opt/thura/compact.py
RUN printf 'servers = "127.0.0.1:6379";\ndb = "0";\n' > /etc/rspamd/local.d/redis.conf \
    && printf 'max_tasks = 1;\n' >> /etc/rspamd/local.d/worker-normal.inc \
    && sed -i 's/^TestDatabases yes$/TestDatabases no/' /etc/clamav/freshclam.conf \
    && mkdir -p /var/lib/thura-valkey /run/clamav \
    && chown valkey:valkey /var/lib/thura-valkey \
    && chown clamav:clamav /var/lib/clamav /run/clamav
ENTRYPOINT ["python3", "/opt/thura/compact.py"]
