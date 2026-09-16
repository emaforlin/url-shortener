# 001 — Lambda runtime

| Status       | Draft                                                                                                          |
| ------------ | -------------------------------------------------------------------------------------------------------------- |
| Architecture | [Lambda](../architecture.md#system-overview), [Application changes](../architecture.md#application-changes) 1–3 |
| Depends on   | —                                                                                                              |

## Summary

The service must run on AWS Lambda, invoked by an API Gateway HTTP API, and
behave exactly as it does as a container. Clients must not be able to tell which
runtime answered. Local runs and the Docker image keep working unchanged.

## Scope

**In scope:** the Lambda entry point, the construction code both entry points
share, and the Lambda build artifact.

**Out of scope:**

- AWS resources ([002](002-aws-infrastructure.md)).
- Deploying the artifact ([003](003-delivery-pipeline.md)).
- Durable storage ([004](004-persistent-storage.md)).
- Provisioned concurrency.
- Response streaming.

## Requirements

| ID     | Requirement |
| ------ | ----------- |
| LAM-01 | **Same API contract.** Every endpoint, status code, header and error envelope documented in the README's [API section](../../README.md#api) MUST behave identically in two cases: when the function is invoked with API Gateway HTTP API events (payload format 2.0), and when `cmd/api` serves the request. |
| LAM-02 | **One construction path.** Both entry points MUST build store → service → handler → router through one shared function. A change to routes, middleware or dependencies then applies to both without editing either entry point. |
| LAM-03 | **Configuration.** The function MUST load and validate configuration with the same loader as `cmd/api`. Invalid configuration MUST fail the init phase and log every problem at once. The function then serves no request. |
| LAM-04 | **Settings that don't apply.** On Lambda, `APP_PORT`, `APP_READ_TIMEOUT`, `APP_WRITE_TIMEOUT`, `APP_IDLE_TIMEOUT` and `APP_SHUTDOWN_TIMEOUT` MUST have no effect. `APP_REQUEST_TIMEOUT` and `APP_MAX_BODY_BYTES` MUST keep their current behavior. |
| LAM-05 | **Version.** `GET /healthz` MUST report the version stamped at build time, exactly as `cmd/api` does. A build without a version reports `dev`. |
| LAM-06 | **Logs.** Application logs MUST be JSON on stdout, with the same fields as the container. Every line logged while handling a request carries its `request_id`. The startup line is logged once per execution environment, during init, not once per invocation. |
| LAM-07 | **Interim store.** Until [004](004-persistent-storage.md) ships, the function uses `MemoryStore`. It MUST log a warning at init saying that links are not durable. |
| LAM-08 | **Build artifact.** `make build-lambda` MUST produce `dist/lambda.zip`, containing only an executable named `bootstrap` at the root of the zip. The binary is static, built for `linux/arm64` with the `lambda.norpc` tag. The target accepts the same `VERSION` override as `make build`, and needs neither AWS credentials nor Docker. |
| LAM-09 | **No regressions.** `make run`, `make build`, `make docker-build` and the container image MUST behave as before. |

## Acceptance criteria

Criteria 1–7 run the API's HTTP test cases twice: once through the router with
`httptest`, and once through the Lambda handler with API Gateway HTTP API v2
events. Both runs must give the same results.

1. _(automated, LAM-01)_ `POST /api/v1/links` with a valid body returns `201`,
   the JSON body, a `Location` header and an `X-Request-Id` header.
2. _(automated, LAM-01)_ `GET /{code}`, for a link created earlier in the same
   test, returns `302`, `Location` set to the target and
   `Cache-Control: no-store`.
3. _(automated, LAM-01)_ `GET /does/not/exist` returns `404` with the
   `not_found` envelope.
4. _(automated, LAM-01)_ A malformed JSON body returns `400` `bad_request`. The
   body's `request_id` equals the `X-Request-Id` response header.
5. _(automated, LAM-01)_ A body larger than `APP_MAX_BODY_BYTES` returns `413`
   `payload_too_large`.
6. _(automated, LAM-01)_ An event with `isBase64Encoded: true` and a valid
   base64 JSON body gives the same result as the plain-text event.
7. _(automated, LAM-01)_ `HEAD /healthz` returns `200` with no body.
8. _(automated, LAM-03)_ Starting the Lambda handler without `PUBLIC_BASE_URL`
   fails init with the same error text as `cmd/api`.
9. _(automated, LAM-08)_ After `make build-lambda VERSION=v9.9.9`:
   - `unzip -l dist/lambda.zip` lists exactly one entry, `bootstrap`.
   - `go version -m` on the extracted binary shows `GOOS=linux`, `GOARCH=arm64`,
     `CGO_ENABLED=0`, `-tags=lambda.norpc` and `-X main.version=v9.9.9`.
10. _(manual, LAM-05, LAM-06)_ On the first deployed invocation, `/healthz`
    reports the deployed tag. CloudWatch shows one startup line per execution
    environment, and the request lines include `request_id`.

## Edge cases

- **Percent-encoded paths.** `GET /abc%2Fdef` must give the same response as on
  `cmd/api`: `404` for an unknown code, never `500`.
- **Duplicate headers.** Payload format 2.0 joins repeated headers with commas.
  No endpoint depends on repeated headers, so this changes nothing. Keep it in
  mind before adding one that does.
- **Init failure.** Lambda retries init on the next invocation. Each failed
  attempt is logged, and API Gateway answers clients with its own generic `500`
  (see [002](002-aws-infrastructure.md#api-gateway)).
