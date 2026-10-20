FROM rspamd/rspamd@sha256:dfb8176e560c178faa4e18ebf9c9b43d0d60c2d2fa03f748c34a27e5c3418dc4
USER root
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends postfix python3 ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 thura-mail \
    && useradd --system --uid 10001 --gid thura-mail --no-create-home thura-mail
COPY relay.py configure.py managed.py supervise.py /opt/thura/
COPY rspamd/ /etc/rspamd/local.d/
RUN chmod 0755 /opt/thura && chmod 0644 /opt/thura/*.py
ENTRYPOINT ["python3", "/opt/thura/supervise.py"]
