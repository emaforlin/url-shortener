# The topic is deliberately unencrypted.
# trivy:ignore:AVD-AWS-0095
resource "aws_sns_topic" "alerts" {
  name = "${var.project}-alerts"
}

# AWS leaves this subscription in PendingConfirmation until the address clicks
# the link in the confirmation email. Terraform reports the resource as created
# either way, so the stack can look entirely healthy while every alarm goes
# nowhere. The README runbook says so; it is the first thing to check when an
# alarm fires and no mail arrives (INF-23).
resource "aws_sns_topic_subscription" "alerts_email" {
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

locals {
  # Notify on the way into ALARM and on the way back to OK. Without ok_actions
  # an alarm that resolves itself is never mentioned again, which makes a
  # transient failure indistinguishable from an ongoing one.
  alarm_actions = [aws_sns_topic.alerts.arn]

  # Traffic is sparse: an idle five-minute window produces no datapoints at all.
  # The CloudWatch default treats that as insufficient data and can leave alarms
  # stuck; "notBreaching" reads no news as good news, which is correct here.
  treat_missing_data = "notBreaching"
}

resource "aws_cloudwatch_metric_alarm" "lambda_errors" {
  alarm_name        = "${var.project}-lambda-errors"
  alarm_description = "The function returned an error. Check /aws/lambda/${local.function_name}."

  namespace   = "AWS/Lambda"
  metric_name = "Errors"
  dimensions  = { FunctionName = aws_lambda_function.api.function_name }

  statistic           = "Sum"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = var.lambda_errors_threshold
  period              = var.alarm_period_seconds
  evaluation_periods  = 1

  treat_missing_data = local.treat_missing_data
  alarm_actions      = local.alarm_actions
  ok_actions         = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "lambda_throttles" {
  alarm_name        = "${var.project}-lambda-throttles"
  alarm_description = "Lambda refused an invocation. Concurrency limit reached, or a runaway caller."

  namespace   = "AWS/Lambda"
  metric_name = "Throttles"
  dimensions  = { FunctionName = aws_lambda_function.api.function_name }

  statistic           = "Sum"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = var.lambda_throttles_threshold
  period              = var.alarm_period_seconds
  evaluation_periods  = 1

  treat_missing_data = local.treat_missing_data
  alarm_actions      = local.alarm_actions
  ok_actions         = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "api_5xx" {
  alarm_name        = "${var.project}-api-5xx"
  alarm_description = "API Gateway returned a 5xx. Some of these never reach the service: see the README."

  namespace   = "AWS/ApiGateway"
  metric_name = "5xx"
  dimensions  = { ApiId = aws_apigatewayv2_api.this.id }

  statistic           = "Sum"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  threshold           = var.api_5xx_threshold
  period              = var.alarm_period_seconds
  evaluation_periods  = 1

  treat_missing_data = local.treat_missing_data
  alarm_actions      = local.alarm_actions
  ok_actions         = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "api_latency_p99" {
  alarm_name        = "${var.project}-api-latency-p99"
  alarm_description = "End-to-end p99 latency is high for a sustained period. Compare with IntegrationLatency to tell cold starts from slow handlers."

  namespace   = "AWS/ApiGateway"
  metric_name = "Latency"
  dimensions  = { ApiId = aws_apigatewayv2_api.this.id }

  # A percentile, not an average: with this little traffic a single cold start
  # would drag an average around, while p99 over three consecutive periods only
  # fires when slowness is actually persistent.
  extended_statistic  = "p99"
  comparison_operator = "GreaterThanThreshold"
  threshold           = var.api_latency_p99_threshold_ms
  period              = var.alarm_period_seconds
  evaluation_periods  = var.api_latency_evaluation_periods

  treat_missing_data = local.treat_missing_data
  alarm_actions      = local.alarm_actions
  ok_actions         = local.alarm_actions
}

# A backstop against a runaway bill, not a spending cap: AWS budgets alert and
# nothing more, and billing data can lag by most of a day. Forecast crossing the
# limit is usually the earlier of the two signals (INF-24).
#
# Budgets email the address directly rather than through SNS, so this one needs
# no confirmation click.
resource "aws_budgets_budget" "monthly" {
  name         = "${var.project}-monthly"
  budget_type  = "COST"
  limit_amount = format("%.2f", var.budget_limit_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alert_email]
  }
}
