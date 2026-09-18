output "public_base_url" {
  description = "Origin short links are built against. Equals PUBLIC_BASE_URL in the function's environment."
  value       = local.public_base_url
}

output "api_endpoint" {
  description = "Default execute-api URL. Stops serving traffic once a custom domain is configured."
  value       = aws_apigatewayv2_api.this.api_endpoint
}

output "api_id" {
  description = "API Gateway API ID, for CloudWatch metric dimensions and log queries."
  value       = aws_apigatewayv2_api.this.id
}

output "links_table_name" {
  description = "Name of the DynamoDB table holding the links."
  value       = aws_dynamodb_table.links.name
}

output "function_name" {
  description = "Name of the API Lambda function."
  value       = aws_lambda_function.api.function_name
}

output "live_alias_arn" {
  description = "ARN of the live alias. Grant API Gateway lambda:InvokeFunction on this, not on the function."
  value       = aws_lambda_alias.live.arn
}

output "live_alias_invoke_arn" {
  description = "Invoke ARN of the live alias, for the API Gateway integration URI."
  value       = aws_lambda_alias.live.invoke_arn
}

output "alerts_topic_arn" {
  description = "SNS topic the alarms notify. Its email subscription must be confirmed by hand."
  value       = aws_sns_topic.alerts.arn
}

output "log_group_names" {
  description = "The project's two log groups: application logs and API Gateway access logs."
  value = {
    function   = aws_cloudwatch_log_group.api.name
    access_log = aws_cloudwatch_log_group.access.name
  }
}
