FROM docker.io/library/golang:1.23-alpine AS builder

WORKDIR /app
COPY go.mod go.sum* ./
COPY *.go ./
COPY VERSION ./
# Build with version, commit, and build time info
RUN go mod download && \
    VERSION=$(cat VERSION 2>/dev/null || echo "unknown") && \
    COMMIT=${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "unknown")} && \
    BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) && \
    CGO_ENABLED=0 GOOS=linux go build -mod=mod \
        -ldflags="-s -w -X main.buildVersion=$VERSION -X main.buildCommit=$COMMIT -X main.buildTimeStr=$BUILD_TIME" \
        -o zai-proxy .

FROM docker.io/library/alpine:3.19

LABEL maintainer="ardenone"
LABEL description="Z.AI API proxy with token injection and Prometheus metrics"

RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -u 1000 proxyuser

COPY --from=builder /app/zai-proxy /zai-proxy

USER proxyuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/zai-proxy"]
