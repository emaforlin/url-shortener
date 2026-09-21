data "aws_partition" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition

  # One role per workflow, each trusting exactly one GitHub OIDC subject.
  #
  # The `sub` claim describes what produced the token: a repository prefix, then
  # the context. `:pull_request` is minted for pull request runs;
  # `:environment:<name>` only for a job that declares `environment: <name>`,
  # which GitHub gates on its own branch/tag rules. So the deploy role can only
  # be reached from a job running in the `production` environment, which the
  # repository settings restrict to `v*` tags.
  #
  # The prefix is not `repo:owner/name` — see var.github_subject_prefix. The
  # context segments below are unaffected by that format change.
  ci_roles = {
    plan = {
      name        = "gh-plan"
      sub         = "${var.github_subject_prefix}:pull_request"
      description = "Terraform plan on pull requests. Read-only, plus the state lock object."
    }
    infra = {
      name        = "gh-infra"
      sub         = "${var.github_subject_prefix}:environment:${var.infra_environment}"
      description = "Terraform apply of infra/main. Cannot touch the bootstrap stack."
    }
    deploy = {
      name        = "gh-deploy"
      sub         = "${var.github_subject_prefix}:environment:${var.production_environment}"
      description = "Function code deploys and rollbacks. Cannot change infrastructure."
    }
  }

  # Resource ARNs the policies below are scoped to. They are built by hand rather
  # than referenced, because the resources live in the other stack (or, for the
  # API token, are created outside Terraform entirely). Granting access to an ARN
  # that does not exist yet is legal and creates no dependency between the stacks.
  function_name = "${var.project}-api"
  function_arn  = "arn:${local.partition}:lambda:${var.region}:${local.account_id}:function:${local.function_name}"
  table_arn     = "arn:${local.partition}:dynamodb:${var.region}:${local.account_id}:table/links"
  topic_arn     = "arn:${local.partition}:sns:${var.region}:${local.account_id}:${var.project}-alerts"

  # SSM parameter names already start with a slash, so no separator here.
  api_token_parameter_arn = "arn:${local.partition}:ssm:${var.region}:${local.account_id}:parameter${var.api_token_parameter_name}"

  # The state file and, with use_lockfile = true, the lock object beside it.
  state_object_arns = [
    "${aws_s3_bucket.state.arn}/${var.state_key}",
    "${aws_s3_bucket.state.arn}/${var.state_key}.tflock",
  ]

  ci_role_arns = [for r in aws_iam_role.ci : r.arn]
}

# ---------------------------------------------------------------------------
# Trust policies
# ---------------------------------------------------------------------------

data "aws_iam_policy_document" "github_assume_role" {
  for_each = local.ci_roles

  statement {
    sid     = "GitHubOIDC"
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    # StringEquals, never StringLike. A subject wildcard such as
    # `repo:owner/name:*` would let any branch, tag or pull request in the
    # repository assume this role, which throws away the whole point of having
    # three of them. Exact match only (INF-16).
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = [each.value.sub]
    }
  }
}

resource "aws_iam_role" "ci" {
  for_each = local.ci_roles

  name        = each.value.name
  description = each.value.description

  assume_role_policy = data.aws_iam_policy_document.github_assume_role[each.key].json

  # A workflow job that runs longer than this has bigger problems.
  max_session_duration = 3600
}

# ---------------------------------------------------------------------------
# gh-plan: read everything, write one object (INF-17)
# ---------------------------------------------------------------------------

# Read-only across the account, so a plan can refresh any resource the main
# stack might grow later without this file needing an edit every time.
resource "aws_iam_role_policy_attachment" "plan_read_only" {
  role       = aws_iam_role.ci["plan"].name
  policy_arn = "arn:${local.partition}:iam::aws:policy/ReadOnlyAccess"
}

data "aws_iam_policy_document" "plan" {
  # A plan is not purely read-only: with S3 native locking it writes a `.tflock`
  # object next to the state file and deletes it afterwards. Scoped to those two
  # keys, not to the bucket, so the role cannot rewrite the state itself.
  statement {
    sid    = "StateReadAndLock"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:GetObjectVersion",
      "s3:PutObject",
      "s3:DeleteObject",
    ]
    resources = local.state_object_arns
  }

  statement {
    sid       = "StateListBucket"
    effect    = "Allow"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.state.arn]
  }

  # ReadOnlyAccess includes ssm:GetParameter, which returns the *decrypted* value
  # of a SecureString. That would put the API token in reach of any pull request,
  # including one that only pretends to change infrastructure. An explicit Deny
  # beats every Allow in IAM evaluation, so this closes the hole without giving
  # up the convenience of the managed policy (INF-17).
  statement {
    sid    = "NeverReadSecrets"
    effect = "Deny"
    actions = [
      "ssm:GetParameter",
      "ssm:GetParameters",
      "ssm:GetParametersByPath",
      "ssm:GetParameterHistory",
      "secretsmanager:GetSecretValue",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "plan" {
  name   = "gh-plan-state-and-denies"
  role   = aws_iam_role.ci["plan"].id
  policy = data.aws_iam_policy_document.plan.json
}

# ---------------------------------------------------------------------------
# gh-infra: manage the project's resources, never the bootstrap ones (INF-18)
# ---------------------------------------------------------------------------

data "aws_iam_policy_document" "infra" {
  statement {
    sid    = "StateReadWriteAndLock"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:GetObjectVersion",
      "s3:PutObject",
      "s3:DeleteObject",
    ]
    resources = local.state_object_arns
  }

  statement {
    sid       = "StateListBucket"
    effect    = "Allow"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.state.arn]
  }

  # API Gateway's control plane has no per-API ARN before the API exists, so the
  # grant covers the service's whole path space in this region.
  statement {
    sid       = "ApiGateway"
    effect    = "Allow"
    actions   = ["apigateway:*"]
    resources = ["arn:${local.partition}:apigateway:${var.region}::/*"]
  }

  statement {
    sid     = "Lambda"
    effect  = "Allow"
    actions = ["lambda:*"]
    resources = [
      local.function_arn,
      "${local.function_arn}:*", # versions and aliases
    ]
  }

  statement {
    sid     = "DynamoDB"
    effect  = "Allow"
    actions = ["dynamodb:*"]
    resources = [
      local.table_arn,
      "${local.table_arn}/index/*",
    ]
  }

  statement {
    sid     = "Logs"
    effect  = "Allow"
    actions = ["logs:*"]
    resources = [
      "arn:${local.partition}:logs:${var.region}:${local.account_id}:log-group:/aws/lambda/${local.function_name}",
      "arn:${local.partition}:logs:${var.region}:${local.account_id}:log-group:/aws/lambda/${local.function_name}:*",
      "arn:${local.partition}:logs:${var.region}:${local.account_id}:log-group:/aws/apigateway/${var.project}",
      "arn:${local.partition}:logs:${var.region}:${local.account_id}:log-group:/aws/apigateway/${var.project}:*",
    ]
  }

  # API Gateway does not write access logs itself: it asks CloudWatch Logs to set
  # up a *log delivery*, and the caller needs rights over that delivery. None of
  # these actions support resource-level permissions — IAM only ever matches them
  # against "*" — so the `logs:*` grant above, scoped to log-group ARNs, does not
  # cover them and the stage fails to create with "Insufficient permissions to
  # enable logging".
  #
  # PutResourcePolicy is the widest of them: it is how CloudWatch Logs is granted
  # write access to the destination group, and on "*" it reaches every log
  # resource policy in the account. It is needed only where the delivery is set
  # up for the first time, but an apply that finds the policy missing fails
  # without it.
  statement {
    sid    = "VendedLogDelivery"
    effect = "Allow"
    actions = [
      "logs:CreateLogDelivery",
      "logs:GetLogDelivery",
      "logs:UpdateLogDelivery",
      "logs:DeleteLogDelivery",
      "logs:ListLogDeliveries",
      "logs:PutResourcePolicy",
      "logs:DescribeResourcePolicies",
    ]
    resources = ["*"]
  }

  # DescribeLogGroups and DescribeAlarms have no resource-level permissions: they
  # are list operations and only accept "*".
  statement {
    sid    = "ReadOnlyListing"
    effect = "Allow"
    actions = [
      "logs:DescribeLogGroups",
      "cloudwatch:DescribeAlarms",
      "cloudwatch:ListTagsForResource",
      "budgets:ViewBudget",
      "budgets:DescribeBudget",
      "iam:ListOpenIDConnectProviders",
      "iam:GetOpenIDConnectProvider",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "Alarms"
    effect = "Allow"
    actions = [
      "cloudwatch:PutMetricAlarm",
      "cloudwatch:DeleteAlarms",
      "cloudwatch:TagResource",
      "cloudwatch:UntagResource",
      "cloudwatch:SetAlarmState",
    ]
    resources = ["arn:${local.partition}:cloudwatch:${var.region}:${local.account_id}:alarm:${var.project}-*"]
  }

  statement {
    sid       = "Sns"
    effect    = "Allow"
    actions   = ["sns:*"]
    resources = [local.topic_arn]
  }

  statement {
    sid       = "Budgets"
    effect    = "Allow"
    actions   = ["budgets:*"]
    resources = ["arn:${local.partition}:budgets::${local.account_id}:budget/*"]
  }

  # Only roles carrying the project prefix. The CI roles are named gh-*, so they
  # fall outside this pattern already; the explicit Deny below makes that a rule
  # rather than an accident of naming.
  statement {
    sid     = "ProjectRoles"
    effect  = "Allow"
    actions = ["iam:*"]
    resources = [
      "arn:${local.partition}:iam::${local.account_id}:role/${var.project}-*",
    ]
  }

  # --- The security boundary (INF-18) -------------------------------------
  #
  # Everything above is an Allow, and Allows are what a change to infra/main can
  # add to. These Denies are what it cannot remove, because they live in a stack
  # CI is not allowed to apply. Without them, a pull request could add an IAM
  # statement granting gh-infra administrator rights and CI would apply it.

  statement {
    sid       = "NeverTouchCiRoles"
    effect    = "Deny"
    actions   = ["iam:*"]
    resources = local.ci_role_arns
  }

  statement {
    sid       = "NeverTouchOidcProvider"
    effect    = "Deny"
    actions   = ["iam:*OpenIDConnectProvider*"]
    resources = ["*"]
  }

  statement {
    sid    = "NeverWeakenTheStateBucket"
    effect = "Deny"
    actions = [
      "s3:PutBucketPolicy",
      "s3:DeleteBucketPolicy",
      "s3:PutBucketVersioning",
      "s3:PutBucketPublicAccessBlock",
      "s3:DeleteBucket",
    ]
    resources = [
      aws_s3_bucket.state.arn,
      "${aws_s3_bucket.state.arn}/*",
    ]
  }

  # Residual escalation path worth knowing about: gh-infra can create a role
  # named url-shortener-* and write its trust policy, so in principle it could
  # mint itself a more powerful role. Denying the obvious admin policies raises
  # the cost; the complete fix is a permissions boundary required on every role
  # this policy can create, which is worth adding if this account ever holds
  # anything but this project.
  statement {
    sid       = "NeverAttachAdminPolicies"
    effect    = "Deny"
    actions   = ["iam:AttachRolePolicy", "iam:AttachUserPolicy", "iam:AttachGroupPolicy"]
    resources = ["*"]

    condition {
      test     = "ArnEquals"
      variable = "iam:PolicyARN"
      values = [
        "arn:${local.partition}:iam::aws:policy/AdministratorAccess",
        "arn:${local.partition}:iam::aws:policy/PowerUserAccess",
        "arn:${local.partition}:iam::aws:policy/IAMFullAccess",
      ]
    }
  }
}

resource "aws_iam_role_policy" "infra" {
  name   = "gh-infra-project-resources"
  role   = aws_iam_role.ci["infra"].id
  policy = data.aws_iam_policy_document.infra.json
}

# ---------------------------------------------------------------------------
# gh-deploy: move code onto one function, nothing else (INF-19)
# ---------------------------------------------------------------------------

data "aws_iam_policy_document" "deploy" {
  # Exactly the five actions in the role table, and no others. Note what is
  # absent: no UpdateFunctionConfiguration, so a deploy cannot change memory,
  # timeout or environment variables. That half of the function belongs to
  # Terraform, and the split is enforced here rather than by convention.
  #
  # `aws lambda wait function-updated` polls GetFunction, and the live tag is
  # read from GetFunction's Configuration.Description, so GetFunction covers
  # both without widening the list.
  statement {
    sid    = "DeployFunctionCode"
    effect = "Allow"
    actions = [
      "lambda:UpdateFunctionCode",
      "lambda:PublishVersion",
      "lambda:GetAlias",
      "lambda:UpdateAlias",
      "lambda:GetFunction",
      "lambda:GetFunctionConfiguration",
    ]
    resources = [
      local.function_arn,
      "${local.function_arn}:*",
    ]
  }

  # The smoke test authenticates against POST /api/v1/links, so it needs the
  # token. Reading it here keeps the value out of GitHub secrets: there is one
  # copy, in Parameter Store (DEL-10).
  statement {
    sid       = "ReadApiTokenForSmokeTest"
    effect    = "Allow"
    actions   = ["ssm:GetParameter"]
    resources = [local.api_token_parameter_arn]

    # No kms:Decrypt statement is needed while the parameter uses the default
    # alias/aws/ssm key: that key's policy already allows account principals
    # through the kms:ViaService condition. A customer-managed key would need one.
  }
}

resource "aws_iam_role_policy" "deploy" {
  name   = "gh-deploy-function-code"
  role   = aws_iam_role.ci["deploy"].id
  policy = data.aws_iam_policy_document.deploy.json
}
