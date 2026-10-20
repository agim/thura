#!/bin/sh
set -eu
valkey-server --bind 127.0.0.1 --port 6379 --save '' --appendonly no --maxmemory 128mb --maxmemory-policy noeviction --dir /tmp --daemonize no &
cache_pid=$!
unbound -d -c /etc/unbound-evaluation.conf &
dns_pid=$!
rspamd -f -u _rspamd -g _rspamd &
filter_pid=$!
trap 'kill "$filter_pid" "$cache_pid" "$dns_pid" 2>/dev/null || true' EXIT INT TERM
while kill -0 "$cache_pid" 2>/dev/null && kill -0 "$filter_pid" 2>/dev/null && kill -0 "$dns_pid" 2>/dev/null; do sleep 1; done
exit 1
