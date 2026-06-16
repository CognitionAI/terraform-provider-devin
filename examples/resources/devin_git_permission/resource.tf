# Look up the git connection instead of hard-coding its ID.
data "devin_git_connections" "all" {}

locals {
  github_connection_id = one([
    for c in data.devin_git_connections.all.connections : c.git_connection_id
    if c.git_provider_type == "github_app"
  ])
}

# Grant access to all repos under a group/org prefix.
resource "devin_git_permission" "frontend_repos" {
  org_id            = devin_organization.frontend.org_id
  git_connection_id = local.github_connection_id
  group_prefix      = "mycompany/frontend"
}

# Grant read-only access to a single repository.
resource "devin_git_permission" "shared_lib" {
  org_id            = devin_organization.frontend.org_id
  git_connection_id = local.github_connection_id
  repo_path         = "mycompany/shared-components"
  read_only         = true
}
