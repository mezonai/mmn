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

# Build RocksDB
## Option 1: Using RocksDB from source

# RUN git clone https://github.com/facebook/rocksdb.git && \
#     cd rocksdb && \
#     make static_lib && \
#     make install && \
#     cd .. && \
#     rm -rf rocksdb

## Option 2: Using pre-built RocksDB binaries
COPY libs/librocksdb.a /usr/local/lib/
COPY libs/rocksdb /usr/local/include/rocksdb


# Set up CGO build environment
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-I/usr/local/include"
ENV CGO_LDFLAGS="-L/usr/local/lib -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd"

WORKDIR /app
COPY . .
# Install dependencies
RUN go mod download

# Build binary
RUN go build -o mmn .

# Runtime stage
FROM debian:bookworm-slim AS runtime

# Install runtime libraries needed for RocksDB and Go
RUN apt-get update && apt-get install -y \
  libstdc++6 \
  libsnappy1v5 \
  zlib1g \
  libbz2-1.0 \
  liblz4-1 \
  libzstd1 \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/mmn .
COPY --from=builder /app/config/ /app/config/
COPY --from=builder /usr/local/lib/librocksdb.a /usr/local/lib/
COPY --from=builder /usr/local/include/rocksdb /usr/local/include/rocksdb
