# The allowed session tags for an organization. This is a per-org singleton:
# declare at most one per organization, and applying it replaces the whole set.
resource "devin_org_tags" "backend" {
  org_id      = devin_organization.backend.org_id
  tags        = ["bugfix", "feature", "maintenance"]
  default_tag = "feature"
}
