FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -trimpath \
    -o /usr/local/bin/gofly ./cmd/gofly/

FROM scratch

COPY --from=builder /usr/local/bin/gofly /gofly
COPY config.json /etc/gofly/config.json

EXPOSE 80 443

USER 65534:65534

ENTRYPOINT ["/gofly"]
CMD ["-config", "/etc/gofly/config.json"]
