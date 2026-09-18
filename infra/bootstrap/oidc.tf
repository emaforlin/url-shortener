# The trust anchor for every workflow that talks to AWS.
#
# Registering GitHub as an OpenID Connect identity provider: 
# a workflow asks GitHub for a short lived JWT describing itself (which repo, which ref, 
# which environment), hands that token to STS, and gets back credentials 
# that expire in an hour.
resource "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"

  # The audience the workflow requests. aws-actions/configure-aws-credentials
  # asks for exactly this value, and the role trust policies below check it
  # again, so a token minted for some other audience cannot be replayed here.
  client_id_list = ["sts.amazonaws.com"]

  # thumbprint_list is deliberately omitted. AWS validates GitHub's certificate
  # against its own trust store for this well-known issuer.
}
