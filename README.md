# Notification Engine Monolith (V2 SaaS)

A highly resilient, production-grade Go notification scheduler and queue service featuring multi-tenant Auth0 isolation, PostgreSQL concurrency primitives, exponential backoff retries, and real-time dashboard analytics.

---

## ⚡ Key Technical Features

### 1. Database Concurrency & Worker Pools
- **Atomic Queue Acquisition**: Employs PostgreSQL transactional queries with `FOR UPDATE SKIP LOCKED` locking clauses to execute task claiming safely across horizontal worker nodes.
- **Partial Indexing**: Leverages targeted database indexes (`WHERE status = 'PENDING'`) to limit search ranges, keeping index sizes small and RAM-resident even at millions of logs.

### 2. Distributed Resiliency
- **Exponential Backoff**: Delays retries dynamically ($2^{\text{retry\_count}}$ seconds) on transit delivery faults to protect downstream partner servers.
- **Dead Letter Queue (DLQ)**: Quarantines persistent failures in an isolated archival table for diagnostic logs audits.

### 3. Identity & API Security
- **Auth0 Multitenancy**: Restricts template queries and notification audits securely. Users can only view or manage records owned by their Auth0 sub claims.
- **JWT Validation Cache**: Uses a local thread-safe `sync.Map` validation cache to save user profile validations, preventing latency penalties.
- **User-Scoped Rate Limiting**: Scopes Fixed Window rate limiter tokens directly to the authenticated user ID (falling back to client IP).

### 4. Interactive Console Developer UI
- Uses Go's native `go:embed` filesystem utility to deliver a self-contained monolith build package.
- **Quota Progress Gauge**: Renders live rate-limit utilization states dynamically.
- **Live Terminal Console**: Streams real-time scheduler state log entries directly to the dashboard.

---

## 🚀 Quick Start

1. Create a local `.env` file (copied from `.env.example`) and inject your Resend API Key:
   ```env
   RESEND_API_KEY=re_MfLT8nBm_...
   AUTH0_DOMAIN=dev-i6avz7x124upwug6.us.auth0.com
   ```
2. Build and start the container stack:
   ```bash
   docker compose up --build
   ```
3. Open your browser in an **Incognito Window** (to clear old redirect caches) and visit:
   👉 **`http://localhost:8080/`**

---

## 🛠 Project Structure

```text
cmd/server/           Application entrypoint and DI wiring
internal/
  config/             Configuration schema loading
  database/           Connection pools and migration triggers
  domain/             Core entity representations (Notification, Template)
  handler/            REST controllers (Notification, Template)
  middleware/         Auth0 verification & cache validation layer
  ratelimit/          Fixed Window middleware scoped by User ID
  repository/         Postgres client mapping interfaces
  router/             Gin router mapping static and protected streams
  service/            Background concurrency workers & retry scheduler
migrations/           Postgre SQL migration files (001 to 004)
```

---

## ⚙️ Configuration Variables

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://notification:...@postgres:5432/...` | PostgreSQL connection DSN |
| `HTTP_ADDR` | `:8080` | Server HTTP binding address |
| `WORKER_COUNT` | `5` | Number of concurrent poller workers |
| `RATE_LIMIT_MAX` | `20` | Requests allowed per rate limit window |
| `RATE_LIMIT_DURATION` | `24h` | Rate limiter window length |
| `AUTH0_DOMAIN` | `dev-i6avz7x124upwug6.us.auth0.com` | Auth0 domain endpoint |
| `RESEND_API_KEY` | | Partner API key for email delivery |
