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
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/appdeck ./cmd/appdeck

FROM alpine:3.22

LABEL org.opencontainers.image.source="https://github.com/jmlero/the-platform" \
      org.opencontainers.image.title="AppDeck" \
      org.opencontainers.image.description="Human-facing work-control plane for The Platform"

RUN addgroup -S appdeck && adduser -S -G appdeck -u 10001 appdeck \
    && mkdir -p /data && chown appdeck:appdeck /data
COPY --from=backend /out/appdeck /usr/local/bin/appdeck

USER appdeck
EXPOSE 8080
VOLUME ["/data"]

ENV APPDECK_ADDR=:8080 \
    APPDECK_DATA_DIR=/data \
    APPDECK_DEMO_AGENT=true \
    APPDECK_LOG_LEVEL=info

ENTRYPOINT ["/usr/local/bin/appdeck"]
