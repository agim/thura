FROM debian:trixie-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends postfix python3 ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 thura-mail \
    && useradd --system --uid 10001 --gid thura-mail --no-create-home thura-mail
COPY relay.py configure.py /opt/thura/
RUN chmod 0755 /opt/thura && chmod 0644 /opt/thura/*.py
ENTRYPOINT ["python3", "/opt/thura/configure.py"]
