variable "npm_token" {
  description = "npm registry token for frontend builds"
  type        = string
  sensitive   = true
}

# Exposed to Devin sessions as the NPM_TOKEN environment variable.
resource "devin_secret" "npm_token" {
  org_id = devin_organization.frontend.org_id
  key    = "NPM_TOKEN"
  value  = var.npm_token
  note   = "Read-only npm token for CI builds"
}
