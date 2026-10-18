FROM node:24-alpine AS frontend

WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN npm --prefix web ci
COPY web ./web
RUN mkdir -p internal/webui/dist && npm --prefix web run build

FROM golang:1.26-alpine AS backend

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/deck ./cmd/deck

FROM alpine:3.22

LABEL org.opencontainers.image.source="https://github.com/jmlero/the-platform" \
      org.opencontainers.image.title="Deck" \
      org.opencontainers.image.description="Human-facing work-control plane for The Platform"

RUN addgroup -S deck && adduser -S -G deck -u 10001 deck \
    && mkdir -p /data && chown deck:deck /data
COPY --from=backend /out/deck /usr/local/bin/deck

USER deck
EXPOSE 8080
VOLUME ["/data"]

ENV DECK_ADDR=:8080 \
    DECK_DATA_DIR=/data \
    DECK_LOG_LEVEL=info

ENTRYPOINT ["/usr/local/bin/deck"]
