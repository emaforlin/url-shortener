# 005 — Link creation protection

| Status       | Draft |
| ------------ | ----- |
| Architecture | [Security](../architecture.md#security), [Application changes](../architecture.md#application-changes) 5 |
| Depends on   | [002](002-aws-infrastructure.md), [003](003-delivery-pipeline.md), [004](004-persistent-storage.md) |

## Summary

An open URL shortener gets abused for phishing quickly. In production, only the
holder of the API token can create links. Anyone can still follow them. API
Gateway caps request rates before the function runs, so abuse can't grow costs
without limit.

## Scope

**In scope:** the bearer token on `POST /api/v1/links`, how the token is
configured and rotated, and default throttling limits.

**Out of scope:**

- A public demo. Visitors can follow links but not create them. The README's
  `curl` examples show how creation works.
- User accounts, and more than one token.
- Per-client rate limits. They need AWS WAF, which HTTP APIs don't support.
- Checking target URLs against phishing lists.

## Requirements

| ID      | Requirement |
| ------- | ----------- |
| AUTH-01 | **Token required.** `POST /api/v1/links` MUST require `Authorization: Bearer <token>`. It returns `401` when the header is missing, uses another scheme, carries an empty token or carries a wrong one. Every case gets the same response: `WWW-Authenticate: Bearer`, and the envelope with the code `unauthorized` and the same message. |
| AUTH-02 | **Checked first.** The token MUST be checked before the body is read or validated. An unauthenticated request never gets `400`, `409`, `413` or `415`. |
| AUTH-03 | **Other routes.** `GET /{code}`, `/healthz`, `/readyz` and unknown paths MUST NOT require or inspect credentials. An `Authorization` header on them changes nothing. |
| AUTH-04 | **Constant-time comparison.** The time taken to compare tokens MUST NOT depend on how many characters match, or on the presented token's length. |
| AUTH-05 | **Never logged.** The token MUST NOT appear in logs, error bodies or the startup log. Rejected attempts show up as `401` lines in the access log, without the presented value. |
| AUTH-06 | **Configuration.** `cmd/api` MUST read the token from `API_TOKEN`. The Lambda function MUST read it once, at init, from the SSM Parameter Store `SecureString` named in `API_TOKEN_PARAMETER`, and that read times out after 3 s. A token shorter than 32 characters MUST be rejected at startup. |
| AUTH-07 | **Fail closed.** When `APP_ENV=production` and no token is configured, startup MUST fail, with `API_TOKEN` listed alongside any other configuration errors. On Lambda, if the parameter can't be read or its value is invalid, init MUST fail. Creation never becomes open because a token couldn't be loaded. |
| AUTH-08 | **Development.** When `APP_ENV=development` and no token is configured, creation MUST be open. Startup logs a warning that says so. |
| AUTH-09 | **Parameter ownership.** The parameter `/url-shortener/api-token` MUST be created and set by the maintainer with the AWS CLI. Infrastructure code references it only by name or ARN, and never reads its value (INF-08). Only the execution role and `gh-deploy`, for the smoke test, can read it. |
| AUTH-10 | **Rotation.** Rotating the token MUST take two steps. First, put the new value in the parameter. Second, open a pull request that bumps the Terraform variable `api_token_version`. The function receives it as `API_TOKEN_VERSION` but doesn't use it. Merging runs an apply and a configuration redeploy (DEL-15), and once that finishes, the old token is rejected. The version bump is required: the token is read at init, and redeploying unchanged code and configuration publishes no new version (DEL-18). Without the bump, running environments would keep the old token. |
| AUTH-11 | **Throttling defaults.** `POST /api/v1/links` MUST default to a rate of 1 request/s with a burst of 5. Every other route MUST default to a rate of 10 requests/s with a burst of 20. All clients share these limits. |
| AUTH-12 | **Documentation.** The README MUST document: the `Authorization` header and the `401` response; that throttled requests get API Gateway's `429`, not the service's envelope; and how to set and rotate the token. |

**Trade-offs of the limits.** A single abuser can use up the creation limit and
make the maintainer's own requests fail with `429`. That's accepted for this
project. At the default limits, sustained traffic at the maximum rate adds up to
about 26 million requests a month, which costs tens of USD. The budget alert
(INF-24) is the signal to react.

## Acceptance criteria

1. _(automated, AUTH-01)_ `POST /api/v1/links` without `Authorization` returns
   `401` `unauthorized` with `WWW-Authenticate: Bearer`. No link is stored.
2. _(automated, AUTH-01)_ `Authorization: Basic …`, `Authorization: Bearer `
   (empty) and a wrong bearer token each return `401` with the same message.
3. _(automated, AUTH-02)_ A wrong token with malformed JSON returns `401`. So
   does a wrong token with a body over `APP_MAX_BODY_BYTES`.
4. _(automated, AUTH-01)_ The correct token with a valid body returns `201`.
5. _(automated, AUTH-03)_ `GET /{code}` and `GET /healthz` with
   `Authorization: Bearer garbage` return the same as without the header.
6. _(automated, AUTH-06, AUTH-07)_ Startup with `APP_ENV=production` fails in
   two cases:
   - No `API_TOKEN`: the error names `API_TOKEN`.
   - A 31-character token: the error names the minimum length.
7. _(automated, AUTH-08)_ With `APP_ENV=development` and no token, the service
   starts with a warning, and a `POST` without the header returns `201`.
8. _(automated, AUTH-05)_ Run the handler tests with a known token and capture
   all log output. The token doesn't appear in it.
9. _(manual, AUTH-04)_ Code review confirms the comparison uses a constant-time
   primitive over fixed-length digests of both tokens.
10. _(manual, AUTH-07)_ In production, deny the execution role access to the
    parameter:
    - Init fails, `POST` gets a `5xx` from API Gateway, and no link is created.
    - After access is restored, creation works again.
11. _(manual, AUTH-10)_ After the rotation procedure finishes, the old token
    gets `401` and the new one `201`.
12. _(manual, AUTH-11)_ 20 `POST`s sent within one second:
    - Some return `429`, with API Gateway's body.
    - The function's invocation count doesn't include the throttled requests.

## Notes

- **Docker default.** The Docker image defaults to `APP_ENV=production`, so the
  README's `docker run` example needs `-e API_TOKEN=…` once this ships.
