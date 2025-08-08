# Build stage
FROM golang:1.23.8 AS builder
WORKDIR /app
COPY . .

# Static binary build for Alpine
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o mmn ./cmd/main.go

# Runtime stage
FROM alpine:latest
WORKDIR /app

# Copy statically linked binary and config
COPY --from=builder /app/mmn .
COPY --from=builder /app/config/ /app/config/

CMD ["./mmn"]
