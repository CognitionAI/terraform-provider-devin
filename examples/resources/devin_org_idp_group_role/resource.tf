# Look up role IDs instead of hard-coding them.
data "devin_roles" "all" {}

locals {
  org_member_role_id = one([
    for r in data.devin_roles.all.roles : r.role_id
    if r.role_type == "org" && r.role_name == "Member"
  ])
}

# Grant an org-scoped role to everyone in the IdP group.
resource "devin_org_idp_group_role" "engineering_backend" {
  org_id         = devin_organization.backend.org_id
  idp_group_name = devin_idp_group.engineering.name
  role_id        = local.org_member_role_id
}
