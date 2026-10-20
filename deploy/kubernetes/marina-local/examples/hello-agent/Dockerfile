FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/hello-agent .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hello-agent /hello-agent
# The engine's egress proxy presents certificates from this bundle.
ENV SSL_CERT_FILE=/run/kagent/egress/trust-bundle.pem
ENTRYPOINT ["/hello-agent"]
