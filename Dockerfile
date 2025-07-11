# Build stage
FROM golang:1.23.4-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o mmn ./cmd/main.go

# Runtime stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/mmn .
COPY --from=builder /app/config/ /app/config/
