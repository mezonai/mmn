# MMN Server Simulator

A server simulator for testing MMN (Mezon) blockchain functionality.

## Prerequisites

- Docker and Docker Compose
- Go 1.19 or higher
- PostgreSQL (via Docker)

## Quick Start

### 1. Navigate to the project directory

```bash
cd client_test/mezon-server-sim
```

### 2. Start the database

```bash
docker compose up -d
```

## Running Tests

After setting up the database and seed data, you can run the integration tests:

```bash
go test ./...
```