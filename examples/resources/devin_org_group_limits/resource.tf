# Enterprise singleton: declare at most one, owning the whole group config.
resource "devin_org_group_limits" "all" {
  groups = [
    {
      name           = "product-teams"
      org_ids        = [devin_organization.frontend.org_id, devin_organization.backend.org_id]
      max_cycle_acus = 5000
    },
    {
      name    = "experiments"
      org_ids = [devin_organization.labs.org_id]
    },
  ]
}
