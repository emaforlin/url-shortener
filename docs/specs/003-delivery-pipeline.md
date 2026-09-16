# 003 — Delivery pipeline

| Status       | Draft |
| ------------ | ----- |
| Architecture | [CI/CD](../architecture.md#cicd), [Deploy and rollback](../architecture.md#deploy-and-rollback), [Ownership](../architecture.md#ownership) |
| Depends on   | [001](001-lambda-runtime.md), [002](002-aws-infrastructure.md) |

## Summary

Code reaches production only through a `vMAJOR.MINOR.PATCH` tag on a commit
merged into `main`. Each deploy checks itself against the public URL and rolls
back automatically when that check fails. The maintainer can redeploy or roll
back to any released tag from the Actions tab, and the artifact deployed is
always the one published with that release.

## Scope

**In scope:** `ci.yml` as the merge gate, branch and tag settings, `deploy.yml`,
GitHub Releases, and redeploys after configuration changes.

**Out of scope:**

- `infra.yml` checks and applies ([002](002-aws-infrastructure.md)).
- Canary or weighted alias routing.
- Deploying every merge to `main` (an open question in `architecture.md`).

## Current state

`ci.yml` already covers DEL-01, with two gaps:

- **No Lambda build.** It doesn't run `make build-lambda` yet.
- **Broken coverage step.** It runs `go test cover -func=coverage.out`, which
  isn't a valid command. It should be `go tool cover -func=coverage.out`.

## Requirements

### CI gate

| ID     | Requirement |
| ------ | ----------- |
| DEL-01 | **Checks.** Every pull request and every push to `main` MUST run the gofmt check, a `go mod tidy` diff, lint, the tests with the race detector, `govulncheck`, `make build`, `make build-lambda`, and a Docker build without push. One aggregate check, `ci-ok`, reports the combined result. |
| DEL-02 | **Branch protection.** `main` MUST require `ci-ok` to pass before a merge. |
| DEL-03 | **Coverage.** CI MUST publish total test coverage in the job summary. Coverage is only reported: no minimum fails the build. The _(automated)_ acceptance criteria of the other specs run in this workflow. |

### What can be deployed

| ID     | Requirement |
| ------ | ----------- |
| DEL-04 | **Tag format.** Pushing a tag that matches `^v\d+\.\d+\.\d+$` starts a deploy. A tag that matches `v*` but not that pattern MUST fail in the first step with a clear message, and deploy nothing. |
| DEL-05 | **Merged commits only.** The tagged commit MUST be reachable from `main`. If it isn't, the run fails before building. |
| DEL-06 | **Tags are immutable.** On a tag push, if a GitHub Release already exists for the tag, the run MUST fail before deploying. A ruleset SHOULD restrict creating `v*` tags to the maintainer, and releases SHOULD be immutable. |

### Artifact

| ID     | Requirement |
| ------ | ----------- |
| DEL-07 | **Build once.** A deploy of a tag without a Release MUST run the tests and then `make build-lambda VERSION=<tag>`. It uploads the zip and its SHA-256 as a workflow artifact. The deploy job verifies the checksum and deploys that exact file. |
| DEL-08 | **Reuse released artifacts.** Redeploys and rollbacks of a tag that already has a Release MUST deploy that Release's `lambda.zip`, after checking it against `SHA256SUMS`. They never rebuild. |

### Deploy and rollback

| ID     | Requirement |
| ------ | ----------- |
| DEL-09 | **Deploy.** The deploy job runs in the environment `production`. It MUST: record the version `live` points to (N-1) and that version's tag; upload the code and publish version N, whose description is the tag; wait until the update completes; and point `live` at N. |
| DEL-10 | **Smoke test.** The smoke test runs against `PUBLIC_BASE_URL`. An alias switch isn't instant, so `GET /healthz` MUST return `200` with `version` equal to the tag on 3 consecutive attempts, polling every 2 s for up to 60 s. Once [004](004-persistent-storage.md) and [005](005-link-creation-protection.md) are implemented, the smoke test also MUST check three things: a `POST /api/v1/links` without a token returns `401`; one with the token, target `https://example.com/smoke/<run id>` and `ttl_seconds: 600`, returns `201`; and `GET /{code}`, without following redirects, returns `302` with that target in `Location`. The job reads the token from SSM with the `gh-deploy` role and masks it, so it never appears in the job log. GitHub stores no copy of the token. |
| DEL-11 | **Automatic rollback.** The job MUST point `live` back at N-1 when the smoke test fails, or when the job fails or is cancelled after `live` moved. It then checks that `/healthz` reports the previous tag, polling the same way, and the run fails. |
| DEL-12 | **Run summary.** The summary MUST state the tag deployed, the previous tag and the outcome: `deployed`, `rolled back`, or `rollback failed — production state unknown`. The last outcome fails a step whose name says so. |
| DEL-13 | **First deploy.** When `live` points to the placeholder version (a description that isn't a tag), rollback points back at the placeholder. The summary says there was no previous release. |
| DEL-14 | **Manual redeploy and rollback.** A `workflow_dispatch` run, with "Use workflow from" set to a tag, MUST redeploy that tag, with the same smoke test and automatic rollback. Dispatching an older tag rolls production back to it. A dispatch from a branch MUST fail before it touches AWS. The run uses the workflow definition as it was at that tag. |
| DEL-15 | **Configuration redeploy.** After an apply changes the function's configuration (INF-29), the live tag MUST be redeployed. That publishes a new version with the new configuration and points `live` at it. If `live` points at the placeholder, nothing is redeployed and the apply summary says so. |
| DEL-16 | **One at a time.** Deploys, rollbacks, configuration redeploys and infra applies MUST share one concurrency group. A newer run never cancels a running one. |
| DEL-17 | **Stale configuration redeploy.** A configuration redeploy carries the tag that was live when the apply finished. If a different tag is live when the redeploy starts, it MUST do nothing and succeed. That deploy ran after the apply (DEL-16), so it already published a version with the current configuration. |
| DEL-18 | **No-op redeploy.** Lambda doesn't publish a new version if neither code nor configuration changed. Redeploying the live tag with nothing changed MUST succeed anyway: `live` stays on the same version, and the smoke test still runs. |
| DEL-19 | **Release.** After a successful smoke test on a tag push, the workflow MUST create a GitHub Release for the tag, with `lambda.zip`, `SHA256SUMS` and generated notes. Redeploys and rollbacks never create or modify a Release. |
| DEL-20 | **Forced smoke failure.** A `workflow_dispatch` input SHOULD make the smoke test fail. That exercises rollback on demand, without shipping a broken build. |
| DEL-21 | **Credentials.** Jobs that call AWS MUST assume their OIDC role ([002](002-aws-infrastructure.md)) with `id-token: write` and `contents: read`. Only the release job gets `contents: write`, and only the infra apply job gets `actions: write`, which it needs to dispatch a redeploy. GitHub stores no AWS credentials. |

**Known limitation.** GitHub keeps at most one _pending_ run per concurrency
group. When another run queues, the older pending run is cancelled. A cancelled
pending run changed nothing, so the maintainer re-runs it by hand. If it was a
configuration redeploy, that means dispatching the live tag.

## Acceptance criteria

1. _(manual, DEL-01, DEL-02)_ A pull request with a failing test makes `ci-ok`
   fail, and GitHub blocks the merge.
2. _(manual, DEL-07, DEL-09, DEL-10, DEL-19)_ Push `v0.1.0` on a commit in
   `main`. Then:
   - The run succeeds, and `/healthz` reports `v0.1.0`.
   - The version `live` points to has the description `v0.1.0`.
   - Release `v0.1.0` contains `lambda.zip` and `SHA256SUMS`.
   - The version's `CodeSha256` equals the base64-encoded SHA-256 of the
     Release's `lambda.zip`.
3. _(manual, DEL-05)_ A tag on a commit that exists only on a feature branch
   fails before the build. `live` doesn't change.
4. _(manual, DEL-04)_ The tag `vtest` fails in the first step.
5. _(manual, DEL-11, DEL-12, DEL-20)_ With `v0.1.0` live and `v0.2.0` released,
   dispatch `v0.2.0` with the forced smoke failure. Then:
   - The run fails, and the summary says `rolled back` to `v0.1.0`.
   - `/healthz` reports `v0.1.0`.
   - Release `v0.2.0` is unchanged.
6. _(manual, DEL-11)_ Cancel a run right after `live` moves. `live` returns to
   the previous version.
7. _(manual, DEL-08, DEL-14)_ With `v0.2.0` live, dispatch `v0.1.0`. Then:
   - `/healthz` reports `v0.1.0`.
   - The new version's `CodeSha256` matches Release `v0.1.0`'s asset.
   - No Release is created or modified.
8. _(manual, DEL-14)_ A dispatch from `main` fails before any AWS call.
9. _(manual, DEL-15)_ With `v0.2.0` live, merge an infra change to `LOG_LEVEL`.
   After the apply, a redeploy runs, and `live` points to a new version whose
   description is `v0.2.0` and whose configuration has the new `LOG_LEVEL`.
10. _(manual, DEL-16)_ A tag pushed while an apply is running starts deploying
    only after the apply finishes.
11. _(manual, DEL-18)_ Dispatching the live tag with nothing changed succeeds,
    and the version number of `live` stays the same.
12. _(manual, DEL-06)_ Deleting a released tag and pushing it again makes the
    run fail before it deploys.
13. _(manual, DEL-10)_ After a deploy that runs the full smoke test, the job log
    doesn't contain the token, and the repository and its environments have no
    secret holding it.
