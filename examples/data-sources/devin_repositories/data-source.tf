data "devin_repositories" "acme" {
  org_id      = devin_organization.frontend.org_id
  filter_name = "acme/"
}

# Index every matching repository.
resource "devin_repository_indexing" "acme" {
  for_each        = toset([for r in data.devin_repositories.acme.repositories : r.repository_path])
  org_id          = devin_organization.frontend.org_id
  repository_path = each.value
}
