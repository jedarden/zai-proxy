# ZAI Proxy Dashboard - Multi-stage Dockerfile
#
# Real-time web dashboard for monitoring zai-proxy metrics.
# Scrapes Prometheus endpoints, keeps a bounded in-memory history, and broadcasts via SSE.
#
# Build: docker build -t ronaldraygun/zai-proxy-dashboard:latest -f dashboard/Dockerfile .
# Run:   docker run -p 8080:8080 ronaldraygun/zai-proxy-dashboard:latest

# =============================================================================
# Stage 1: Build frontend (Node.js + Vite)
# =============================================================================
FROM node:20-alpine AS frontend

WORKDIR /app/dashboard/frontend

# Copy package files first for better caching
COPY dashboard/frontend/package.json dashboard/frontend/package-lock.json ./

# Install dependencies
RUN npm ci --prefer-offline --no-audit

# Copy source and build
COPY dashboard/frontend/ ./
RUN npm run build

# Output: frontend/dist/ with index.html and hashed assets

# =============================================================================
# Stage 2: Build Go binary
# =============================================================================
FROM golang:1.23-alpine AS backend

WORKDIR /app

# The dashboard module replaces the root zai-proxy module with ../, so build
# from the repository context rather than dashboard/ alone.
COPY . .

WORKDIR /app/dashboard

# Download the dashboard module's dependencies.
RUN go mod download

# Copy frontend build from stage 1
COPY --from=frontend /app/dashboard/frontend/dist ./frontend/dist

# Build static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /zai-proxy-dashboard .

# =============================================================================
# Stage 3: Runtime (minimal Alpine)
# =============================================================================
FROM alpine:3.19

# Install ca-certificates for HTTPS requests to scrape targets
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN adduser -D -u 1000 appuser

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
    SCRAPE_INTERVAL=5s \
    SCRAPE_TIMEOUT=3s \
    RETENTION_5S=24h \
    RETENTION_1M=168h

# Entrypoint
ENTRYPOINT ["zai-proxy-dashboard"]
