# aidventure

A system for long, complex AI roleplay. Go backend that talks to the models and
arranges the play; TypeScript single-page app for the browser. The repository
builds into one binary that serves everything.

## Layout

- [adr](adr) - contains important project directions that must always hold.
- [aidventure](cmd/aidventure) - server entry point
- [internal](internal) - server runtime
  - [config](internal/config) - runtime settings and where data lives
  - [store](internal/store) - database and migrations
- [web](web) - the web app source (Vite + TypeScript + React)

## Develop

Everything runs in containers. Once: `cp .env.example .env` and set your uid/gid
so bind-mounted files stay yours, and install the web dependencies as described
in [web](web). Then:

    docker compose up

Open http://localhost:8080. The Go server proxies the UI to Vite, so edits under
`web/` hot-reload in the browser; edits under `cmd/` or `internal/` rebuild and
restart the Go binary (see [dev/api.sh](dev/api.sh)). The dev binary and its
data live in `bin/dev/`.

## Build

    docker build --target artifact -o bin .

produces `bin/aidventure`, a static binary with the web app inside.
`docker build -t aidventure .` gives the same binary as a scratch image.
