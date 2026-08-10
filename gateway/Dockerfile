# Build from monorepo root:
#   docker build -f gateway/Dockerfile -t gateway .
# (gateway is a self-contained Go module under ./gateway)

FROM golang:1.26 AS builder

WORKDIR /src

COPY gateway /src/gateway

WORKDIR /src/gateway

ENV GOPROXY=https://goproxy.cn,direct
ENV CGO_ENABLED=0

RUN go build -ldflags "-X main.Version=docker" -o /out/gateway ./cmd/gateway

FROM debian:stable-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
		ca-certificates \
		netbase \
		curl \
		&& rm -rf /var/lib/apt/lists/* \
		&& apt-get autoremove -y && apt-get autoclean -y

COPY --from=builder /out/gateway /app/gateway
COPY gateway/configs /app/configs
COPY deploy/gateway/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

WORKDIR /app

EXPOSE 8088

ENTRYPOINT ["/app/docker-entrypoint.sh"]
