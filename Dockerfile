# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies with retry
RUN apk update && apk add --no-cache git make || \
    (sleep 5 && apk update && apk add --no-cache git make)

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
ARG VERSION=dev
ARG BUILD_TIME
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -s -w" \
    -o oelala-storage \
    ./cmd/oelala-storage

# Runtime stage
FROM alpine:latest

# Install runtime dependencies with retry and update
RUN apk update && apk --no-cache add ca-certificates tzdata wget || \
    (sleep 5 && apk update && apk --no-cache add ca-certificates tzdata wget)

# Create non-root user
RUN addgroup -g 1000 oelala && \
    adduser -D -u 1000 -G oelala oelala

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/oelala-storage /app/

# Create data directory
RUN mkdir -p /data && chown -R oelala:oelala /data /app

# Switch to non-root user
USER oelala

# Expose ports
EXPOSE 7990 7991

# Volume for data
VOLUME ["/data"]

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:7990/health || exit 1

# Run the binary
# Note: The application uses sensible defaults when no config file is provided
# To use a custom config, mount it at: -v /path/to/config.yaml:/app/oelala-storage.yaml
ENTRYPOINT ["/app/oelala-storage"]
CMD ["serve"]
