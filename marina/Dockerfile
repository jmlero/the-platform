FROM golang:1.26.5-alpine3.24 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN mkdir -p /out/data && \
    CGO_ENABLED=0 GOOS=linux go build \
      -buildvcs=false \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/marina \
      ./cmd/marina

FROM scratch

LABEL org.opencontainers.image.source="https://github.com/jmlero/the-platform" \
      org.opencontainers.image.title="The Marina" \
      org.opencontainers.image.description="Always-on autonomous software factory control service"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/marina /marina

USER 65532:65532

ENV MARINA_ADDR=:8090 \
    MARINA_JOURNAL_PATH=/data/marina.db

EXPOSE 8090

ENTRYPOINT ["/marina"]
CMD ["serve"]
