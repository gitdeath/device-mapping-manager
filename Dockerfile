# syntax=docker/dockerfile:1

# Build Stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy and download dependencies first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Run tidy to ensure modules are in sync with the source code.
RUN go mod tidy

# Build the static binary
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags "-linkmode external -extldflags -static" -o /dvd .

# Final Stage
FROM alpine:latest

COPY --from=builder /app/dvd /dvd

ENTRYPOINT ["/dvd"]
