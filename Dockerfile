FROM golang:1.27.0-alpine3.24 AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

RUN apk add --no-cache ca-certificates

WORKDIR /src

COPY go.mod ./
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION}" -trimpath \
    -o /usr/local/bin/gofly ./cmd/gofly/

FROM scratch

COPY --from=builder /usr/local/bin/gofly /gofly
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 80 443

USER 65534:65534

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/gofly", "-health", "-port", "80"]

ENTRYPOINT ["/gofly"]
CMD ["-root", "/www"]

LABEL org.opencontainers.image.source="https://github.com/rroblf01/gofly"
LABEL org.opencontainers.image.description="Minimal HTTP reverse proxy and static file server"
LABEL org.opencontainers.image.licenses="MIT"
