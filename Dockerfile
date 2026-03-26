# Stage 1: Build Go binary
FROM golang:1.25-bookworm AS go-builder

WORKDIR /build

# Download dependencies first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o contractanalyzer .

# Stage 2: Install Node.js bridge dependencies
FROM node:22-slim AS node-builder

WORKDIR /build

# Copy package files and install dependencies
COPY opencode-bridge/package.json opencode-bridge/package-lock.json* ./
RUN npm ci --production || npm install --production

# Stage 3: Runtime image
FROM node:22-slim

WORKDIR /app

# Install wget for healthchecks
RUN apt-get update && apt-get install -y --no-install-recommends wget && \
    rm -rf /var/lib/apt/lists/*

# Copy Go binary from builder
COPY --from=go-builder /build/contractanalyzer .

# Copy Node.js bridge and dependencies
COPY --from=node-builder /build/node_modules ./opencode-bridge/node_modules
COPY opencode-bridge/bridge.mjs opencode-bridge/package.json ./opencode-bridge/

# Copy application assets
COPY templates/ ./templates/
COPY static/ ./static/
COPY prompts/ ./prompts/

# Create data directory
RUN mkdir -p /app/data

EXPOSE 8080

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:8080/ || exit 1

CMD ["./contractanalyzer"]
