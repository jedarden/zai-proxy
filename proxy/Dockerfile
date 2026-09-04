FROM docker.io/library/golang:1.23-alpine AS builder

# go.mod/go.sum moved to the repo root in 91746e7 (module consolidation);
# this Dockerfile now needs the full repo as its build context (not just
# proxy/), so the CI template passes --context=dir:///workspace and
# --dockerfile=proxy/Dockerfile, no --context-sub-path.
WORKDIR /app
COPY go.mod go.sum ./
COPY internal/ ./internal/
COPY proxy/ ./proxy/
# Build with version, commit, and build time info
RUN go mod download && \
    VERSION=$(cat proxy/VERSION 2>/dev/null || echo "unknown") && \
    COMMIT=${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "unknown")} && \
    BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) && \
    CGO_ENABLED=0 GOOS=linux go build -mod=mod -buildvcs=false \
        -ldflags="-s -w -X main.buildVersion=$VERSION -X main.buildCommit=$COMMIT -X main.buildTimeStr=$BUILD_TIME" \
        -o /app/zai-proxy ./proxy

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
