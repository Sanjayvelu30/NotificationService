# 📓 Backend Systems Troubleshooting & Architectural Decisions Log

This journal documents the advanced engineering challenges, database conflicts, and network limitations encountered during the development and deployment of the Go Notification Monolith, along with their root causes and resolutions.

---

## 1. PgBouncer Transaction Pooler vs. Prepared Statement Cache
* **Symptom**: `ERROR: prepared statement "stmtcache_..." already exists (SQLSTATE 42P05)` or `does not exist`.
* **Context**: Deployed to Render connecting to Supabase PostgreSQL connection pooler (Port `6543`) in Transaction Mode.
* **Root Cause**: Go’s `pgx` driver defaults to **Extended Query Protocol**, compiling queries into named prepared statements and caching their OIDs on the connection to optimize speed. In **Transaction Mode**, the PgBouncer proxy routes consecutive queries to different random physical database connections. When Go attempts to execute a cached statement, it hits a connection that has no compilation context of that statement, causing a cash mismatch/collision.
* **Resolution**: 
  1. Configured the Go database pool to execute using **Simple Protocol** (`QueryExecModeSimpleProtocol`), which sends raw SQL directly in a single step without caching statements.
  2. Appended `?statement_cache_mode=describe` to the migration connection string to prevent statement cache compilation inside the database migration engine.

---

## 2. Render Cloud Network Outbound IPv6 Limits
* **Symptom**: `connect: network is unreachable` on port `5432`.
* **Context**: Attempting to bypass PgBouncer and connect directly to Supabase's direct host (`db.xxx.supabase.co`).
* **Root Cause**: Render’s serverless hosting infrastructure only supports **IPv4 outbound routing** on standard nodes. Supabase's direct connection host resolves primarily to an **IPv6 address**. The Go server was unable to route packets to the IPv6 address, resulting in unreachable TCP sockets.
* **Resolution**: Routed connections through the Supabase pooler host (`aws-0-xxx.pooler.supabase.com`) which resolves to an IPv4 address, but switched the port to **`5432`** (Session Mode). This allows the application to communicate directly with PostgreSQL (bypassing PgBouncer constraints) over a compatible IPv4 network channel.

---

## 3. JSONB Parameter Serialization under Simple Protocol
* **Symptom**: `save notification: ERROR: invalid input syntax for type json (SQLSTATE 22P02)`.
* **Context**: Storing template variables map (`map[string]string`) in a PostgreSQL `JSONB` column.
* **Root Cause**: When executing queries under **Simple Protocol**, `pgx` does not perform OID type-negotiation with the database. Because Go's `json.Marshal` returns a `[]byte` type, `pgx` default-serializes this parameter as a **hex-encoded binary string** (e.g. `\x7b22...`). PostgreSQL receives the hex payload and fails to cast it to `JSONB`, throwing a syntax error.
* **Resolution**: Modified the Go repositories to convert marshaled JSON bytes into Go **strings** (`string(varBytes)`) before binding to query parameters. PostgreSQL automatically and successfully parses text parameters into `JSONB` values.

---

## 4. UI High-Frequency Polling vs. API Rate Limiter Lockouts
* **Symptom**: Dashboard alerts print `rate limit exceeded: max requests reached for this period` and the logs terminal freezes.
* **Context**: Rate limiter config is set to 20 requests per 24 hours. The dashboard console polls `/api/v1/notifications/:id` every 1 second.
* **Root Cause**: The `RateLimitMiddleware` was applied universally across the entire `/api/v1` route group. As a result, the 1-second status polling loop consumed the client's entire 20-request daily rate quota in exactly 20 seconds, locking the developer out of the system.
* **Resolution**: Restructured route registration in `router.New()` to isolate read operations from write mutations:
  * **Non-rate-limited**: Read endpoints (`GET /api/v1/notifications/:id` and `GET /api/v1/templates`) are free of the limiter, enabling stable UI polling.
  * **Rate-limited**: Mutation endpoints (`POST /api/v1/notifications` and `POST /api/v1/templates`) remain rate-limited, protecting downstream partner budget limits from abuse.
