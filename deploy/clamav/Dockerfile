FROM alpine:3.23
RUN apk add --no-cache clamav clamav-daemon ca-certificates tini \
    && mkdir -p /var/lib/clamav /run/clamav \
    && chown -R clamav:clamav /var/lib/clamav /run/clamav
COPY clamd.conf /etc/clamav/clamd.conf
COPY freshclam.conf /etc/clamav/freshclam.conf
COPY start.sh /usr/local/bin/start-clamav
RUN chmod 0755 /usr/local/bin/start-clamav
USER clamav
EXPOSE 3310
ENTRYPOINT ["/sbin/tini", "-g", "--", "/usr/local/bin/start-clamav"]
