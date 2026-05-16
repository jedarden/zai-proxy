# ZAI Proxy Dashboard - Multi-stage Dockerfile
#
# Real-time web dashboard for monitoring zai-proxy metrics.
# Scrapes Prometheus endpoints, stores in SQLite, broadcasts via SSE.
#
# Build: docker build -t ronaldraygun/zai-proxy-dashboard:latest .
# Run:   docker run -p 8080:8080 ronaldraygun/zai-proxy-dashboard:latest

# =============================================================================
# Stage 1: Build frontend (Node.js + Vite)
# =============================================================================
FROM node:20-alpine AS frontend

WORKDIR /app/frontend

# Copy package files first for better caching
COPY frontend/package.json frontend/package-lock.json ./

# Install dependencies
RUN npm ci --prefer-offline --no-audit

# Copy source and build
COPY frontend/ ./
RUN npm run build

# Output: frontend/dist/ with index.html and hashed assets

# =============================================================================
# Stage 2: Build Go binary
# =============================================================================
FROM golang:1.23-alpine AS backend

WORKDIR /app

# Copy go.mod first
COPY go.mod ./

# Copy source code first (needed for go mod tidy to work)
COPY . .

# Download dependencies and generate go.sum if needed
RUN go mod tidy && go mod download

# Copy frontend build from stage 1
COPY --from=frontend /app/frontend/dist ./frontend/dist

# Build static binary (CGO_ENABLED=0 works with modernc.org/sqlite pure Go)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /zai-proxy-dashboard .

# =============================================================================
# Stage 3: Runtime (minimal Alpine)
# =============================================================================
FROM alpine:3.19

# Install ca-certificates for HTTPS requests to scrape targets
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN adduser -D -u 1000 appuser

# Create data directory for SQLite
RUN mkdir -p /data && chown appuser:appuser /data

WORKDIR /app

# Copy binary from stage 2
COPY --from=backend /zai-proxy-dashboard /usr/local/bin/zai-proxy-dashboard

# Set ownership
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/status || exit 1

# Default environment variables
ENV LISTEN_ADDR=:8080 \
    DB_PATH=/data/dashboard.db \
    SCRAPE_INTERVAL=5s \
    SCRAPE_TIMEOUT=3s \
    RETENTION_5S=24h \
    RETENTION_1M=168h

# Entrypoint
ENTRYPOINT ["zai-proxy-dashboard"]
