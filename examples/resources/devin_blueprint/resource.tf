# Org-wide blueprint, applied to every repository in the organization.
resource "devin_blueprint" "org" {
  org_id   = devin_organization.frontend.org_id
  contents = file("${path.module}/blueprints/org.yaml")
}

# Repository blueprint stored in Devin.
resource "devin_blueprint" "web" {
  org_id    = devin_organization.frontend.org_id
  repo_name = "acme/web"
  contents  = file("${path.module}/blueprints/web.yaml")
  position  = 1
}

# Repository blueprint synced from .devin/blueprint.yaml, built on every
# push to the default branch and reported as a GitHub check run.
resource "devin_blueprint" "api" {
  org_id                         = devin_organization.frontend.org_id
  repo_name                      = "acme/api"
  position                       = 2
  source                         = "git"
  build_on_default_branch_commit = true
  report_check_runs              = true
}
