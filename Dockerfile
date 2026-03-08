# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o filearchiver .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS support
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -S archiver && adduser -S archiver -G archiver

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /build/filearchiver /app/filearchiver

# Create directories for mounting
RUN mkdir -p /data/input /data/output /config
RUN chown -R archiver:archiver /data /config

# Volume for persistent data
VOLUME ["/config"]

# Set the working directory to /data for database and lock files
WORKDIR /config

USER archiver

ENTRYPOINT ["/app/filearchiver"]
CMD ["--help"]
