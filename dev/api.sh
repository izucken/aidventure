#!/bin/sh
# Development only: runs the server and rebuilds it whenever Go sources
# change. Polls mtimes rather than watching inotify so it needs nothing the
# Go image lacks, and lives in the service itself so a plain `compose up`
# hot-reloads without `--watch`. A failed build keeps the last binary running.
set -u
cd /src
bin=bin/dev/aidventure
stamp=/tmp/aidventure.built
pid=

build() {
  touch "$stamp"
  go build -o "$bin" ./cmd/aidventure
}

start() {
  "$bin" &
  pid=$!
}

stop() {
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
    pid=
  fi
}

trap 'stop; exit 0' INT TERM

if build; then start; fi
while sleep 2; do
  changed=$(find cmd internal go.mod go.sum -path internal/ui/static/dist -prune -o -newer "$stamp" -print -quit)
  [ -z "$changed" ] && continue
  echo "dev: $changed changed, rebuilding"
  if build; then
    stop
    start
  fi
done
