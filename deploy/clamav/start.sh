#!/bin/sh
set -eu
# Initial failure keeps the service unavailable, so Thura holds required scans.
freshclam --config-file=/etc/clamav/freshclam.conf
freshclam --config-file=/etc/clamav/freshclam.conf --daemon --foreground=true &
exec clamd --config-file=/etc/clamav/clamd.conf
