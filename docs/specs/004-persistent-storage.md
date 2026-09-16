# 004 — Persistent storage

| Status       | Draft |
| ------------ | ----- |
| Architecture | [Data store: DynamoDB](../architecture.md#data-store-dynamodb), [Application changes](../architecture.md#application-changes) 6 |
| Depends on   | [002](002-aws-infrastructure.md) |

## Summary

Links survive restarts, deploys and rollbacks, and every Lambda execution
environment sees the same links. `DynamoStore` implements `links.Store` with the
behavior clients already get from `MemoryStore`. What's documented here is only
what a database changes: consistency, concurrency, expiry precision and
failures.

## Scope

**In scope:** `DynamoStore`, choosing a store through configuration, local
development and CI against DynamoDB Local, and the item format.

**Out of scope:**

- New endpoints: deleting links, stats.
- Backups and point-in-time recovery. The data is portfolio data, and losing it
  is acceptable.
- Migrating links from `MemoryStore`. They don't survive a restart anyway.

## Requirements

| ID     | Requirement |
| ------ | ----------- |
| STO-01 | **Durability.** A link created through the API MUST resolve from any execution environment, and after any deploy, rollback or configuration redeploy, until it expires. |
| STO-02 | **One contract.** `MemoryStore` and `DynamoStore` MUST pass the same contract test suite. It covers every `Store` method, including the `ErrAlreadyExists` and `ErrNotFound` results, which callers match with `errors.Is`. |
| STO-03 | **Read after write.** A link MUST resolve immediately after its creation returns `201`, with no eventual-consistency `404`. |
| STO-04 | **Unique codes under concurrency.** When several requests try to create the same custom code at once, exactly one MUST succeed. The rest get `409`. A collision on a generated code is retried, as it is today. |
| STO-05 | **Hit counting.** Each successful redirect MUST add exactly one to `hits`, including when redirects run concurrently. Counting a hit for a code that no longer exists MUST NOT create a record: it returns `ErrNotFound`, which is logged, and the redirect still succeeds. |
| STO-06 | **Expiry precision.** Expiry MUST be stored in whole seconds. The service truncates `expires_at` to the second when it creates the link, so the create response shows exactly what a later read returns. This applies to both stores. |
| STO-07 | **Expired links.** An expired link MUST return `410` until DynamoDB deletes it, and `404` after that. Deletion can take days. Until then, its custom code is still taken (`409`). |
| STO-08 | **Item format.** Each item MUST have these attributes: <br>• `code` (S, partition key)<br>• `target_url` (S)<br>• `created_at` (S, RFC 3339, UTC)<br>• `hits` (N)<br>• `expires_at` (N, epoch seconds), omitted for links that never expire<br>The table's TTL attribute is `expires_at`. |
| STO-09 | **Readiness.** `GET /readyz` MUST return `503` when the table can't be reached, within `APP_REQUEST_TIMEOUT`. |
| STO-10 | **Failures.** A DynamoDB error that remains after the SDK's retries MUST become `500` `internal_error`. The AWS error is logged with the `request_id` and never returned. A request that runs out of time returns the `timeout` envelope. An item without `target_url` counts as an internal error, not as a link. |
| STO-11 | **Store selection.** When `DYNAMODB_TABLE` is set, the service MUST use DynamoDB; otherwise it uses memory. The startup log line includes `store=memory` or `store=dynamodb`. On Lambda, `DYNAMODB_TABLE` is required, and init fails without it. This replaces LAM-07. |
| STO-12 | **Request cost.** A redirect MUST make at most one read and one write. A create makes one conditional write, plus one per retry after a generated-code collision. |
| STO-13 | **Local and CI.** `DynamoStore` MUST work against DynamoDB Local through `AWS_ENDPOINT_URL_DYNAMODB`. A `make` target starts DynamoDB Local and creates the table. CI runs the contract suite against DynamoDB Local. Locally, the DynamoDB tests are skipped, with a message, when the endpoint isn't set. |

## Acceptance criteria

1. _(automated, STO-02)_ The contract suite passes for both stores in CI.
2. _(automated, STO-04)_ 20 concurrent creates of the same custom code produce
   exactly one success and 19 `ErrAlreadyExists`.
3. _(automated, STO-05)_ 50 concurrent `IncrementHits` calls on one code raise
   `hits` by exactly 50.
4. _(automated, STO-05)_ `IncrementHits` on a code that doesn't exist returns
   `ErrNotFound`. A following `GetByCode` also returns `ErrNotFound`.
5. _(automated, STO-06)_ Create a link with `ttl_seconds: 2`. The response's
   `expires_at` has no fractional seconds, and the stored `expires_at` equals
   it. After 3 s, `GET /{code}` returns `410`.
6. _(automated, STO-10)_ When the store points at a table that doesn't exist:
   - `POST /api/v1/links` returns `500` `internal_error`, whose `request_id`
     matches a log line containing the AWS error.
   - `GET /readyz` returns `503`.
7. _(automated, STO-11)_ The Lambda handler's init fails when `DYNAMODB_TABLE`
   is unset.
8. _(manual, STO-01, STO-03)_ In production, create a link and follow it
   immediately: `302`. Redeploy the live tag, then follow it again: still `302`.
9. _(manual, STO-12)_ After 10 redirects to one link, CloudWatch shows at most
   10 read requests and 10 write requests on the table for that period.

## Edge cases

- **Case.** Codes are case-sensitive in both stores, so `abcd` and `ABCD` are
  different links.
- **Hit counter overflow.** `hits` is an `int64`. It won't overflow in practice.
- **Hits on expired links.** An expired link counts no hit, because `Resolve`
  returns before `IncrementHits`.
