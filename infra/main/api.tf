locals {
  # PUBLIC_BASE_URL, resolved without a dependency cycle (INF-07).
  #
  # This reads as circular — the function needs the API's URL, the API needs the
  # function — but it is not. `api_endpoint` is an attribute of the API resource
  # itself, and the API does not depend on the integration. The graph is:
  #
  #   api ──► function (env reads api.api_endpoint)
  #    │         └──► alias ──► integration ──► route ──► stage
  #    └────────────────────────────────────────────────────┘
  #
  # Terraform creates the API first, hands its URL to the function, and wires the
  # integration afterwards.
  #
  # The conditional matters because a custom domain turns the execute-api
  # endpoint off (INF-14). Falling back to api_endpoint unconditionally would set
  # PUBLIC_BASE_URL to a host that no longer answers.
  public_base_url = var.custom_domain_name != "" ? "https://${var.custom_domain_name}" : aws_apigatewayv2_api.this.api_endpoint
}

resource "aws_apigatewayv2_api" "this" {
  name          = var.project
  protocol_type = "HTTP"
  description   = "Public entry point for the URL shortener."

  # Once a custom domain exists, the execute-api URL must stop serving the API:
  # two public hosts would make short links ambiguous and would defeat the
  # service's check that a target does not point back at its own host.
  disable_execute_api_endpoint = var.custom_domain_name != ""
}

resource "aws_apigatewayv2_integration" "lambda" {
  api_id           = aws_apigatewayv2_api.this.id
  integration_type = "AWS_PROXY"

  # The alias ARN, never the function ARN. This is what keeps $LATEST out of the
  # request path (INF-10).
  integration_uri        = aws_lambda_alias.live.invoke_arn
  payload_format_version = "2.0"

  # The service already bounds its own handlers with APP_REQUEST_TIMEOUT; this
  # is the outer limit and stays just above the function timeout.
  timeout_milliseconds = (var.lambda_timeout + 1) * 1000
}

# Everything the service does not route explicitly still reaches it, so an
# unknown path gets the service's JSON error envelope rather than API Gateway's
# bare {"message":"Not Found"} (INF-10).
resource "aws_apigatewayv2_route" "default" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

# $default would already carry this request. The route exists only so link
# creation can be throttled separately from redirects (INF-12).
resource "aws_apigatewayv2_route" "create_link" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "POST /api/v1/links"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

resource "aws_cloudwatch_log_group" "access" {
  name              = "/aws/apigateway/${var.project}"
  retention_in_days = var.access_log_retention_days
}

resource "aws_apigatewayv2_stage" "default" {
  api_id = aws_apigatewayv2_api.this.id

  # The literal name "$default" means the stage contributes no path prefix, so
  # the base URL is the bare host and short links stay as short as possible.
  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.access.arn

    # JSON, so CloudWatch Logs Insights can query fields rather than parse text
    # (INF-15). integrationErrorMessage is the field that explains a 500 the
    # function never got to log itself.
    format = jsonencode({
      requestId               = "$context.requestId"
      sourceIp                = "$context.identity.sourceIp"
      httpMethod              = "$context.httpMethod"
      path                    = "$context.path"
      routeKey                = "$context.routeKey"
      status                  = "$context.status"
      protocol                = "$context.protocol"
      responseLength          = "$context.responseLength"
      responseLatency         = "$context.responseLatency"
      integrationLatency      = "$context.integrationLatency"
      integrationStatus       = "$context.integrationStatus"
      integrationErrorMessage = "$context.integrationErrorMessage"
      requestTime             = "$context.requestTime"
    })
  }

  default_route_settings {
    throttling_rate_limit  = var.throttle_rate_limit
    throttling_burst_limit = var.throttle_burst_limit
  }

  # Stricter than the stage default. A client that trips this gets API Gateway's
  # own 429, which does not carry the service's error envelope — see the README.
  route_settings {
    route_key              = aws_apigatewayv2_route.create_link.route_key
    throttling_rate_limit  = var.create_link_rate_limit
    throttling_burst_limit = var.create_link_burst_limit
  }
}

# Custom domain (INF-14): the variable and the execute-api switch above are in
# place, but aws_acm_certificate, aws_apigatewayv2_domain_name and the API
# mapping are not declared yet — they need a Route 53 hosted zone to validate
# against, and this project has no domain. Adding them is the remaining work for
# acceptance criterion 10.
