#!/usr/bin/env sh
# docker-entrypoint.sh
#
# Runs as root, ensures the data volume is owned by the technician user,
# then drops privileges via gosu before exec'ing the binary. CAP_NET_RAW
# remains effective because it is set as a file capability on the binary.
#
# This is idempotent and fast on already-correct volumes — chown skips
# inodes whose ownership already matches.
set -eu

DATA_DIR="${TECHNICIAN_DATA_DIR:-/var/lib/technician}"

if [ "$(id -u)" = "0" ]; then
    if [ -d "$DATA_DIR" ]; then
        chown -R --from=root:root technician:technician "$DATA_DIR" 2>/dev/null || \
            chown -R technician:technician "$DATA_DIR"
    fi
    exec gosu technician:technician /usr/local/bin/technician "$@"
fi

exec /usr/local/bin/technician "$@"
