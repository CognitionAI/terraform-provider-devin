data "devin_roles" "all" {}
data "devin_service_users" "all" {}

locals {
  org_member_role_id = one([
    for r in data.devin_roles.all.roles : r.role_id
    if r.role_type == "org" && r.role_name == "Member"
  ])
  ci_service_user_id = one([
    for su in data.devin_service_users.all.service_users : su.service_user_id
    if su.name == "CI Pipeline"
  ])
}

# Grant an existing service user a role in one organization.
resource "devin_org_service_user_role" "ci_backend" {
  org_id          = devin_organization.backend.org_id
  service_user_id = local.ci_service_user_id
  role_id         = local.org_member_role_id
}
