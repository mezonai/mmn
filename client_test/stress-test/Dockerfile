FROM golang:1.24-bookworm 

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Install dependencies
RUN go mod download
