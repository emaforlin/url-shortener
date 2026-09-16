# Functional specs

[`architecture.md`](../architecture.md) describes how the production setup is
built. The specs in this folder say what must be true once each part of it is
done, and how to check it. When the two disagree, fix whichever is wrong in the
same change. Don't leave them drifting apart.

| Spec                                                    | What it delivers                                                               | Depends on    | Status |
| ------------------------------------------------------- | ------------------------------------------------------------------------------ | ------------- | ------ |
| [001 Lambda runtime](001-lambda-runtime.md)             | The API served from AWS Lambda with the same behavior as the container          | —             | Draft  |
| [002 AWS infrastructure](002-aws-infrastructure.md)     | Every AWS resource in code, least-privilege CI access, alarms and cost guards   | 001           | Draft  |
| [003 Delivery pipeline](003-delivery-pipeline.md)       | CI gate, tag-driven deploys, smoke test, automatic rollback, releases           | 001, 002      | Draft  |
| [004 Persistent storage](004-persistent-storage.md)     | Links stored in DynamoDB, durable and shared across execution environments      | 002           | Draft  |
| [005 Link creation protection](005-link-creation-protection.md) | Bearer token on link creation and throttling at the edge            | 002, 003, 004 | Draft  |

## Implementation order

This order follows the [implementation plan](../architecture.md#implementation-plan).
Some specs are delivered in more than one step:

1. **001**, complete.
2. **002**, bootstrap section.
3. **003**, CI gate section.
4. **002**, main stack and infra pipeline sections.
5. **003**, deploy, rollback and release sections, with the `/healthz` smoke test.
6. **004**, then **005**, then the full smoke test in 003.

## Conventions

- **MUST** and **SHOULD** follow RFC 2119. A spec is done when every MUST holds
  and every acceptance criterion passes.
- **Requirement IDs** (`LAM-03`, `STO-05`, …) are stable. Never renumber them. If
  a requirement is dropped, mark it _Withdrawn_ and keep the ID.
- **Acceptance criteria** name the requirements they verify. Criteria marked
  _(automated)_ must run in CI. The rest are checked by hand once, against the
  real deployment, and the result goes in the pull request that completes the
  spec.
- **Status** moves from Draft to Approved to Implemented. A spec changes status
  in the same pull request that justifies the change.
- **README.** When a spec changes user-visible behavior, the pull request that
  implements it updates `README.md` too.
