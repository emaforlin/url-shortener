variable "region" {
  description = "AWS region where the stack is deployed."
  type        = string
  default     = "us-east-1"
}

variable "project" {
  description = "Project name, used as prefix of every resource name."
  type        = string
  default     = "url-shortener"
}

# Note: there is deliberately no `public_base_url` variable. PUBLIC_BASE_URL is
# derived from the API itself (see api.tf), because a value typed in here could
# disagree with the URL that actually serves traffic, and every short link ever
# handed out would carry the wrong origin.

variable "custom_domain_name" {
  description = <<-EOT
    Custom domain serving the API, e.g. sho.rt. Empty means the default
    execute-api URL is the public origin. Setting it disables the execute-api
    endpoint, so short links stay canonical on one host.
  EOT
  type        = string
  default     = ""
}

variable "log_level" {
  description = "Value of LOG_LEVEL for the function."
  type        = string
  default     = "info"

  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.log_level)
    error_message = "log_level must be one of: debug, info, warn, error."
  }
}

variable "lambda_memory_size" {
  description = "Memory (MB) for the function. CPU scales with it."
  type        = number
  default     = 256
}

variable "app_request_timeout_seconds" {
  description = <<-EOT
    Mirror of the service's APP_REQUEST_TIMEOUT default. Not passed to the
    function; it only guards the lambda_timeout invariant below.
  EOT
  type        = number
  default     = 8
}

variable "lambda_timeout" {
  description = "Function timeout (s). Must exceed APP_REQUEST_TIMEOUT so the service can return its own timeout response."
  type        = number
  default     = 10

  validation {
    condition     = var.lambda_timeout > var.app_request_timeout_seconds
    error_message = "lambda_timeout must be greater than app_request_timeout_seconds, or Lambda kills the request before the service can answer with a timeout."
  }
}

variable "log_retention_days" {
  description = "Retention of the function's CloudWatch log group."
  type        = number
  default     = 14
}

variable "access_log_retention_days" {
  description = "Retention of the API Gateway access log group."
  type        = number
  default     = 14
}

# ---------------------------------------------------------------------------
# Throttling (INF-12)
# ---------------------------------------------------------------------------

variable "throttle_rate_limit" {
  description = "Stage-wide steady-state request rate (requests/second)."
  type        = number
  default     = 50
}

variable "throttle_burst_limit" {
  description = "Stage-wide burst capacity (concurrent requests)."
  type        = number
  default     = 100
}

variable "create_link_rate_limit" {
  description = <<-EOT
    Steady-state rate for POST /api/v1/links. Deliberately far below the stage
    default: an open shortener is abused for phishing within hours, and only the
    maintainer ever creates links. Redirects are unaffected.
  EOT
  type        = number
  default     = 1
}

variable "create_link_burst_limit" {
  description = "Burst capacity for POST /api/v1/links."
  type        = number
  default     = 5
}

# ---------------------------------------------------------------------------
# Observability and cost (INF-22, INF-24)
# ---------------------------------------------------------------------------

variable "alert_email" {
  description = <<-EOT
    Address that receives alarm notifications and budget alerts. AWS sends a
    confirmation link for the SNS subscription that must be clicked by hand;
    until then no alarm is ever delivered.
  EOT
  type        = string

  validation {
    condition     = can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", var.alert_email))
    error_message = "alert_email must be a valid email address."
  }
}

variable "alarm_period_seconds" {
  description = "Evaluation period shared by the alarms."
  type        = number
  default     = 300
}

variable "lambda_errors_threshold" {
  description = "Lambda Errors in one period that trigger the alarm."
  type        = number
  default     = 1
}

variable "lambda_throttles_threshold" {
  description = "Lambda Throttles in one period that trigger the alarm."
  type        = number
  default     = 1
}

variable "api_5xx_threshold" {
  description = "API Gateway 5xx responses in one period that trigger the alarm."
  type        = number
  default     = 1
}

variable "api_latency_p99_threshold_ms" {
  description = "p99 end-to-end latency (ms) above which the latency alarm fires."
  type        = number
  default     = 2000
}

variable "api_latency_evaluation_periods" {
  description = "Consecutive periods the p99 latency must stay high before alarming."
  type        = number
  default     = 3
}

variable "budget_limit_usd" {
  description = "Monthly cost budget. It alerts only; it does not stop spending."
  type        = number
  default     = 5
}
