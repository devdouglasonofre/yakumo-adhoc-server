#!/bin/sh
set -eu

/usr/local/bin/yakumo-adhoc-server &
server_pid=$!

/usr/local/bin/yakumo-adhoc-gateway &
gateway_pid=$!

stop() {
    kill "$server_pid" "$gateway_pid" 2>/dev/null || true
    wait "$server_pid" "$gateway_pid" 2>/dev/null || true
}
trap stop TERM INT

while kill -0 "$server_pid" 2>/dev/null && kill -0 "$gateway_pid" 2>/dev/null; do
    sleep 1
done

stop
exit 1
