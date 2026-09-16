# 002 — AWS infrastructure

| Status       | Draft |
| ------------ | ----- |
| Architecture | [System overview](../architecture.md#system-overview), [Infrastructure as code](../architecture.md#infrastructure-as-code), [AWS access from GitHub](../architecture.md#aws-access-from-github), [Observability](../architecture.md#observability), [Security](../architecture.md#security), [Cost](../architecture.md#cost) |
| Depends on   | [001](001-lambda-runtime.md) |

## Summary

Every AWS resource the service uses is declared in `infra/`. Starting from an
account where only the bootstrap has been applied, a merge to `main` builds the
whole stack. GitHub reaches AWS without stored credentials, each identity can do
only its own job, logs expire, and alerts and cost overruns reach the maintainer
by email.

## Scope

**In scope:** `infra/bootstrap`, `infra/main`, and the `infra.yml` workflow.

**Out of scope:**

- Deploying function code ([003](003-delivery-pipeline.md)).
- What the links table stores ([004](004-persistent-storage.md)).
- The API token's value ([005](005-link-creation-protection.md)).
- Staging environments.
- Drift detection.
- Tearing the stack down.

## Requirements

### Bootstrap

| ID     | Requirement |
| ------ | ----------- |
| INF-01 | **One-time apply.** Applying `infra/bootstrap` once, by hand, with admin credentials MUST create the state bucket, the GitHub OIDC provider, and the roles `gh-plan`, `gh-infra` and `gh-deploy`. After that, routine changes need no human with AWS credentials. Changes to the bootstrap itself are still applied by hand. |
| INF-02 | **State bucket.** Versioning MUST be enabled, public access blocked, and encryption at rest on. The bucket policy MUST reject requests that don't use TLS, and no apply may delete the bucket. |
| INF-03 | **State files.** Bootstrap state MUST start local, and it MUST be possible to migrate it into the bucket later. No state file or `.terraform/` directory is ever committed; `.gitignore` covers them. |

### Main stack

| ID     | Requirement |
| ------ | ----------- |
| INF-04 | **Resources.** Every resource lives in `us-east-1`. `infra/main` MUST declare every resource in the [System overview](../architecture.md#system-overview): the HTTP API and its stage, the function with its `live` alias and execution role, the `links` table, log groups, alarms, the SNS topic and its email subscription, and the budget. It MUST also declare the custom domain and certificate when a domain is configured. Every resource SHOULD carry the tags `project=url-shortener` and `managed-by=terraform`. |
| INF-05 | **Ownership split.** An apply MUST NOT change the function's code, and MUST NOT change the version the `live` alias points to. Running plan right after a deploy reports no changes. |
| INF-06 | **Function settings.** The function MUST run on `provided.al2023`, arm64, with 256 MB of memory and a 10 s timeout. Its environment is `APP_ENV=production`, `PUBLIC_BASE_URL` and `LOG_LEVEL`, plus the variables [004](004-persistent-storage.md) and [005](005-link-creation-protection.md) add: `DYNAMODB_TABLE`, `API_TOKEN_PARAMETER` and `API_TOKEN_VERSION`. Plan MUST fail validation if the function timeout isn't greater than `APP_REQUEST_TIMEOUT`. |
| INF-07 | **Base URL.** `PUBLIC_BASE_URL` MUST be the stage's `execute-api` URL, since there is no custom domain for now. If one is added later, it becomes the custom domain. Neither case may create a dependency cycle. |
| INF-08 | **No secrets in code or state.** No secret value may appear in the repository, in state, in plan output or in the function's environment variables. Resources that read secret values back into state MUST NOT be used; this includes an `aws_ssm_parameter` resource or data source for the API token. |
| INF-09 | **Table protection.** The `links` table MUST have deletion protection enabled, so an apply can't drop it. |

### API Gateway

| ID     | Requirement |
| ------ | ----------- |
| INF-10 | **Routing.** A `$default` route and an explicit `POST /api/v1/links` route MUST both invoke the function's `live` alias, never `$LATEST`, using payload format 2.0. Unknown paths reach the function and get its JSON envelope, not API Gateway's `{"message":"Not Found"}`. |
| INF-11 | **Request ID.** In production, the `X-Request-Id` the service sees MUST equal API Gateway's `$context.requestId`, whatever the client sent. The same ID then appears in the access log entry, the application log lines, the `X-Request-Id` and `Apigw-Requestid` response headers, and error bodies. This can be done with parameter mapping, if the Lambda integration supports it, or by the entry point from `requestContext.requestId`. The README must say that clients can't choose the request ID in production. |
| INF-12 | **Edge throttling.** Throttling MUST be configured per route: a stage default, plus a stricter limit on `POST /api/v1/links`. The values are variables, with defaults set in [005](005-link-creation-protection.md). |
| INF-13 | **Errors API Gateway produces itself.** The README MUST document that some responses never reach the service, so they don't use its error envelope: `429` when throttled, `500` when the function fails init or crashes, and `503` when Lambda is unavailable. |
| INF-14 | **Single public host.** Applies only once a custom domain is added. When one is configured, the default `execute-api` endpoint MUST be disabled. This keeps short links canonical and keeps the service's self-referential target check meaningful. |
| INF-15 | **Access logs.** Access logs MUST be JSON and include at least `requestId`, `sourceIp`, `httpMethod`, `path`, `status`, `responseLatency`, `integrationLatency` and `integrationErrorMessage`. |

### Access control

| ID     | Requirement |
| ------ | ----------- |
| INF-16 | **OIDC trust.** Each CI role MUST trust only `token.actions.githubusercontent.com`, with `aud = sts.amazonaws.com` and an exact-match `sub` from the [role table](../architecture.md#aws-access-from-github). No wildcards. |
| INF-17 | **`gh-plan`.** The role is read-only, except that it may create and delete the state lock object. It MUST NOT be able to read SSM parameter values. |
| INF-18 | **`gh-infra`.** The role can manage only this project's resources. It MUST NOT be able to modify the bootstrap resources: its own role, the other CI roles, the OIDC provider, or the state bucket policy. That prevents a change to `infra/main` from widening its own permissions. |
| INF-19 | **`gh-deploy`.** The role gets only the actions in the role table, on the one function, its versions and its alias. It also gets `ssm:GetParameter` on the token parameter, which the smoke test needs (DEL-10). |
| INF-20 | **Execution role.** The function's role gets `GetItem`, `PutItem`, `UpdateItem`, `DeleteItem` and `DescribeTable` on the `links` table, `ssm:GetParameter` on the single token parameter, and write access to its own log group. Nothing else. |

### Observability and cost

| ID     | Requirement |
| ------ | ----------- |
| INF-21 | **Log retention.** The function's log group and the access log group MUST exist before the first invocation, with 14-day retention. No log group without retention may exist for the project. |
| INF-22 | **Alarms.** Alarms MUST notify the SNS topic when they fire and when they recover. Traffic is sparse, so missing data counts as not breaching. Thresholds are variables with these defaults:<br>• Lambda `Errors` ≥ 1 in one 5-minute period.<br>• Lambda `Throttles` ≥ 1 in one 5-minute period.<br>• API Gateway `5xx` ≥ 1 in one 5-minute period.<br>• API Gateway p99 `Latency` > 2000 ms for 3 consecutive 5-minute periods. |
| INF-23 | **Email subscription.** AWS requires the maintainer to confirm the email subscription by hand. Until then, no alert is delivered. The runbook in the README MUST say so. |
| INF-24 | **Budget.** A monthly cost budget of 5 USD MUST email the maintainer when actual spend or forecast spend exceeds it. It alerts only, it doesn't stop spending, and billing data can lag by hours. |

### Infra pipeline (`infra.yml`)

| ID     | Requirement |
| ------ | ----------- |
| INF-25 | **Pull requests.** On a pull request that touches `infra/**`, the workflow MUST run `fmt -check`, `validate`, `tflint`, `trivy config` and `plan`. The plan is posted as one PR comment, updated on every push rather than added again. Findings of HIGH or CRITICAL severity fail the check, unless an inline ignore gives a justification. |
| INF-26 | **Fork pull requests.** Pull requests from forks get no OIDC token. The plan job MUST skip with a message saying why, not fail with an AWS error. |
| INF-27 | **Apply.** A push to `main` that touches `infra/**` MUST run `apply` in the GitHub environment `infra`. Applies never run at the same time as deploys (see [003](003-delivery-pipeline.md), DEL-16). |
| INF-28 | **Required checks.** If infra checks are required by branch protection, they MUST also report success on pull requests that don't touch `infra/**`. Otherwise those pull requests wait forever for a check that never runs. |
| INF-29 | **Configuration redeploy.** When an apply changes the function's configuration, the workflow MUST request a redeploy of the live tag ([003](003-delivery-pipeline.md), DEL-15). |

## Acceptance criteria

1. _(manual, INF-01, INF-04)_ Take an account where only the bootstrap has been
   applied and merge the pull request that adds `infra/main`. The apply succeeds
   with no other AWS steps, apart from these manual ones:
   - confirming the SNS email;
   - setting the token value ([005](005-link-creation-protection.md)).
2. _(manual, INF-10)_ After the first deploy, `GET <PUBLIC_BASE_URL>/nope`
   returns `404` with the service's envelope.
3. _(manual, INF-11)_ A request sent with `X-Request-Id: client-chosen` gets a
   response whose `X-Request-Id` equals its `Apigw-Requestid` and is not
   `client-chosen`. That ID also appears in both log groups.
4. _(manual, INF-05)_ Right after a deploy, plan on `main` reports no changes.
5. _(manual, INF-25, INF-29)_ A pull request that changes `LOG_LEVEL` gets a
   plan comment showing an update to the function only. After the merge, the
   apply succeeds and a redeploy of the live tag starts.
6. _(manual, INF-28)_ A pull request that only touches `cmd/` can be merged.
   No infra check blocks it.
7. _(manual, INF-17, INF-18, INF-19)_ Each role is denied an action outside its
   job:
   - With `gh-plan`, reading the token parameter with decryption returns
     `AccessDenied`.
   - With `gh-deploy`, `dynamodb:Scan` on `links` returns `AccessDenied`.
   - With `gh-infra`, `iam:UpdateAssumeRolePolicy` on `gh-infra` returns
     `AccessDenied`.
8. _(manual, INF-22, INF-23)_ Setting an alarm to `ALARM` with
   `aws cloudwatch set-alarm-state` delivers an email. Setting it back to `OK`
   delivers another.
9. _(manual, INF-21)_ `aws logs describe-log-groups` shows exactly the
   project's two log groups, each with `retentionInDays` = 14.
10. _(manual, INF-14, once a custom domain exists)_ With the domain configured,
    the `execute-api` URL no longer serves the API.
11. _(manual, INF-09)_ A plan that would delete the table fails on apply.
