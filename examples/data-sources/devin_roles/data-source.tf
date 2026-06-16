data "devin_roles" "all" {}

# Look up role IDs for IdP group role assignments.
locals {
  org_member_role_id = one([
    for r in data.devin_roles.all.roles : r.role_id
    if r.role_type == "org" && r.role_name == "Member"
  ])
}
