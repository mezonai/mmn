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

### 3. Database Setup

Create the required database table and seed data:

```sql
-- Create users table
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    -- Add other columns as needed
);

-- Insert seed data
INSERT INTO users (id) VALUES (0), (1), (2);
```

## Running Tests

After setting up the database and seed data, you can run the integration tests:

```bash
go test ./...
```