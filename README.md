# Notification Service (V1)

A production-quality notification service with REST API, worker pool, retry scheduler, DLQ support, and PostgreSQL persistence.

## Features

- REST API for creating and querying notifications
- Asynchronous worker pool for delivery
- Retry scheduler (polls every 1 second)
- Retry metadata stored on the notification entity
- Exponential backoff (2^retryCount seconds)
- DLQ when max retries exceeded
- Graceful shutdown via context cancellation
- PostgreSQL persistence with migrations

## Quick start

```bash
docker compose up --build
```

## API

### Create notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "user@example.com",
    "template": "WELCOME",
    "variable": {
      "name": "User"
    },
    "type": "EMAIL"
  }'
```

### Get notification

```bash
curl http://localhost:8080/api/v1/notifications/{id}
```

### Health check

```bash
curl http://localhost:8080/health
```

## Local development

```bash
# Start PostgreSQL only
docker compose up postgres -d

# Run migrations and start the app
export DATABASE_URL="postgres://notification:notification@localhost:5432/notification?sslmode=disable"
go run ./cmd/server
```

## Configuration

| Variable       | Default                                                                 | Description          |
|----------------|-------------------------------------------------------------------------|----------------------|
| `DATABASE_URL` | `postgres://notification:notification@localhost:5432/notification?sslmode=disable` | PostgreSQL DSN |
| `HTTP_ADDR`    | `:8080`                                                                 | HTTP listen address  |
| `WORKER_COUNT` | `5`                                                                     | Worker pool size     |

## Project structure

```
cmd/server/           Application entrypoint and DI wiring
internal/
  config/             Environment-based configuration
  database/           Connection pool and migrations
  domain/             Notification entity
  handler/            REST handlers
  repository/         Repository interface + PostgreSQL impl
  router/             Gin router
  service/            Business logic, worker pool, scheduler
migrations/           SQL migrations
```

## Future improvements

- Redis/Kafka for distributed job queue
- Real delivery providers (SES, Twilio, FCM) via `Sender` interface
- Outbox pattern for reliable publish-after-commit
- Idempotency keys
- Metrics and distributed tracing
- Distributed retry scheduler with leader election
