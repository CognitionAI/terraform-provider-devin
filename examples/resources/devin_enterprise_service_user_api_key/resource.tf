data "devin_service_users" "all" {}

locals {
  ci_service_user_id = one([
    for su in data.devin_service_users.all.service_users : su.service_user_id
    if su.name == "CI Pipeline"
  ])
}

# A key that does not expire.
resource "devin_enterprise_service_user_api_key" "ci" {
  service_user_id = local.ci_service_user_id
  name            = "ci"
}

# Rotate a key every 30 days (uses the hashicorp/time provider). Each new key
# is created before the old one is revoked and expires after 60 days.
resource "time_rotating" "ci" {
  rotation_days = 30
}

resource "devin_enterprise_service_user_api_key" "rotating" {
  service_user_id = local.ci_service_user_id
  name            = "ci-${formatdate("YYYY-MM-DD", time_rotating.ci.id)}"
  expires_at      = time_rotating.ci.unix + 60 * 86400

  lifecycle {
    create_before_destroy = true
  }
}

output "ci_token" {
  value     = devin_enterprise_service_user_api_key.rotating.token
  sensitive = true
}
