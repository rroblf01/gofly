FROM golang:1.26-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -trimpath \
    -o /usr/local/bin/gofly ./cmd/gofly/

FROM scratch

COPY --from=builder /usr/local/bin/gofly /gofly
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY config.json /etc/gofly/config.json

EXPOSE 80 443

USER 65534:65534

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/gofly", "-config", "/etc/gofly/config.json", "-health"]

ENTRYPOINT ["/gofly"]
CMD ["-config", "/etc/gofly/config.json"]

LABEL org.opencontainers.image.source="https://github.com/rroblf01/gofly"
LABEL org.opencontainers.image.description="Minimal HTTP reverse proxy and static file server"
LABEL org.opencontainers.image.licenses="MIT"
