# The links table. On-demand capacity.
resource "aws_dynamodb_table" "links" {
  name         = "links"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "code"

  attribute {
    name = "code"
    type = "S"
  }

  # DynamoDB TTL needs a Number attribute in epoch seconds. (deletion is lazy)
  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  # Two guards at two different layers, on purpose (INF-09):
  #
  #   prevent_destroy          fails during `terraform plan`, before anything
  #                            reaches AWS, and covers `terraform destroy` too.
  #   deletion_protection      fails at the DynamoDB API, so it still holds if
  #                            someone deletes the resource outside Terraform.
  deletion_protection_enabled = true

  lifecycle {
    prevent_destroy = true
  }
}
