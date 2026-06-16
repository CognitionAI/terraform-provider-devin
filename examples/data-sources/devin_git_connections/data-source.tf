data "devin_git_connections" "all" {}

# Pick the GitHub App connection for use with devin_git_permission.
locals {
  github_connection_id = one([
    for c in data.devin_git_connections.all.connections : c.git_connection_id
    if c.git_provider_type == "github_app"
  ])
}
