# Builds the single aidventure binary with the browser app embedded.
#   docker build -t aidventure .                      # runnable image
#   docker build --target artifact -o bin .           # just the binary, at bin/aidventure

FROM node:24.10 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.1-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/ui/static/dist ./internal/ui/static/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/aidventure ./cmd/aidventure

FROM scratch AS artifact
COPY --from=build /out/aidventure /aidventure

FROM scratch
COPY --from=build /out/aidventure /aidventure
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/aidventure"]
