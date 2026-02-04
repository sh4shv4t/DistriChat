# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /distribchat

# Final stage
FROM alpine:3.19

WORKDIR /app

# Copy binary from builder
COPY --from=builder /distribchat .

# Expose gRPC ports
EXPOSE 50051 50052 50053

# Run the simulation
CMD ["./distribchat"]
