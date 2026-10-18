#!/bin/sh
# Starts OpenBao's in-memory dev server, configures it for Rotor, and stays in the foreground with it.
set -eu

rm -f /tmp/rotor-configured
bao server -dev -dev-listen-address=0.0.0.0:8200 &
server=$!
trap 'kill "$server" 2>/dev/null' TERM INT

tries=0
until bao status >/dev/null 2>&1; do
  tries=$((tries + 1))
  [ "$tries" -lt 60 ] || { echo "OpenBao did not answer within a minute" >&2; exit 1; }
  sleep 1
done

sh /rotor/init.sh
touch /tmp/rotor-configured
wait "$server"
