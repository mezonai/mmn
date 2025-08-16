FROM golang:1.23-bookworm AS builder

# Install necessary libraries to build RocksDB
RUN apt-get update && apt-get install -y \
  build-essential \
  libsnappy-dev \
  zlib1g-dev \
  libbz2-dev \
  libgflags-dev \
  liblz4-dev \
  libzstd-dev \
  git \
  cmake \
  wget \
  unzip

# Build RocksDB (only when DATABASE=rocksdb)
ARG DATABASE=leveldb
RUN if [ "$DATABASE" = "rocksdb" ]; then \
        git clone https://github.com/facebook/rocksdb.git && \
        cd rocksdb && \
        make static_lib && \
        make install && \
        cd .. && \
        rm -rf rocksdb; \
    fi


# Set up CGO build environment
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-I/usr/local/include"
# Set environment variables for RocksDB (only when DATABASE=rocksdb)
RUN if [ "$DATABASE" = "rocksdb" ]; then \
        echo 'export CGO_LDFLAGS="-L/usr/local/lib -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd"' >> /etc/environment; \
    fi

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Install dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build binary (leveldb by default)
RUN go build -o mmn .

# Runtime stage
FROM debian:bookworm-slim AS runtime

# Install runtime libraries needed for Go
RUN apt-get update && apt-get install -y \
  ca-certificates \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/mmn .
COPY --from=builder /app/config/ /app/config/