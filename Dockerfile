# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git make
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/scaffold-api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/scaffold-rpc ./cmd/rpc

FROM alpine:3.20
ARG TARGETARCH=amd64
ARG GRPC_HEALTH_PROBE_VERSION=v0.4.28
RUN apk add --no-cache ca-certificates tzdata wget \
 && ARCH="${TARGETARCH}" \
 && case "$ARCH" in amd64|arm64) ;; *) echo "unsupported TARGETARCH=$ARCH"; exit 1 ;; esac \
 && BASE="https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}" \
 && wget -qO /tmp/checksums.txt "${BASE}/checksums.txt" \
 && wget -qO /bin/grpc_health_probe "${BASE}/grpc_health_probe-linux-${ARCH}" \
 && SUM="$(awk -v f="grpc_health_probe-linux-${ARCH}" '$2==f || $2=="*"f {print $1; exit}' /tmp/checksums.txt)" \
 && test -n "$SUM" \
 && echo "${SUM}  /bin/grpc_health_probe" | sha256sum -c - \
 && chmod +x /bin/grpc_health_probe \
 && rm -f /tmp/checksums.txt \
 && apk del wget
WORKDIR /app
COPY --from=builder /out/scaffold-api /out/scaffold-rpc ./
COPY config ./config
RUN adduser -D -H -u 10001 scaffold \
 && chown -R scaffold:scaffold /app
USER scaffold
ENV APP_ENV=prod
EXPOSE 8080 8081
# 镜像同时包含 scaffold-api 与 scaffold-rpc；默认只启动 api。
# 生产须两个进程/两个容器：rpc 使用 CMD ["./scaffold-rpc", "-f", "config"]（见 deploy/）。
CMD ["./scaffold-api", "-f", "config"]
