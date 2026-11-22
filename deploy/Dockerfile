FROM golang:1.27-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/sekailink-relay ./cmd/sekailink-relay

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && mkdir -p /data/certs
COPY --from=builder /out/sekailink-relay /usr/local/bin/sekailink-relay
ENV SEKAILINK_NODE_ID=SV-Docker \
    SEKAILINK_LISTEN=:25590 \
    SEKAILINK_CERT_DIR=/data/certs
EXPOSE 25590/tcp 25600-25700/tcp 25600-25700/udp
ENTRYPOINT ["sekailink-relay", "--port-min", "25600", "--port-max", "25700"]
