data "devin_roles" "all" {}
data "devin_service_users" "all" {}

locals {
  enterprise_admin_role_id = one([
    for r in data.devin_roles.all.roles : r.role_id
    if r.role_type == "enterprise" && r.role_name == "Admin"
  ])
  ci_service_user_id = one([
    for su in data.devin_service_users.all.service_users : su.service_user_id
    if su.name == "CI Pipeline"
  ])
}

# Grant an existing service user an enterprise-wide role. Destroying this
# resource leaves the assignment in place (see the resource documentation).
resource "devin_enterprise_service_user_role" "ci" {
  service_user_id = local.ci_service_user_id
  role_id         = local.enterprise_admin_role_id
}
