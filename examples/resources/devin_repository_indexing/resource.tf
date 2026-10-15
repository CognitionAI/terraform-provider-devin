# Index the default branch only.
resource "devin_repository_indexing" "api" {
  org_id          = devin_organization.frontend.org_id
  repository_path = "acme/api"
}

# Index release branches as well as the default branch.
resource "devin_repository_indexing" "web" {
  org_id          = devin_organization.frontend.org_id
  repository_path = "acme/web"
  branch_names    = ["release/2026.10", "release/2026.11"]
}
