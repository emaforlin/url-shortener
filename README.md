# url-shortener

A small URL shortening service written in Go. It turns long URLs into short
links and redirects anyone who opens them. It uses only the standard library
plus `godotenv`.

- Short codes are generated at random (7 base62 characters), or you can choose your own.
- Links can expire after a TTL you set.
- Every response carries an `X-Request-Id`, and errors use a single JSON shape.
- Separate liveness and readiness probes, plus graceful shutdown on `SIGTERM`.
- Logs are structured: readable text in development, JSON in production.

> **Status:** links are stored in memory, so they are lost on every restart. See
> [Current limitations](#current-limitations).

## Quick start

Requires Go 1.27.1 or newer.

```sh
cp .env.example .env
make run
```

```sh
# Shorten a URL
curl -i -X POST http://localhost:8080/api/v1/links \
  -H 'Content-Type: application/json' \
  -d '{"url": "https://example.com/a/very/long/path"}'

# Follow the short link
curl -i http://localhost:8080/aZ3kP9q
```

## API

### `POST /api/v1/links`

Creates a short link.

| Field         | Type   | Required | Description                                          |
| ------------- | ------ | -------- | ---------------------------------------------------- |
| `url`         | string | yes      | Destination. Absolute `http`/`https`, max 2048 chars. |
| `code`        | string | no       | Custom code: 4–32 letters or digits.                 |
| `ttl_seconds` | int    | no       | Expire the link after this many seconds. `0` or absent means never. |

**`201 Created`**, with a `Location` header pointing at the short link:

```json
{
  "code": "aZ3kP9q",
  "short_url": "http://localhost:8080/aZ3kP9q",
  "target_url": "https://example.com/a/very/long/path",
  "expires_at": "2026-09-14T12:00:00Z"
}
```

`expires_at` is left out for links that never expire.

The request body is parsed strictly:

- If a `Content-Type` is sent, it must be `application/json`.
- Unknown fields are rejected.
- The body must be a single JSON object no larger than `APP_MAX_BODY_BYTES`.
- The target URL cannot point back at the service's own host, so a short link can't lead to another short link.

| Status | Code                | When                                             |
| ------ | ------------------- | ------------------------------------------------ |
| 400    | `bad_request`       | Malformed JSON, invalid URL, invalid code, negative TTL |
| 409    | `conflict`          | The custom `code` is already taken               |
| 413    | `payload_too_large` | Body exceeds `APP_MAX_BODY_BYTES`                |
| 415    | `bad_request`       | `Content-Type` is not `application/json`         |

### `GET /{code}`

Redirects to the target with **`302 Found`** and `Cache-Control: no-store`. A
`301` would stay in browser caches indefinitely, and then a link could never be
changed or revoked. Each successful redirect adds one to the link's hit counter.

| Status | Code        | When                        |
| ------ | ----------- | --------------------------- |
| 404    | `not_found` | No link exists for the code |
| 410    | `gone`      | The link has expired        |

### `GET /healthz`

Liveness probe. Returns `200` as long as the process can serve requests and
never checks dependencies.

```json
{ "status": "ok", "version": "v0.1.0" }
```

### `GET /readyz`

Readiness probe. Pings the store and returns `200 {"status":"ready"}`, or
`503` when the store can't be reached. A failing store takes the instance out
of the load balancer without restarting it.

### Errors

Every failure uses the same envelope, including unknown routes, timeouts and
recovered panics:

```json
{
  "error": {
    "code": "not_found",
    "message": "short link not found",
    "request_id": "3f9c2a7e5b1d4c8fa0e6b2d9c4f1a7e3"
  }
}
```

Clients should branch on `code`, not on `message`, which may change. Beyond the
codes listed above, `timeout` means the request exceeded `APP_REQUEST_TIMEOUT`,
and `internal_error` covers everything else. Details of an internal error are
only logged, never returned.

### Request IDs

The service reuses an incoming `X-Request-Id` if it has at most 64 characters
and they are all printable ASCII. Otherwise it generates a new one. The ID is
echoed in the response header, included in error bodies, and attached to every
log line for that request.

## Configuration

All configuration comes from environment variables and is validated at
startup. If anything is invalid, the process exits and lists every problem at
once.

| Variable               | Default       | Description                                                  |
| ---------------------- | ------------- | ------------------------------------------------------------ |
| `PUBLIC_BASE_URL`      | *(required)*  | Public origin used to build short links, e.g. `https://sho.rt` |
| `APP_ENV`              | `development` | `development` or `production`                                |
| `APP_PORT`             | `8080`        | Port the HTTP server listens on                              |
| `LOG_LEVEL`            | `info`        | `debug`, `info`, `warn` or `error`                           |
| `APP_READ_TIMEOUT`     | `5s`          | Time limit for reading the request                           |
| `APP_WRITE_TIMEOUT`    | `10s`         | Time limit for writing the response                          |
| `APP_IDLE_TIMEOUT`     | `120s`        | How long an idle keep-alive connection stays open            |
| `APP_REQUEST_TIMEOUT`  | `8s`          | Time limit for a handler. Must be less than `APP_WRITE_TIMEOUT`. |
| `APP_SHUTDOWN_TIMEOUT` | `15s`         | How long in-flight requests get to finish on shutdown        |
| `APP_MAX_BODY_BYTES`   | `65536`       | Maximum request body size                                    |

Durations use Go syntax: `200ms`, `5s`, `1m`.

### `.env` files

A `.env` file in the working directory is read **only in development**.
Variables already set in the environment take precedence over the file.

In production, the environment is the only source of configuration:

- If `APP_ENV=production` and a `.env` file exists, startup is aborted.
- A `.env` file that sets `APP_ENV=production` is rejected.

This keeps a stale file baked into an image from silently overriding the
deployment's configuration.

## Development

| Command            | Description                                       |
| ------------------ | ------------------------------------------------- |
| `make run`         | Run the service from source                       |
| `make build`       | Compile to `bin/api`                              |
| `make test`        | Run the tests                                     |
| `make test-race`   | Run the tests with the race detector              |
| `make cover`       | Generate `coverage.html`                          |
| `make fmt`         | Format the code with `gofmt`                      |
| `make lint`        | Run `golangci-lint`, or `go vet` if it isn't installed |
| `make check`       | fmt-check, vet, lint and race tests — what CI runs |
| `make help`        | List all targets                                  |

The build stamps a version into the binary from `git describe`. You can
override it with `make build VERSION=1.4.2`. The version appears in the startup
log and in `/healthz`.

## Docker

The image is built in two stages and produces a static binary on `scratch`,
which runs as a non-root user (`65532`) and listens on port 8080.

```sh
make docker-build

docker run --rm -p 8080:8080 \
  -e PUBLIC_BASE_URL=http://localhost:8080 \
  api:latest
```

The image sets `APP_ENV=production` by default, so logs are JSON and no `.env`
file is read. Because `scratch` has no shell, the image has no `HEALTHCHECK`.
Point your orchestrator's probes at `/healthz` (liveness) and `/readyz`
(readiness).

## Deployment notes

- Set `PUBLIC_BASE_URL` to the domain users will actually see. Short links are built from it.
- The server handles `SIGINT`/`SIGTERM` by draining in-flight requests for up to `APP_SHUTDOWN_TIMEOUT`. The orchestrator's termination grace period should be longer than that.
- **Run a single replica for now.** With the in-memory store, each instance holds its own links, so a link created on one replica would return 404 on the others.

## Project layout

```txt
cmd/api/            entry point: dependency wiring, HTTP server, graceful shutdown
internal/config/    loads and validates configuration from the environment
internal/logging/   slog setup and a request-scoped logger carried in the context
internal/links/     domain: Link entity, Store interface, Service rules, in-memory store
internal/api/       HTTP transport: routes, handlers, middleware, error mapping
```

Dependencies only point inward. `links` doesn't import `net/http`, and handlers
only talk to `links.Service`. Moving to a database means writing a new
`links.Store` implementation and changing a single line in `cmd/api/main.go`.

Each request passes through middleware in this order: request ID, access log,
panic recovery, request timeout, body size limit.

## Current limitations

- **No persistence.** `MemoryStore` loses all links on restart and can't be shared between instances.
- **No endpoints for deleting links or reading stats.** `Store.Delete` exists and hits are counted, but neither is exposed over HTTP.
- **No authentication or rate limiting.** Anyone who can reach the service can create links.
