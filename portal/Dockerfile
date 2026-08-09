# Build from monorepo root:
#   docker build -f portal/Dockerfile -t portal .
# (portal/go.mod replaces framework with ../framework)

FROM golang:1.26 AS builder

WORKDIR /src

COPY framework /src/framework
COPY portal /src/portal

WORKDIR /src/portal

ENV GOPROXY=https://goproxy.cn,direct
ENV CGO_ENABLED=0

RUN go build -ldflags "-X main.Version=docker" -o /out/backend ./cmd/backend

FROM debian:stable-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
		ca-certificates \
		netbase \
		&& rm -rf /var/lib/apt/lists/* \
		&& apt-get autoremove -y && apt-get autoclean -y

COPY --from=builder /out/backend /app/backend

WORKDIR /app

EXPOSE 8000
EXPOSE 9000
VOLUME /data/conf

CMD ["./backend", "-conf", "/data/conf"]
