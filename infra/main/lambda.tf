locals {
  function_name = "${var.project}-api"
}

# Created explicitly so retention is managed here: the group Lambda creates on
# its own never expires
resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/lambda/${local.function_name}"
  retention_in_days = var.log_retention_days
}

data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "api" {
  name               = "${local.function_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

data "aws_iam_policy_document" "api" {
  statement {
    sid    = "WriteOwnLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["${aws_cloudwatch_log_group.api.arn}:*"]
  }

  # Exactly the operations links.Store performs, and nothing else (INF-20).
  statement {
    sid    = "ReadWriteLinks"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
      "dynamodb:DescribeTable",
    ]
    resources = [aws_dynamodb_table.links.arn]
  }
}

resource "aws_iam_role_policy" "api" {
  name   = "${local.function_name}-policy"
  role   = aws_iam_role.api.id
  policy = data.aws_iam_policy_document.api.json
}

# The function has to be created with *some* code, but it must never be the real
# code. Terraform owns configuration; deploy.yml owns the binary. Pointing
# `filename` at dist/lambda.zip would mean an apply run from a laptop with a
# stale build would silently ship it.
#
# This zip holds a stub that is never invoked: the first deploy replaces it
# before any traffic reaches the function.
data "archive_file" "placeholder" {
  type        = "zip"
  output_path = "${path.module}/.terraform/placeholder.zip"

  source {
    filename = "bootstrap"
    content  = "#!/bin/sh\necho 'placeholder: deploy.yml has not shipped a build yet' >&2\nexit 1\n"
  }
}

resource "aws_lambda_function" "api" {
  function_name = local.function_name
  role          = aws_iam_role.api.arn

  # Custom runtime: the zip holds a single `bootstrap` executable, so there is
  # no handler to name.
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  memory_size   = var.lambda_memory_size
  timeout       = var.lambda_timeout

  filename         = data.archive_file.placeholder.output_path
  source_code_hash = data.archive_file.placeholder.output_base64sha256

  # Publishes version 1 on creation, so the alias below has a real version to
  # point at and never has to target $LATEST. Later applies publish further
  # versions when configuration changes; they are inert, because the alias
  # ignores function_version and nothing routes to them.
  publish = true

  environment {
    variables = {
      APP_ENV         = "production"
      PUBLIC_BASE_URL = local.public_base_url
      LOG_LEVEL       = var.log_level

      # DYNAMODB_TABLE arrives with spec 004, when DynamoStore exists and the
      # service actually reads it. The table and its permissions exist already.
    }
  }

  # Half of the ownership split (INF-05). After creation Terraform stops caring
  # what code is deployed; `terraform plan` right after a deploy must report no
  # changes, which is acceptance criterion 4.
  lifecycle {
    ignore_changes = [filename, source_code_hash]
  }

  # Without this the function would create the log group itself, unretained, on
  # its first invocation.
  depends_on = [
    aws_cloudwatch_log_group.api,
    aws_iam_role_policy.api,
  ]
}

# The only invoke target API Gateway is given: it resolves to a published
# version, never to $LATEST, so a half-applied deploy cannot serve traffic.
resource "aws_lambda_alias" "live" {
  name             = "live"
  description      = "Version currently serving production traffic."
  function_name    = aws_lambda_function.api.function_name
  function_version = aws_lambda_function.api.version

  # deploy.yml moves this alias; Terraform records whatever version existed 
  # when it last looked.
  # Without ignore_changes, an apply triggered by something entirely unrelated —
  # bumping LOG_LEVEL, say — would reset `live` to that stale version, rolling
  # production back with no indication that it had happened.
  lifecycle {
    ignore_changes = [function_version]
  }
}

# Invoke rights are granted on the alias, not the function, so the gateway
# cannot reach $LATEST even if an integration is misconfigured.
resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowInvokeFromApiGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  qualifier     = aws_lambda_alias.live.name
  principal     = "apigateway.amazonaws.com"

  # Any stage, any route of this one API.
  source_arn = "${aws_apigatewayv2_api.this.execution_arn}/*/*"
}
